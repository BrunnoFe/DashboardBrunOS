package files

// Lugares remotos (0.37.0, decisão 73): o Nextcloud aparece na janela Arquivos ao lado da Home e dos
// discos. As rotas /api/files/* são as mesmas; quando o lugar é remoto, cada operação fala com ele (WebDAV,
// em integrations/webdav.go) em vez de abrir uma pasta local. Funcionam: listar, ver (com avançar/voltar no
// vídeo), baixar, enviar, criar pasta, renomear, mover, apagar (vai para a lixeira do próprio Nextcloud) e
// copiar/mover entre o Nextcloud e os discos. Ainda não: zip, busca, editor, extrair, duplicados e mapa
// do espaço (respondem com uma mensagem clara).

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// Remote é um lugar que não é pasta local. Caminhos relativos à raiz, já limpos ("." = a raiz).
type Remote interface {
	List(ctx context.Context, rel string) ([]Entry, error)
	Stat(ctx context.Context, rel string) (Entry, error)
	// Get abre o arquivo; rng é o cabeçalho Range do navegador ("" = inteiro). Quem chama fecha o corpo.
	Get(ctx context.Context, rel, rng string) (*http.Response, error)
	Mkdir(ctx context.Context, rel string) error
	Put(ctx context.Context, rel string, body io.Reader, size int64) error
	Move(ctx context.Context, from, to string) error // ErrExists se o destino já existe
	Copy(ctx context.Context, from, to string) error
	Delete(ctx context.Context, rel string) error // no Nextcloud, vai para a lixeira dele
}

// RemoteRoot é um lugar remoto com nome (id começa com "nc-").
type RemoteRoot struct {
	ID, Name string
	R        Remote
}

// ErrRemoteUnsupported: a função ainda não existe para lugares remotos.
var ErrRemoteUnsupported = errors.New("isso ainda não funciona no Nextcloud: copie para um disco do servidor e faça lá")

const remoteTimeout = 30 * time.Second // operações curtas (listar, criar, mover); envio e cópia usam o pedido

func (s *Service) remote(id string) Remote {
	if s.Remotes == nil || !strings.HasPrefix(id, "nc-") {
		return nil
	}
	for _, x := range s.Remotes() {
		if x.ID == id {
			return x.R
		}
	}
	return nil
}

func rctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), remoteTimeout)
}

func joinRel(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}

// ── operações (chamadas pelos métodos do Service quando o lugar é remoto) ──

func (s *Service) remoteList(rm Remote, rel string) ([]Entry, error) {
	rel, err := clean(rel)
	if err != nil {
		return nil, err
	}
	ctx, cancel := rctx()
	defer cancel()
	return rm.List(ctx, rel)
}

func (s *Service) remoteMkdir(rm Remote, rel, name string) (string, error) {
	if err := validName(name); err != nil {
		return "", err
	}
	rel, err := clean(rel)
	if err != nil {
		return "", err
	}
	p := joinRel(rel, name)
	ctx, cancel := rctx()
	defer cancel()
	return p, rm.Mkdir(ctx, p)
}

func (s *Service) remoteRename(rm Remote, rel, newName string) error {
	if err := validName(newName); err != nil {
		return err
	}
	rel, err := clean(rel)
	if err != nil || rel == "." {
		return ErrBadPath
	}
	ctx, cancel := rctx()
	defer cancel()
	return rm.Move(ctx, rel, joinRel(path.Dir(rel), newName))
}

