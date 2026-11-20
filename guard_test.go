package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"brunos/internal/runtipi"
)

// Revisão de segurança, lote 4: X-Forwarded-For só do Traefik e nomes de endereço aceitos.

func httptestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
}

func TestProxyConfiavel(t *testing.T) {
	t.Setenv("BRUNOS_TRUSTED_PROXIES", "172.30.0.5, 10.9.0.0/16")
	trusted.mu.Lock()
	trusted.at = time.Time{}
	trusted.mu.Unlock()
	t.Cleanup(func() { trusted.mu.Lock(); trusted.at = time.Time{}; trusted.mu.Unlock() })

	cases := []struct {
		remote, xff, proto, want string
		https                    bool
	}{
		{"172.30.0.5:4000", "203.0.113.9", "https", "203.0.113.9", true},         // Traefik: vale
		{"10.9.3.4:4000", "198.51.100.1, 203.0.113.9", "", "203.0.113.9", false}, // faixa confiável: o último (quem o Traefik viu)
		{"192.168.1.20:4000", "203.0.113.9", "https", "192.168.1.20", false},     // aparelho da rede: ignorado
		{"127.0.0.1:4000", "203.0.113.9", "https", "127.0.0.1", false},           // nem do próprio servidor
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		r.Header.Set("X-Forwarded-For", c.xff)
		r.Header.Set("X-Forwarded-Proto", c.proto)
		if got := clientIP(r); got != c.want {
			t.Errorf("%s com XFF %q: IP %s, esperava %s", c.remote, c.xff, got, c.want)
		}
		if got := isHTTPS(r); got != c.https {
			t.Errorf("%s: https=%v, esperava %v", c.remote, got, c.https)
		}
	}
}

func TestEnderecosAceitos(t *testing.T) {
	dir, tipi := t.TempDir(), t.TempDir()
	for _, d := range []string{"state", "apps"} {
		if err := os.MkdirAll(filepath.Join(tipi, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(tipi, "state/settings.json"), []byte(`{"localDomain":"casa.exemplo"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"allowedHosts":"painel.meudominio.com.br, https://outro.com/x"}`), 0o600)
	t.Setenv("BRUNOS_HOSTS", "env.exemplo.org")
	s := &server{cfg: config{DataDir: dir}, tipi: runtipi.New(tipi)}

	for h, want := range map[string]bool{
		"":                         true,
		"192.168.1.50:8080":        true,
		"[fd7a::1]:80":             true,
		"localhost:8080":           true,
		"servidor":                 true,
		"brunos.local":             true,
		"nas.lan":                  true,
		"pc.tail1234.ts.net":       true,
		"casa.exemplo":             true,
		"brunos.casa.exemplo:443":  true,
		"PAINEL.meudominio.com.br": true,
		"outro.com":                true,
		"env.exemplo.org":          true,
		"attacker.example.com":     false,
		"casa.exemplo.evil.com":    false,
		"xcasa.exemplo":            false,
		"meudominio.com.br":        false,
	} {
		if got := s.hostOK(h); got != want {
			t.Errorf("hostOK(%q) = %v, esperava %v", h, got, want)
		}
	}

	// recusado: 421, e a página não reflete o nome sem escapar
	h := s.hostCheck(httptestHandler())
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "<script>x</script>.evil.com"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 421 || strings.Contains(w.Body.String(), "<script>") {
		t.Fatalf("página do 421: %d %q", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "/api/public", nil)
	r.Host = "attacker.example.com"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 421 || !strings.Contains(w.Header().Get("Content-Type"), "json") {
		t.Fatalf("API no 421: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
}
