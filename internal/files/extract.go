package files

// Extrair .zip, .tar e .tar.gz numa pasta nova ao lado do arquivo.
//
// Proteções:
//   - zip-slip: cada nome é limpo e precisa ficar dentro da pasta nova; tudo é escrito pelo os.Root,
//     que recusa sair da raiz até por links simbólicos.
//   - links simbólicos, dispositivos etc. dentro do arquivo são ignorados.
//   - zip-bomba: conta os bytes realmente gravados e para se passar do espaço livre (menos 1 GB de
//     folga) ou de 200 mil itens; nesse caso apaga o que já tinha extraído.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

const (
	extractMaxEntries = 200_000
	extractMargin     = 1 << 30
)

var ErrArchive = errors.New("arquivo compactado inválido ou não suportado")

type archiveErr string

func (e archiveErr) Error() string   { return string(e) }
func (e archiveErr) Is(t error) bool { return t == ErrArchive }

// archiveBase devolve o nome sem a extensão de arquivo compactado, ou "" se não for suportado.
func archiveBase(name string) (base, kind string) {
	l := strings.ToLower(name)
	for _, s := range []struct{ ext, kind string }{{".tar.gz", "tgz"}, {".tgz", "tgz"}, {".tar", "tar"}, {".zip", "zip"}} {
		if strings.HasSuffix(l, s.ext) && len(name) > len(s.ext) {
			return name[:len(name)-len(s.ext)], s.kind
		}
	}
	return "", ""
}

type extractor struct {
	r       *Root
	dest    string // pasta nova, relativa à raiz
	budget  int64
	entries int
	dirs    map[string]bool
}

// safeName limpa o nome de um item do arquivo; "" = pular.
func safeName(n string) (string, error) {
	n = strings.ReplaceAll(n, `\`, "/")
	if strings.HasPrefix(n, "/") || strings.Contains(n, "\x00") {
		return "", archiveErr("o arquivo tem caminhos absolutos ou inválidos (" + n + ")")
	}
	c := path.Clean(n)
	if c == "." || c == "" {
		return "", nil
	}
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", archiveErr("o arquivo tenta gravar fora da pasta (" + n + ")")
	}
	for _, p := range strings.Split(c, "/") {
		if validName(p) != nil {
			return "", nil
		}
	}
	return c, nil
}

func (x *extractor) mkdirs(rel string) error {
	cur := x.dest
	for _, p := range strings.Split(rel, "/") {
		cur = path.Join(cur, p)
		if x.dirs[cur] {
			continue
		}
		if err := x.r.r.Mkdir(cur, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return mapErr(err)
		}
		x.dirs[cur] = true
	}
	return nil
}

func (x *extractor) count() error {
	x.entries++
	if x.entries > extractMaxEntries {
		return archiveErr("o arquivo tem itens demais (mais de 200 mil)")
	}
	return nil
}

func (x *extractor) file(name string, mode fs.FileMode, src io.Reader) error {
	if dir := path.Dir(name); dir != "." {
		if err := x.mkdirs(dir); err != nil {
			return err
		}
	}
	perm := mode.Perm()&0o755 | 0o600
	f, err := x.r.r.OpenFile(path.Join(x.dest, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil // nome repetido dentro do arquivo: fica o primeiro
		}
		return mapErr(err)
	}
	n, err := io.Copy(f, io.LimitReader(src, x.budget+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return archiveErr("não deu para ler o arquivo compactado (corrompido?)")
	}
	x.budget -= n
	if x.budget < 0 {
		return archiveErr("o conteúdo descompactado não cabe no disco (ou é uma zip-bomba)")
	}
	return nil
}

// Extract extrai o arquivo rel numa pasta nova e devolve o caminho dela.
func (s *Service) Extract(rootID, rel string) (string, error) {
	r, err := s.root(rootID)
	if err != nil {
		return "", err
	}
	if err := r.writable(); err != nil {
		return "", err
	}
	rel, err = clean(rel)
	if err != nil || rel == "." {
		return "", ErrBadPath
	}
	base, kind := archiveBase(path.Base(rel))
	if kind == "" {
		return "", archiveErr("só dá para extrair .zip, .tar e .tar.gz")
	}
	src, st, err := s.Open(rootID, rel)
	if err != nil {
		return "", err
	}
	defer src.Close()
	if st.IsDir() {
		return "", ErrBadPath
	}
	parentRel := path.Dir(rel)
	parent, err := r.realDir(parentRel)
	if err != nil {
		return "", err
	}
	free, _ := freeSpace(parent)
	budget := int64(free) - extractMargin
	if budget <= 0 {
		return "", ErrNoSpace
	}
	dest := path.Join(parentRel, uniqueName(parent, base))
	if err := r.r.Mkdir(dest, 0o755); err != nil {
		return "", mapErr(err)
	}
	x := &extractor{r: r, dest: dest, budget: budget, dirs: map[string]bool{dest: true}}
	switch kind {
	case "zip":
		err = x.zip(src, st.Size())
	default:
		var rd io.Reader = src
		if kind == "tgz" {
			gz, gerr := gzip.NewReader(src)
			if gerr != nil {
				err = archiveErr("o arquivo .tar.gz está corrompido")
				break
			}
			defer gz.Close()
			rd = gz
		}
		err = x.tar(rd)
	}
	if err != nil {
		if p, perr := r.realPath(dest); perr == nil {
			_ = os.RemoveAll(p) // não deixa extração pela metade
		}
		return "", err
	}
	return dest, nil
}

func (x *extractor) zip(f *os.File, size int64) error {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return archiveErr("o arquivo .zip está corrompido ou não é um zip")
	}
	for _, zf := range zr.File {
		if err := x.count(); err != nil {
			return err
		}
		name, err := safeName(zf.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		mode := zf.Mode()
		switch {
		case mode.IsDir():
			if err := x.mkdirs(name); err != nil {
				return err
			}
		case mode.IsRegular():
			if zf.Flags&0x1 != 0 {
				return archiveErr("o zip tem senha (criptografado); extraia no computador")
			}
			if int64(zf.UncompressedSize64) > x.budget {
				return archiveErr("o conteúdo descompactado não cabe no disco (ou é uma zip-bomba)")
			}
			rc, err := zf.Open()
			if err != nil {
				return archiveErr("o zip usa um formato de compressão não suportado")
			}
			err = x.file(name, mode, rc)
			rc.Close()
			if err != nil {
				return err
			}
		} // links e outros tipos: ignora
	}
	return nil
}

func (x *extractor) tar(rd io.Reader) error {
	tr := tar.NewReader(rd)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return archiveErr("o arquivo .tar está corrompido")
		}
		if err := x.count(); err != nil {
			return err
		}
		name, err := safeName(h.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := x.mkdirs(name); err != nil {
				return err
			}
		case tar.TypeReg:
			if h.Size > x.budget {
				return archiveErr("o conteúdo descompactado não cabe no disco")
			}
			if err := x.file(name, fs.FileMode(h.Mode), tr); err != nil {
				return err
			}
		}
	}
}
