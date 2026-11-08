package files

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"unicode/utf8"
)

// EditMax é o maior arquivo que o editor de texto abre (e salva).
const EditMax = 5 << 20

var (
	ErrConflict = errors.New("o arquivo mudou desde que você abriu (outro programa salvou antes). Recarregue para ver a versão nova")
	ErrTooBig   = errors.New("arquivo grande demais para o editor (máximo 5 MB)")
	ErrBinary   = errors.New("parece ser um arquivo binário, não um texto")
	ErrEncoding = errors.New("o arquivo não está em UTF-8; editar aqui poderia estragar os acentos")
	ErrLink     = errors.New("este item é um link; abra o arquivo original para editar")
)

type TextFile struct {
	Text     string `json:"text"`
	Mtime    int64  `json:"mtime"` // milissegundos (o salvar confere se não mudou)
	Size     int64  `json:"size"`
	ReadOnly bool   `json:"readOnly"`
}

// ReadText lê um arquivo de texto para o editor.
func (s *Service) ReadText(rootID, rel string) (TextFile, error) {
	r, err := s.root(rootID)
	if err != nil {
		return TextFile{}, err
	}
	f, st, err := s.Open(rootID, rel)
	if err != nil {
		return TextFile{}, err
	}
	defer f.Close()
	if st.IsDir() {
		return TextFile{}, ErrBadPath
	}
	if st.Size() > EditMax {
		return TextFile{}, ErrTooBig
	}
	b, err := io.ReadAll(io.LimitReader(f, EditMax+1))
	if err != nil {
		return TextFile{}, mapErr(err)
	}
	if bytes.IndexByte(b[:min(len(b), 8192)], 0) >= 0 {
		return TextFile{}, ErrBinary
	}
	if !utf8.Valid(b) {
		return TextFile{}, ErrEncoding
	}
	ro := r.ReadOnly || st.Mode().Perm()&0o222 == 0
	return TextFile{Text: string(b), Mtime: st.ModTime().UnixMilli(), Size: st.Size(), ReadOnly: ro}, nil
}

// SaveText grava o texto por cima do arquivo, de forma atômica (temporário + rename, mantendo as
// permissões). expect é o mtime (ms) que o editor leu; se o arquivo mudou desde então, recusa.
func (s *Service) SaveText(rootID, rel string, body io.Reader, expect int64) (int64, error) {
	r, err := s.root(rootID)
	if err != nil {
		return 0, err
	}
	if err := r.writable(); err != nil {
		return 0, err
	}
	rel, err = clean(rel)
	if err != nil || rel == "." {
		return 0, ErrBadPath
	}
	p, err := r.realPath(rel)
	if err != nil {
		return 0, err
	}
	st, err := os.Lstat(p)
	if err != nil {
		return 0, mapErr(err)
	}
	if st.Mode()&fs.ModeSymlink != 0 {
		return 0, ErrLink
	}
	if !st.Mode().IsRegular() {
		return 0, ErrBadPath
	}
	if expect != 0 && st.ModTime().UnixMilli() != expect {
		return 0, ErrConflict
	}
	// o rename por cima trocaria um arquivo protegido (ou de outro dono) mesmo sem permissão de
	// escrita nele: confere antes se dá para abri-lo para escrita
	wf, err := os.OpenFile(p, os.O_WRONLY, 0)
	if err != nil {
		return 0, mapErr(err)
	}
	wf.Close()

	dir := filepath.Dir(p)
	tmp, err := os.CreateTemp(dir, ".brunos-edit-*")
	if err != nil {
		return 0, mapErr(err)
	}
	tmpName := tmp.Name()
	fail := func(err error) (int64, error) { tmp.Close(); os.Remove(tmpName); return 0, err }
	n, err := io.Copy(tmp, io.LimitReader(body, EditMax+1))
	if err != nil {
		return fail(mapErr(err))
	}
	if n > EditMax {
		return fail(ErrTooBig)
	}
	if err := tmp.Sync(); err != nil {
		return fail(mapErr(err))
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return 0, mapErr(err)
	}
	_ = os.Chmod(tmpName, st.Mode().Perm())
	if err := os.Rename(tmpName, p); err != nil {
		os.Remove(tmpName)
		return 0, mapErr(err)
	}
	nst, err := os.Stat(p)
	if err != nil {
		return 0, mapErr(err)
	}
	return nst.ModTime().UnixMilli(), nil
}

// NewFile cria um arquivo vazio (para "Novo arquivo de texto").
func (s *Service) NewFile(rootID, rel, name string) (string, error) {
	r, err := s.root(rootID)
	if err != nil {
		return "", err
	}
	if err := r.writable(); err != nil {
		return "", err
	}
	if err := validName(name); err != nil {
		return "", err
	}
	rel, err = clean(rel)
	if err != nil {
		return "", err
	}
	target := path.Join(rel, name)
	f, err := r.r.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", mapErr(err)
	}
	return target, mapErr(f.Close())
}
