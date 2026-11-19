package access

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// traefikFake imita o Traefik: responde pelo nome pedido (Host), com a página 404 padrão para nomes
// desconhecidos.
func traefikFake(t *testing.T) (ip string, port int) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "ok.server.lan":
			w.WriteHeader(200)
		case "login.server.lan":
			w.WriteHeader(401)
		case "app404.server.lan":
			http.Error(w, "Página não existe", 404) // 404 do próprio app: o Domínio funciona
		case "parado.server.lan":
			w.WriteHeader(502)
		default:
			http.NotFound(w, r) // "404 page not found", como o Traefik
		}
	}))
	t.Cleanup(srv.Close)
	h, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ = strconv.Atoi(p)
	return h, port
}

func TestCheck(t *testing.T) {
	ip, port := traefikFake(t)
	ctx := context.Background()
	for domain, want := range map[string]string{
		"ok.server.lan": Up, "login.server.lan": Up, "app404.server.lan": Up,
		"parado.server.lan": Down, "desconhecido.server.lan": Down,
	} {
		if got := Check(ctx, ip, port, domain); got != want {
			t.Errorf("%s: %q, queria %q", domain, got, want)
		}
	}
	if got := Check(ctx, "127.0.0.1", 1, "ok.server.lan"); got != Down {
		t.Errorf("porta fechada: %q", got)
	}
	if got := Check(ctx, ip, port, ""); got != Unknown {
		t.Errorf("sem domínio: %q", got)
	}
}

func TestCache(t *testing.T) {
	c := NewCache()
	t0 := time.Unix(1_000_000, 0)
	if !c.Need("a", "a.lan", t0) {
		t.Fatal("app novo precisa de teste")
	}
	c.Set("a", "a.lan", Up, t0)
	if c.Need("a", "a.lan", t0.Add(TTL-time.Second)) || !c.Need("a", "a.lan", t0.Add(TTL)) {
		t.Error("Up vale por TTL")
	}
	if !c.Need("a", "b.lan", t0) {
		t.Error("domínio trocado precisa de teste")
	}
	c.Set("a", "a.lan", Down, t0)
	if !c.Need("a", "a.lan", t0.Add(RetryTTL)) {
		t.Error("Down tenta de novo em RetryTTL")
	}
	if c.Get("a") != Down || c.Get("x") != Unknown {
		t.Error("Get")
	}
	c.Forget(map[string]bool{})
	if c.Get("a") != Unknown {
		t.Error("Forget")
	}
}
