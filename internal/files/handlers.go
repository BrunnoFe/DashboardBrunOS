package files

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Register liga as rotas /api/files/* ao mux. wrap aplica autenticação/CSRF.
func (s *Service) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/files/roots", wrap(s.hRoots))
	mux.HandleFunc("GET /api/files/disks", wrap(s.hDisks))
	mux.HandleFunc("PUT /api/files/disks", wrap(s.hSetDisks))
	mux.HandleFunc("GET /api/files/list", wrap(s.hList))
	mux.HandleFunc("GET /api/files/raw", wrap(s.hRaw))
	mux.HandleFunc("GET /api/files/zip", wrap(s.hZip))
	mux.HandleFunc("GET /api/files/search", wrap(s.hSearch))
	mux.HandleFunc("PUT /api/files/upload", wrap(s.hUpload))
	mux.HandleFunc("POST /api/files/mkdir", wrap(s.hMkdir))
	mux.HandleFunc("POST /api/files/rename", wrap(s.hRename))
	mux.HandleFunc("POST /api/files/paste", wrap(s.hPaste))
	mux.HandleFunc("POST /api/files/trash", wrap(s.hTrash))
	mux.HandleFunc("POST /api/files/delete", wrap(s.hDelete))
	mux.HandleFunc("GET /api/files/trash", wrap(s.hTrashList))
	mux.HandleFunc("POST /api/files/trash/restore", wrap(s.hTrashRestore))
	mux.HandleFunc("POST /api/files/trash/delete", wrap(s.hTrashDelete))
	mux.HandleFunc("POST /api/files/trash/empty", wrap(s.hTrashEmpty))
	mux.HandleFunc("GET /api/files/text", wrap(s.hTextGet))
	mux.HandleFunc("PUT /api/files/text", wrap(s.hTextPut))
	mux.HandleFunc("POST /api/files/newfile", wrap(s.hNewFile))
	mux.HandleFunc("POST /api/files/upload/start", wrap(s.hUpStart))
	mux.HandleFunc("PUT /api/files/upload/chunk", wrap(s.hUpChunk))
	mux.HandleFunc("GET /api/files/upload/status", wrap(s.hUpStatus))
	mux.HandleFunc("POST /api/files/upload/finish", wrap(s.hUpFinish))
	mux.HandleFunc("POST /api/files/upload/cancel", wrap(s.hUpCancel))
	mux.HandleFunc("POST /api/files/extract", wrap(s.hExtract))
	mux.HandleFunc("GET /api/files/duplicates", wrap(s.hDuplicates))
	mux.HandleFunc("GET /api/files/usage", wrap(s.hUsage))
}

func (s *Service) hUpStart(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) {
		id, err := s.StartUpload(q.Root, q.Path, q.Name, q.Size)
		return map[string]any{"id": id, "offset": 0}, err
	})
}

func (s *Service) hUpChunk(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset, err := strconv.ParseInt(q.Get("offset"), 10, 64)
	if err != nil || offset < 0 {
		writeJSON(w, 400, map[string]string{"error": "posição inválida"})
		return
	}
	if r.ContentLength > ChunkMax {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "pedaço grande demais"})
		return
	}
	n, err := s.UploadChunk(q.Get("id"), offset, r.Body)
	if errors.Is(err, ErrOffset) {
		writeJSON(w, 409, map[string]any{"error": err.Error(), "offset": n})
		return
	}
	if errors.Is(err, ErrUploadGone) {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"offset": n})
}

func (s *Service) hUpStatus(w http.ResponseWriter, r *http.Request) {
	off, size, err := s.UploadStatus(r.URL.Query().Get("id"))
	if errors.Is(err, ErrUploadGone) {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"offset": off, "size": size})
}

func (s *Service) hUpFinish(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) {
		p, err := s.FinishUpload(q.ID)
		return map[string]string{"path": p}, err
	})
}

func (s *Service) hUpCancel(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) { return nil, s.CancelUpload(q.ID) })
}

func (s *Service) hExtract(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) {
		p, err := s.Extract(q.Root, q.Path)
		return map[string]string{"path": p}, err
	})
}

func (s *Service) hDuplicates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	minSize, _ := strconv.ParseInt(q.Get("min"), 10, 64)
	if minSize <= 0 {
		minSize = 1 << 20
	}
	res, err := s.Duplicates(q.Get("root"), q.Get("path"), minSize)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Service) hUsage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := s.Usage(q.Get("root"), q.Get("path"), q.Get("refresh") == "1")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Service) hTextGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tf, err := s.ReadText(q.Get("root"), q.Get("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, tf)
}

func (s *Service) hTextPut(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	expect, _ := strconv.ParseInt(q.Get("mtime"), 10, 64)
	if r.ContentLength > EditMax {
		writeErr(w, ErrTooBig)
		return
	}
	mt, err := s.SaveText(q.Get("root"), q.Get("path"), r.Body, expect)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"mtime": mt})
}

func (s *Service) hNewFile(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) {
		p, err := s.NewFile(q.Root, q.Path, q.Name)
		return map[string]string{"path": p}, err
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	msg := "erro interno"
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrUnknownRoot):
		code, msg = http.StatusNotFound, err.Error()
	case errors.Is(err, ErrReadOnly):
		code, msg = http.StatusForbidden, err.Error()
	case errors.Is(err, ErrExists):
		code, msg = http.StatusConflict, err.Error()
	case errors.Is(err, ErrBadName), errors.Is(err, ErrBadPath), errors.Is(err, ErrIntoItself):
		code, msg = http.StatusBadRequest, err.Error()
	case errors.Is(err, ErrConflict):
		code, msg = http.StatusConflict, err.Error()
	case errors.Is(err, ErrTooBig):
		code, msg = http.StatusRequestEntityTooLarge, err.Error()
	case errors.Is(err, ErrBinary), errors.Is(err, ErrEncoding), errors.Is(err, ErrLink):
		code, msg = http.StatusUnsupportedMediaType, err.Error()
	case errors.Is(err, ErrUploadGone):
		code, msg = http.StatusNotFound, err.Error()
	case errors.Is(err, ErrNoSpace), errors.Is(err, ErrArchive):
		code, msg = http.StatusBadRequest, err.Error()
	default:
		// mensagens que já são amigáveis (em português) passam; o resto vira genérico
		if e := err.Error(); !strings.ContainsAny(e, "/\\:") && len(e) < 160 {
			msg = e
		} else {
			slog.Warn("arquivos", "erro", err)
		}
	}
	writeJSON(w, code, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func (s *Service) hRoots(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.RootInfos())
}

func (s *Service) hDisks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Disks())
}

func (s *Service) hSetDisks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mapped []string `json:"mapped"`
	}
	if err := decode(r, &body); err != nil || len(body.Mapped) > 200 {
		writeJSON(w, 400, map[string]string{"error": "pedido inválido"})
		return
	}
	if err := s.SetMapped(body.Mapped); err != nil {
		slog.Warn("discos", "erro", err)
		writeJSON(w, 500, map[string]string{"error": "não deu para salvar os discos"})
		return
	}
	writeJSON(w, 200, s.RootInfos())
}

func (s *Service) hList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	entries, err := s.List(q.Get("root"), q.Get("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	p, _ := clean(q.Get("path"))
	if p == "." {
		p = ""
	}
	writeJSON(w, 200, map[string]any{"root": q.Get("root"), "path": p, "entries": entries})
}

// Tipos que podem ser exibidos no navegador. Qualquer outro vai como download,
// e textos (inclusive .html e .svg) vão como text/plain: um arquivo da sua pasta
// nunca consegue rodar código com a sessão do BrunOS.
var inlineTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true, "image/avif": true, "image/bmp": true,
	"video/mp4": true, "video/webm": true, "video/ogg": true, "video/quicktime": true, "video/x-matroska": true,
	"audio/mpeg": true, "audio/ogg": true, "audio/wav": true, "audio/x-wav": true, "audio/flac": true, "audio/mp4": true, "audio/aac": true, "audio/webm": true,
	"application/pdf": true,
}
var textExts = map[string]bool{
	".txt": true, ".md": true, ".log": true, ".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
	".conf": true, ".cfg": true, ".csv": true, ".tsv": true, ".xml": true, ".html": true, ".htm": true, ".svg": true,
	".js": true, ".ts": true, ".css": true, ".py": true, ".go": true, ".rs": true, ".sh": true, ".env": true, ".c": true,
	".h": true, ".cpp": true, ".java": true, ".rb": true, ".php": true, ".sql": true, ".r": true, ".m": true, ".tex": true,
	".bib": true, ".srt": true, ".vtt": true, ".nfo": true, ".properties": true, ".dockerfile": true,
}

// mediaTypes: a imagem do BrunOS é "scratch" (sem /etc/mime.types), e a tabela embutida do Go só
// conhece imagens e arquivos da web. Sem isto, .mp4 e .mp3 iam como download e o player do navegador
// não conseguia avançar/retroceder direito.
var mediaTypes = map[string]string{
	".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".ogv": "video/ogg", ".mov": "video/quicktime",
	".mkv": "video/x-matroska",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg",
	".wav": "audio/wav", ".flac": "audio/flac", ".weba": "audio/webm",
	".bmp": "image/bmp", ".avif": "image/avif", ".webp": "image/webp",
}

func contentType(ext string) string {
	if t, ok := mediaTypes[ext]; ok {
		return t
	}
	t, _, _ := mime.ParseMediaType(mime.TypeByExtension(ext))
	return t
}

func contentDisposition(kind, name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return kind + `; filename="` + ascii + `"; filename*=UTF-8''` + url.PathEscape(name)
}

func (s *Service) hRaw(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, st, err := s.Open(q.Get("root"), q.Get("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer f.Close()
	if st.IsDir() {
		writeErr(w, ErrBadPath)
		return
	}
	name := st.Name()
	ext := strings.ToLower(filepath.Ext(name))
	ctype := contentType(ext)
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=600")
	download := q.Get("download") == "1"
	switch {
	case !download && inlineTypes[ctype]:
		h.Set("Content-Type", ctype)
		h.Set("Content-Disposition", contentDisposition("inline", name))
	case !download && (textExts[ext] || name == "Dockerfile" || name == "Makefile" || strings.HasPrefix(ctype, "text/")):
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Content-Disposition", contentDisposition("inline", name))
	default:
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", contentDisposition("attachment", name))
	}
	if ctype != "application/pdf" { // o leitor de PDF do Chrome não funciona em sandbox
		h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'")
	}
	http.ServeContent(w, r, "", st.ModTime(), f) // aceita Range: vídeos podem ser avançados
}

// hZip envia pastas/arquivos como um .zip montado na hora (sem compressão:
// fotos e vídeos já são comprimidos, e assim a CPU do servidor quase não trabalha).
func (s *Service) hZip(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	root, err := s.root(q.Get("root"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var rels []string
	for _, p := range q["p"] {
		c, err := clean(p)
		if err != nil {
			writeErr(w, err)
			return
		}
		if _, err := root.r.Stat(c); err != nil {
			writeErr(w, mapErr(err))
			return
		}
		rels = append(rels, c)
	}
	if len(rels) == 0 {
		writeErr(w, ErrBadPath)
		return
	}
	name := "arquivos.zip"
	if len(rels) == 1 && rels[0] != "." {
		name = path.Base(rels[0]) + ".zip"
	} else if len(rels) == 1 {
		name = root.Name + ".zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", name))
	zw := zip.NewWriter(w)
	defer zw.Close()
	fsys := root.r.FS()
	for _, rel := range rels {
		base := path.Dir(rel)
		_ = fs.WalkDir(fsys, rel, func(p string, d fs.DirEntry, err error) error {
			if err != nil || r.Context().Err() != nil {
				return nil
			}
			if d.IsDir() && d.Name() == trashDirName {
				return fs.SkipDir
			}
			if d.Type()&fs.ModeSymlink != 0 || !(d.IsDir() || d.Type().IsRegular()) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			inZip := p
			if base != "." {
				inZip = strings.TrimPrefix(p, base+"/")
			}
			if rel == "." {
				inZip = path.Join(root.Name, p)
			}
			hdr, err := zip.FileInfoHeader(info)
			if err != nil {
				return nil
			}
			hdr.Name = inZip
			hdr.Method = zip.Store
			if d.IsDir() {
				hdr.Name += "/"
				_, err = zw.CreateHeader(hdr)
				return err
			}
			fw, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			f, err := fsys.Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			_, err = io.Copy(fw, f)
			return err
		})
	}
}

func (s *Service) hSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hits, truncated, err := s.Search(q.Get("root"), q.Get("path"), q.Get("q"), 300)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"hits": hits, "truncated": truncated})
}

func (s *Service) hUpload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if r.ContentLength > s.MaxUpload {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "arquivo maior que o limite de upload"})
		return
	}
	p, err := s.Upload(q.Get("root"), q.Get("path"), q.Get("name"), r.Body)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"path": p})
}

type opReq struct {
	Root     string   `json:"root"`
	Path     string   `json:"path"`
	Paths    []string `json:"paths"`
	Name     string   `json:"name"`
	DestRoot string   `json:"destRoot"`
	DestPath string   `json:"destPath"`
	Copy     bool     `json:"copy"`
	ID       string   `json:"id"`
	Size     int64    `json:"size"`
}

func (s *Service) op(w http.ResponseWriter, r *http.Request, fn func(opReq) (any, error)) {
	var req opReq
	if err := decode(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	if len(req.Paths) > 5000 {
		writeJSON(w, 400, map[string]string{"error": "itens demais de uma vez"})
		return
	}
	out, err := fn(req)
	if err != nil {
		writeErr(w, err)
		return
	}
	if out == nil {
		out = map[string]bool{"ok": true}
	}
	writeJSON(w, 200, out)
}

func (s *Service) hMkdir(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) {
		p, err := s.Mkdir(q.Root, q.Path, q.Name)
		return map[string]string{"path": p}, err
	})
}
func (s *Service) hRename(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) { return nil, s.Rename(q.Root, q.Path, q.Name) })
}
func (s *Service) hPaste(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) { return nil, s.Paste(q.Root, q.Paths, q.DestRoot, q.DestPath, q.Copy) })
}
func (s *Service) hTrash(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) { return nil, s.Trash(q.Root, q.Paths) })
}
func (s *Service) hDelete(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) { return nil, s.Delete(q.Root, q.Paths) })
}
func (s *Service) hTrashList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.TrashList())
}
func (s *Service) hTrashRestore(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) {
		p, err := s.TrashRestore(q.Root, q.ID)
		return map[string]string{"path": p}, err
	})
}
func (s *Service) hTrashDelete(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) { return nil, s.TrashDelete(q.Root, q.ID) })
}
func (s *Service) hTrashEmpty(w http.ResponseWriter, r *http.Request) {
	s.op(w, r, func(q opReq) (any, error) { return nil, s.TrashEmpty(q.Root) })
}