func (s *Service) remoteDelete(rm Remote, rels []string) error {
	ctx, cancel := rctx()
	defer cancel()
	for _, rel := range rels {
		c, err := clean(rel)
		if err != nil || c == "." {
			return ErrBadPath
		}
		if err := rm.Delete(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) remoteUpload(rm Remote, rel, relName string, body io.Reader, size int64) (string, error) {
	rel, err := clean(rel)
	if err != nil {
		return "", err
	}
	relName = strings.Trim(strings.ReplaceAll(relName, `\`, "/"), "/")
	parts := strings.Split(relName, "/")
	for _, p := range parts {
		if err := validName(p); err != nil {
			return "", err
		}
	}
	// enviar pasta: cria as subpastas que faltarem (já existir não é erro)
	ctx := context.Background()
	dir := rel
	for _, p := range parts[:len(parts)-1] {
		dir = joinRel(dir, p)
		if err := rm.Mkdir(ctx, dir); err != nil && !errors.Is(err, ErrExists) {
			return "", err
		}
	}
	dst := joinRel(dir, parts[len(parts)-1])
	return dst, rm.Put(ctx, dst, body, size)
}

// remotePaste: copiar ou mover quando um dos lados (ou os dois) é remoto.
func (s *Service) remotePaste(srcRoot string, rels []string, dstRoot, dstRel string, copyMode bool) error {
	srm, drm := s.remote(srcRoot), s.remote(dstRoot)
	dstRel, err := clean(dstRel)
	if err != nil {
		return err
	}
	ctx := context.Background()
	for _, r := range rels {
		rel, err := clean(r)
		if err != nil || rel == "." {
			return ErrBadPath
		}
		dst := joinRel(dstRel, path.Base(rel))
		switch {
		case srm != nil && srm == drm: // dentro do mesmo Nextcloud: o próprio servidor dele copia/move
			if dst == rel || strings.HasPrefix(dst+"/", rel+"/") {
				return ErrIntoItself
			}
			if copyMode {
				err = srm.Copy(ctx, rel, dst)
			} else {
				err = srm.Move(ctx, rel, dst)
			}
		case srm != nil && drm != nil: // de um Nextcloud para outro: baixa e envia
			err = remoteToRemote(ctx, srm, drm, rel, dst)
			if err == nil && !copyMode {
				err = srm.Delete(ctx, rel)
			}
		case srm != nil: // Nextcloud → disco
			err = s.remoteToLocal(ctx, srm, rel, dstRoot, dstRel)
			if err == nil && !copyMode {
				err = srm.Delete(ctx, rel)
			}
		default: // disco → Nextcloud
			err = s.localToRemote(ctx, srcRoot, rel, drm, dst)
			if err == nil && !copyMode {
				err = s.Delete(srcRoot, []string{rel})
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func remoteToRemote(ctx context.Context, src, dst Remote, rel, to string) error {
	e, err := src.Stat(ctx, rel)
	if err != nil {
		return err
	}
	if e.Dir {
		if err := dst.Mkdir(ctx, to); err != nil {
			return err
		}
		list, err := src.List(ctx, rel)
		if err != nil {
			return err
		}
		for _, c := range list {
			if err := remoteToRemote(ctx, src, dst, joinRel(rel, c.Name), joinRel(to, c.Name)); err != nil {
				return err
			}
		}
		return nil
	}
	resp, err := src.Get(ctx, rel, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return dst.Put(ctx, to, resp.Body, resp.ContentLength)
}

// remoteToLocal baixa um arquivo ou pasta do Nextcloud para dentro de dstRel num disco (pelo Upload, que
// já confere o caminho, cria as subpastas e grava com segurança).
func (s *Service) remoteToLocal(ctx context.Context, rm Remote, rel, dstRoot, dstRel string) error {
	base := path.Dir(rel)
	var walk func(p string) error
	walk = func(p string) error {
		e, err := rm.Stat(ctx, p)
		if err != nil {
			return err
		}
		relName := strings.TrimPrefix(p, base+"/")
		if base == "." {
			relName = p
		}
		if e.Dir {
			if _, err := s.Mkdir(dstRoot, joinRel(dstRel, path.Dir(relName)), path.Base(relName)); err != nil && !errors.Is(err, ErrExists) {
				return err
			}
			list, err := rm.List(ctx, p)
			if err != nil {
				return err
			}
			for _, c := range list {
				if err := walk(joinRel(p, c.Name)); err != nil {
					return err
				}
			}
			return nil
		}
		resp, err := rm.Get(ctx, p, "")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, err = s.Upload(dstRoot, dstRel, relName, resp.Body)
		return err
	}
	return walk(rel)
}

// localToRemote envia um arquivo ou pasta de um disco para o Nextcloud.
func (s *Service) localToRemote(ctx context.Context, srcRoot, rel string, rm Remote, to string) error {
	r, err := s.root(srcRoot)
	if err != nil {
		return err
	}
	if s.isSecret(r, rel) {
		return ErrNotFound
	}
	return fs.WalkDir(r.r.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return mapErr(err)
		}
		if s.isSecret(r, p) || d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil // segredos e atalhos não saem do servidor
		}
		dst := to
		if p != rel {
			dst = joinRel(to, strings.TrimPrefix(p, rel+"/"))
		}
		if d.IsDir() {
			if err := rm.Mkdir(ctx, dst); err != nil && !(p != rel && errors.Is(err, ErrExists)) {
				return err
			}
			return nil
		}
		f, err := r.r.Open(p)
		if err != nil {
			return mapErr(err)
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return mapErr(err)
		}
		return rm.Put(ctx, dst, f, st.Size())
	})
}

// ── rotas de leitura (GET com ?root=) ──

// remoteRaw entrega um arquivo do Nextcloud repassando o Range (o vídeo avança e volta sem baixar tudo).
func (s *Service) remoteRaw(w http.ResponseWriter, r *http.Request, rm Remote, rel string) {
	rel, err := clean(rel)
	if err != nil || rel == "." {
		writeErr(w, ErrBadPath)
		return
	}
	resp, err := rm.Get(r.Context(), rel, r.Header.Get("Range"))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer resp.Body.Close()
	name := path.Base(rel)
	ext := strings.ToLower(path.Ext(name))
	ctype := contentType(ext)
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=600")
	if r.URL.Query().Get("download") != "1" && inlineTypes[ctype] {
		h.Set("Content-Type", ctype)
		h.Set("Content-Disposition", contentDisposition("inline", name))
	} else {
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", contentDisposition("attachment", name))
	}
	for _, k := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified"} {
		if v := resp.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// remoteReaderAt lê pedaços de um arquivo remoto por Range (para os formatos de um vídeo).
type remoteReaderAt struct {
	ctx context.Context
	rm  Remote
	rel string
}

func (x remoteReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	resp, err := x.rm.Get(x.ctx, x.rel, "bytes="+itoa(off)+"-"+itoa(off+int64(len(p))-1))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && off > 0 { // servidor sem Range: não dá para pular
		return 0, io.EOF
	}
	n, err := io.ReadFull(resp.Body, p)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF
	}
	return n, err
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func (s *Service) remoteMedia(rm Remote, rel string) (MediaInfo, error) {
	rel, err := clean(rel)
	if err != nil || rel == "." {
		return MediaInfo{}, ErrBadPath
	}
	ctx, cancel := rctx()
	defer cancel()
	e, err := rm.Stat(ctx, rel)
	if err != nil {
		return MediaInfo{}, err
	}
	info, err := ProbeMedia(remoteReaderAt{ctx, rm, rel}, e.Size)
	if err != nil {
		return MediaInfo{}, errMedia
	}
	return info, nil
}

// remoteGET: as rotas GET com ?root= que valem para lugares remotos; as outras dizem que ainda não.
func (s *Service) remoteGET(next http.HandlerFunc, h func(w http.ResponseWriter, r *http.Request, rm Remote)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rm := s.remote(r.URL.Query().Get("root")); rm != nil {
			if h == nil {
				writeErr(w, ErrRemoteUnsupported)
				return
			}
			h(w, r, rm)
			return
		}
		next(w, r)
	}
}
