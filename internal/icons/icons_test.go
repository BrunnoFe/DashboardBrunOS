package icons

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

var (
	pngData = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	svgData = []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"></svg>`)
	icoData = append([]byte{0, 0, 1, 0, 1, 0}, make([]byte, 32)...)
)

func TestSniff(t *testing.T) {
	cases := map[string]struct {
		in   []byte
		want string
	}{
		"png":            {pngData, "png"},
		"svg":            {svgData, "svg"},
		"svg sem xml":    {[]byte("\n  <svg viewBox='0 0 1 1'/>"), "svg"},
		"svg com bom":    {append([]byte("\xef\xbb\xbf"), svgData...), "svg"},
		"svg comentário": {[]byte("<!-- x --><svg/>"), "svg"},
		"ico":            {icoData, "ico"},
		"jpg":            {append([]byte{0xff, 0xd8, 0xff}, make([]byte, 16)...), "jpg"},
		"html":           {[]byte("<html><body><svg/></body></html>"), ""},
		"texto":          {[]byte("olá"), ""},
		"xml sem svg":    {[]byte("<?xml version='1.0'?><a/>"), ""},
	}
	for name, c := range cases {
		if got := Sniff(c.in); got != c.want {
			t.Errorf("%s: Sniff = %q, quer %q", name, got, c.want)
		}
	}
}

func TestSave(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name, err := s.Save(bytes.NewReader(svgData))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".svg") || len(name) != 20 {
		t.Fatalf("nome inesperado %q", name)
	}
	again, _ := s.Save(bytes.NewReader(svgData))
	if again != name {
		t.Errorf("o mesmo arquivo deveria ter o mesmo nome: %q e %q", name, again)
	}
	if s.UploadPath(name) == "" {
		t.Error("UploadPath não achou o ícone salvo")
	}
	st, _ := os.Stat(s.UploadPath(name))
	if st.Mode().Perm() != 0o600 {
		t.Errorf("permissão %v, quer 0600", st.Mode().Perm())
	}
	for _, bad := range []string{"../x.svg", "0123456789abcdef.exe", "0123456789abcdef.svg", "di"} {
		if s.UploadPath(bad) != "" {
			t.Errorf("UploadPath(%q) deveria recusar", bad)
		}
	}
	if _, err := s.Save(strings.NewReader("não é imagem")); !errors.Is(err, ErrNotImage) {
		t.Errorf("texto: erro %v, quer ErrNotImage", err)
	}
	big := append(append([]byte{}, pngData...), make([]byte, MaxSize)...)
	if _, err := s.Save(bytes.NewReader(big)); !errors.Is(err, ErrTooBig) {
		t.Errorf("grande: erro %v, quer ErrTooBig", err)
	}
}

func TestValidSlug(t *testing.T) {
	for slug, ok := range map[string]bool{"router": true, "home-assistant": true, "a": true, "x1": true,
		"": false, "-x": false, "x-": false, "Router": false, "../x": false, "a/b": false, "a.b": false, strings.Repeat("a", 81): false} {
		if ValidSlug(slug) != ok {
			t.Errorf("ValidSlug(%q) = %v, quer %v", slug, !ok, ok)
		}
	}
}

// CDN falso: router existe só em svg, png-only só em png, "lixo" devolve HTML no lugar do webp.
func fakeCDN(t *testing.T, hits *int32) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		switch r.URL.Path {
		case "/svg/router.svg":
			_, _ = w.Write(svgData)
		case "/png/png-only.png":
			_, _ = w.Write(pngData)
		case "/webp/lixo.webp":
			_, _ = w.Write([]byte("<html>erro</html>"))
		case "/webp/quebrado.webp":
			w.WriteHeader(500)
		case "/svg/nao-existe.svg": // como o jsDelivr de verdade
			w.WriteHeader(403)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := CDN
	CDN = srv.URL
	t.Cleanup(func() { CDN = old })
}

func TestCatalog(t *testing.T) {
	var hits int32
	fakeCDN(t, &hits)
	dir := t.TempDir()
	s, _ := New(dir)
	ctx := context.Background()

	p, err := s.Catalog(ctx, "router")
	if err != nil || filepath.Base(p) != "router.svg" {
		t.Fatalf("router: %q %v", p, err)
	}
	before := atomic.LoadInt32(&hits)
	if _, err := s.Catalog(ctx, "router"); err != nil || atomic.LoadInt32(&hits) != before {
		t.Errorf("a segunda vez deveria vir da pasta, sem ir ao CDN (err %v)", err)
	}
	if p, err := s.Catalog(ctx, "png-only"); err != nil || filepath.Base(p) != "png-only.png" {
		t.Errorf("png-only: %q %v", p, err)
	}
	if _, err := s.Catalog(ctx, "nao-existe"); !errors.Is(err, ErrNotFound) {
		t.Errorf("nao-existe: %v, quer ErrNotFound", err)
	}
	before = atomic.LoadInt32(&hits)
	if _, err := s.Catalog(ctx, "nao-existe"); !errors.Is(err, ErrNotFound) || atomic.LoadInt32(&hits) != before {
		t.Error("ícone que não existe não deveria ir ao CDN de novo tão cedo")
	}
	if _, err := s.Catalog(ctx, "quebrado"); !errors.Is(err, ErrOffline) {
		t.Errorf("quebrado: %v, quer ErrOffline", err)
	}
	if _, err := s.Catalog(ctx, "lixo"); err == nil {
		t.Error("lixo: o HTML no lugar do ícone não pode ser guardado")
	}
	if _, err := os.Stat(filepath.Join(dir, "icons", "di", "lixo.webp")); err == nil {
		t.Error("lixo.webp foi guardado")
	}
	if _, err := s.Catalog(ctx, "../etc"); !errors.Is(err, ErrNotFound) {
		t.Errorf("slug inválido: %v", err)
	}
}

func TestCatalogOneDownload(t *testing.T) {
	var hits int32
	fakeCDN(t, &hits)
	s, _ := New(t.TempDir())
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = s.Catalog(context.Background(), "router") }()
	}
	wg.Wait()
	// router: webp e png dão 404, svg dá certo = 3 pedidos, mesmo com 8 pedidos ao mesmo tempo
	if h := atomic.LoadInt32(&hits); h != 3 {
		t.Errorf("%d pedidos ao CDN, quer 3", h)
	}
}

func TestHandlers(t *testing.T) {
	var hits int32
	fakeCDN(t, &hits)
	s, _ := New(t.TempDir())
	mux := http.NewServeMux()
	s.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/icons", bytes.NewReader(svgData)))
	if rec.Code != 200 {
		t.Fatalf("envio: %d %s", rec.Code, rec.Body)
	}
	name := strings.Split(rec.Body.String(), `"`)[3]

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/icons/"+name, nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("servir enviado: %d %v", rec.Code, rec.Header())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/icons/di/router", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Errorf("catálogo: %d %v", rec.Code, rec.Header())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/icons/di/nao-existe", nil))
	if rec.Code != 404 {
		t.Errorf("catálogo inexistente: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/icons", strings.NewReader("<html>")))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "SVG") {
		t.Errorf("envio inválido: %d %s", rec.Code, rec.Body)
	}
}
