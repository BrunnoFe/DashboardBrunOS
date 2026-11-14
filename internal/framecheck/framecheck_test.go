package framecheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestVerdict(t *testing.T) {
	cases := []struct {
		name string
		h    map[string][]string
		want string
	}{
		{"sem cabeçalhos", nil, OK},
		{"XFO DENY", map[string][]string{"X-Frame-Options": {"DENY"}}, Refuse},
		{"XFO sameorigin minúsculo", map[string][]string{"X-Frame-Options": {"sameorigin"}}, Refuse},
		{"XFO duplicado com vírgula", map[string][]string{"X-Frame-Options": {"SAMEORIGIN, SAMEORIGIN"}}, Refuse},
		{"XFO ALLOW-FROM é ignorado", map[string][]string{"X-Frame-Options": {"ALLOW-FROM https://x"}}, OK},
		{"XFO inválido é ignorado", map[string][]string{"X-Frame-Options": {"bobagem"}}, OK},
		{"CSP sem frame-ancestors", map[string][]string{"Content-Security-Policy": {"default-src 'self'"}}, OK},
		{"CSP frame-ancestors none", map[string][]string{"Content-Security-Policy": {"default-src 'self'; frame-ancestors 'none'"}}, Refuse},
		{"CSP frame-ancestors self", map[string][]string{"Content-Security-Policy": {"frame-ancestors 'self'"}}, Refuse},
		{"CSP frame-ancestors com endereço", map[string][]string{"Content-Security-Policy": {"frame-ancestors https://painel.exemplo"}}, Refuse},
		{"CSP frame-ancestors *", map[string][]string{"Content-Security-Policy": {"script-src 'self'; Frame-Ancestors *"}}, OK},
		{"CSP frame-ancestors http: https:", map[string][]string{"Content-Security-Policy": {"frame-ancestors http: https:"}}, OK},
		{"CSP * vence XFO", map[string][]string{"Content-Security-Policy": {"frame-ancestors *"}, "X-Frame-Options": {"DENY"}}, OK},
		{"duas CSP, uma recusa", map[string][]string{"Content-Security-Policy": {"frame-ancestors *", "frame-ancestors 'self'"}}, Refuse},
		{"CSP só de relatório não conta", map[string][]string{"Content-Security-Policy-Report-Only": {"frame-ancestors 'none'"}}, OK},
	}
	for _, c := range cases {
		h := http.Header{}
		for k, vs := range c.h {
			for _, v := range vs {
				h.Add(k, v)
			}
		}
		if got := Verdict(h); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func serve(t *testing.T, h http.HandlerFunc) (string, int) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	host, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(p)
	return host, port
}

func TestCheckFollowsSameHostRedirect(t *testing.T) {
	host, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Write([]byte("login"))
	})
	if got := Check(context.Background(), Client, host, port); got != Refuse {
		t.Fatalf("got %q", got)
	}
}

func TestCheckExternalRedirectIsUnknown(t *testing.T) {
	host, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://login.exemplo/", http.StatusFound)
	})
	if got := Check(context.Background(), Client, host, port); got != Unknown {
		t.Fatalf("got %q", got)
	}
}

func TestCheckOK(t *testing.T) {
	host, port := serve(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("oi")) })
	if got := Check(context.Background(), Client, host, port); got != OK {
		t.Fatalf("got %q", got)
	}
}

func TestCheckClosedPortIsUnknown(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	_, p, _ := net.SplitHostPort(l.Addr().String())
	l.Close()
	port, _ := strconv.Atoi(p)
	if got := Check(context.Background(), Client, "127.0.0.1", port); got != Unknown {
		t.Fatalf("got %q", got)
	}
}

func TestCache(t *testing.T) {
	c := NewCache()
	now := time.Unix(1_000_000, 0)
	if !c.Need("a", "1", now) {
		t.Fatal("app novo precisa de checagem")
	}
	c.Set("a", "1", Refuse, now)
	if c.Need("a", "1", now.Add(time.Hour)) {
		t.Fatal("veredito recente não precisa de checagem")
	}
	if !c.Need("a", "2", now.Add(time.Minute)) {
		t.Fatal("versão nova precisa de checagem")
	}
	if !c.Need("a", "1", now.Add(KnownTTL)) {
		t.Fatal("veredito vencido precisa de checagem")
	}
	// "não deu para saber" mantém o veredito anterior, mas pergunta de novo em 5 min
	c.Set("a", "1", Unknown, now.Add(KnownTTL))
	if c.Get("a") != Refuse {
		t.Fatalf("got %q", c.Get("a"))
	}
	if c.Need("a", "1", now.Add(KnownTTL+time.Minute)) || !c.Need("a", "1", now.Add(KnownTTL+UnknownTTL)) {
		t.Fatal("tempo do Unknown errado")
	}
	c.Set("b", "1", Unknown, now)
	if c.Need("b", "1", now.Add(time.Minute)) || !c.Need("b", "1", now.Add(UnknownTTL)) {
		t.Fatal("Unknown novo deve voltar em 5 min")
	}
	c.Forget(map[string]bool{"b": true})
	if c.Get("a") != Unknown || !c.Need("a", "1", now) {
		t.Fatal("Forget não apagou")
	}
}
