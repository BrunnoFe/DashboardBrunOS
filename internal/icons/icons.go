// Package icons guarda os Ícones guardados dos apps (0.24.0): os enviados pelo usuário e os do catálogo
// dashboard-icons (dashboardicons.com), baixados uma vez do jsDelivr quando alguém os usa. Depois disso
// aparecem mesmo sem internet. Ficam em <dados>/icons (enviados) e <dados>/icons/di (catálogo), para sempre.
//
// Segurança: SVG pode carregar script. Os arquivos são servidos com CSP "sandbox" e sem nenhum recurso
// externo, então nada roda nem se alguém abrir o endereço do ícone direto; dentro de <img> o navegador já
// não roda script de SVG.
package icons

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"brunos/internal/store"
)

// MaxSize é o tamanho máximo de um ícone (enviado ou baixado).
const MaxSize = 1 << 20

// CDN de onde vêm os ícones do catálogo. Troca nos testes.
var CDN = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons"

var (
	uploadName = regexp.MustCompile(`^[a-f0-9]{16}\.(svg|png|ico|jpg|webp|gif)$`)
	slugRe     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,78}[a-z0-9])?$`)
	// formatos tentados, do menor para o maior; nem todo ícone existe em todos
	catalogFormats = []string{"webp", "png", "svg"}
)

var types = map[string]string{
	"svg": "image/svg+xml", "png": "image/png", "ico": "image/x-icon",
	"jpg": "image/jpeg", "webp": "image/webp", "gif": "image/gif",
}

// Erros com mensagem pronta para o usuário (pt-BR).
var (
	ErrNotImage = errors.New("envie um ícone SVG, PNG, ICO, JPG, WebP ou GIF")
	ErrTooBig   = errors.New("ícone grande demais (máximo 1 MB)")
	ErrBroken   = errors.New("envio interrompido, tente de novo")
	ErrNotFound = errors.New("ícone não encontrado no dashboardicons.com")
	ErrOffline  = errors.New("não deu para baixar o ícone agora (o servidor está sem internet?)")
)

// Store é a pasta de ícones.
type Store struct {
	dir    string
	client *http.Client

	mu       sync.Mutex
	inflight map[string]*sync.WaitGroup // um download por ícone de cada vez
	missing  map[string]time.Time       // ícone que não existe no catálogo: não pergunta de novo por 1 h
	failed   map[string]time.Time       // falha de rede: espera 2 min antes de tentar de novo
}

// New cria as pastas (0700) e devolve a Store.
func New(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "icons")
	if err := os.MkdirAll(filepath.Join(dir, "di"), 0o700); err != nil {
		return nil, err
	}
	return &Store{
		dir:      dir,
		client:   &http.Client{Timeout: 12 * time.Second},
		inflight: map[string]*sync.WaitGroup{},
		missing:  map[string]time.Time{},
		failed:   map[string]time.Time{},
	}, nil
}

// Sniff descobre o formato pelo conteúdo ("" = não é um ícone aceito).
func Sniff(b []byte) string {
	switch http.DetectContentType(b) {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	case "image/x-icon", "image/vnd.microsoft.icon":
		return "ico"
	}
	if isSVG(b) {
		return "svg"
	}
	return ""
}

// isSVG: texto que começa com declaração XML, comentário, DOCTYPE ou a própria tag e tem "<svg" no começo.
func isSVG(b []byte) bool {
	head := b
	if len(head) > 4096 {
		head = head[:4096]
	}
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	head = bytes.TrimLeft(head, " \t\r\n")
	lower := bytes.ToLower(head)
	if !(bytes.HasPrefix(lower, []byte("<?xml")) || bytes.HasPrefix(lower, []byte("<svg")) ||
		bytes.HasPrefix(lower, []byte("<!--")) || bytes.HasPrefix(lower, []byte("<!doctype svg"))) {
		return false
	}
	return bytes.Contains(lower, []byte("<svg"))
}

// Save guarda um ícone enviado e devolve o nome (16 hex do SHA-256 + extensão: o mesmo arquivo enviado
// duas vezes vira um arquivo só).
func (s *Store) Save(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxSize+1))
	if err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			return "", ErrTooBig
		}
		return "", ErrBroken
	}
	if len(data) > MaxSize {
		return "", ErrTooBig
	}
	ext := Sniff(data)
	if ext == "" {
		return "", ErrNotImage
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:8]) + "." + ext
	path := filepath.Join(s.dir, name)
	if _, err := os.Stat(path); err == nil {
		return name, nil
	}
	if err := store.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return name, nil
}

// UploadPath devolve o caminho de um ícone enviado ("" se o nome não vale ou o arquivo não existe).
func (s *Store) UploadPath(name string) string {
	if !uploadName.MatchString(name) {
		return ""
	}
	p := filepath.Join(s.dir, name)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// ValidSlug diz se o nome do ícone do catálogo é aceito (letras minúsculas, números e hífen).
func ValidSlug(slug string) bool { return slugRe.MatchString(slug) }

func (s *Store) cached(slug string) string {
	for _, f := range catalogFormats {
		p := filepath.Join(s.dir, "di", slug+"."+f)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Catalog devolve o caminho do ícone do catálogo, baixando do CDN na primeira vez.
func (s *Store) Catalog(ctx context.Context, slug string) (string, error) {
	if !ValidSlug(slug) {
		return "", ErrNotFound
	}
	if p := s.cached(slug); p != "" {
		return p, nil
	}
	s.mu.Lock()
	if t, ok := s.missing[slug]; ok && time.Since(t) < time.Hour {
		s.mu.Unlock()
		return "", ErrNotFound
	}
	if t, ok := s.failed[slug]; ok && time.Since(t) < 2*time.Minute {
		s.mu.Unlock()
		return "", ErrOffline
	}
	if wg, ok := s.inflight[slug]; ok { // outro pedido já está baixando: espera e usa o resultado
		s.mu.Unlock()
		wg.Wait()
		if p := s.cached(slug); p != "" {
			return p, nil
		}
		return "", ErrOffline
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	s.inflight[slug] = wg
	s.mu.Unlock()

	p, err := s.download(ctx, slug)

	s.mu.Lock()
	delete(s.inflight, slug)
	switch {
	case errors.Is(err, ErrNotFound):
		s.missing[slug] = time.Now()
	case err != nil:
		s.failed[slug] = time.Now()
	}
	if len(s.missing) > 500 { // não deixa os mapas crescerem sem limite
		s.missing = map[string]time.Time{}
	}
	if len(s.failed) > 500 {
		s.failed = map[string]time.Time{}
	}
	s.mu.Unlock()
	wg.Done()
	return p, err
}

func (s *Store) download(ctx context.Context, slug string) (string, error) {
	// o download continua mesmo se quem pediu desistir (o próximo já encontra pronto)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	notFound := 0
	for _, f := range catalogFormats {
		data, status, err := s.get(ctx, fmt.Sprintf("%s/%s/%s.%s", CDN, f, slug, f))
		if err != nil {
			return "", ErrOffline
		}
		if status == http.StatusNotFound || status == http.StatusForbidden { // o jsDelivr dá 403 para svg que não existe
			notFound++
			continue
		}
		if status != http.StatusOK {
			return "", ErrOffline
		}
		if Sniff(data) != f { // o CDN devolveu outra coisa: não guarda
			continue
		}
		p := filepath.Join(s.dir, "di", slug+"."+f)
		if err := store.WriteFile(p, data, 0o600); err != nil {
			return "", err
		}
		return p, nil
	}
	if notFound == len(catalogFormats) {
		return "", ErrNotFound
	}
	return "", ErrOffline
}

func (s *Store) get(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "BrunOS")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, resp.StatusCode, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxSize+1))
	if err != nil {
		return nil, 0, err
	}
	if len(data) > MaxSize {
		return nil, 0, ErrTooBig
	}
	return data, resp.StatusCode, nil
}

// Register põe as rotas no mux; wrap exige a Sessão.
//
//	PUT /api/icons           corpo = o arquivo → {"name": "<16hex>.<ext>"}
//	GET /api/icons/{name}    ícone enviado
//	GET /api/icons/di/{slug} ícone do catálogo (baixa na primeira vez)
func (s *Store) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("PUT /api/icons", wrap(func(w http.ResponseWriter, r *http.Request) {
		name, err := s.Save(http.MaxBytesReader(w, r.Body, MaxSize+1))
		if err != nil {
			code := 400
			if !errors.Is(err, ErrNotImage) && !errors.Is(err, ErrTooBig) && !errors.Is(err, ErrBroken) {
				code, err = 500, errors.New("não foi possível salvar o ícone")
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"name": name})
	}))
	mux.HandleFunc("GET /api/icons/{name}", wrap(func(w http.ResponseWriter, r *http.Request) {
		p := s.UploadPath(r.PathValue("name"))
		if p == "" {
			http.NotFound(w, r)
			return
		}
		serve(w, r, p, 31536000) // o nome é o hash do conteúdo: nunca muda
	}))
	mux.HandleFunc("GET /api/icons/di/{slug}", wrap(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Catalog(r.Context(), r.PathValue("slug"))
		if err != nil {
			code := 502
			if errors.Is(err, ErrNotFound) {
				code = 404
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		serve(w, r, p, 86400)
	}))
}

// serve manda o arquivo com o tipo certo e a CSP que impede qualquer script.
func serve(w http.ResponseWriter, r *http.Request, path string, maxAge int) {
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", types[strings.TrimPrefix(filepath.Ext(path), ".")])
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", fmt.Sprintf("private, max-age=%d", maxAge))
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
