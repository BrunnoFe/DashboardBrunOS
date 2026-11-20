package main

// Revisão de segurança de 19/11/2026 (lote 4, escolhas do Bruno):
//
//   - Proxies confiáveis: o IP do visitante que vem no X-Forwarded-For (e o X-Forwarded-Proto) só vale
//     quando o pedido chega do Traefik do Runtipi. Antes valia de qualquer IP da rede local, e qualquer
//     aparelho podia "mentir" o IP para errar a senha à vontade. Nomes/IPs/faixas em
//     BRUNOS_TRUSTED_PROXIES (padrão: os nomes do container do Traefik do Runtipi).
//
//   - Nomes de endereço aceitos: o BrunOS só responde aos endereços esperados (barra o "DNS rebinding",
//     em que um site de fora faz o próprio nome apontar para o servidor). Entram sozinhos: qualquer IP,
//     nomes sem ponto (localhost, server), terminações de rede de casa (.local, .lan, .home, .internal,
//     .home.arpa, .localdomain) e do Tailscale (.ts.net), o domínio local do Runtipi e os domínios do
//     próprio app BrunOS. Outros: Configurações → Sistema → Endereços aceitos, ou BRUNOS_HOSTS.

import (
	"context"
	"html"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ── proxies confiáveis ───────────────────────────────────────────────────────────────────────────────

var trusted struct {
	mu   sync.Mutex
	at   time.Time
	ips  []net.IP
	nets []*net.IPNet
}

// trustedSpec: o que está em BRUNOS_TRUSTED_PROXIES (nomes, IPs ou faixas CIDR, separados por vírgula).
func trustedSpec() []string {
	v := env("BRUNOS_TRUSTED_PROXIES", "runtipi-reverse-proxy,tipi-reverse-proxy,traefik")
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// trustedProxy diz se o remetente da conexão é um proxy confiável (resolve os nomes a cada minuto: o IP
// do container do Traefik muda quando ele é recriado).
func trustedProxy(ip net.IP) bool {
	if ip == nil {
		return false
	}
	trusted.mu.Lock()
	if time.Since(trusted.at) > time.Minute {
		trusted.ips, trusted.nets = nil, nil
		// DNS lento (nome que não existe nesta rede) não pode segurar os pedidos: no máximo 2 s por minuto
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		for _, p := range trustedSpec() {
			if _, n, err := net.ParseCIDR(p); err == nil {
				trusted.nets = append(trusted.nets, n)
			} else if x := net.ParseIP(p); x != nil {
				trusted.ips = append(trusted.ips, x)
			} else if addrs, err := net.DefaultResolver.LookupIPAddr(ctx, p); err == nil {
				for _, a := range addrs {
					trusted.ips = append(trusted.ips, a.IP)
				}
			}
		}
		cancel()
		trusted.at = time.Now()
	}
	ips, nets := trusted.ips, trusted.nets
	trusted.mu.Unlock()
	for _, x := range ips {
		if x.Equal(ip) {
			return true
		}
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// remoteIP é o IP de quem abriu a conexão TCP.
func remoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// ── nomes de endereço aceitos ────────────────────────────────────────────────────────────────────────

// homeSuffixes: terminações que só existem dentro de casa ou do Tailscale (ninguém de fora registra).
var homeSuffixes = []string{".local", ".lan", ".home", ".internal", ".home.arpa", ".localdomain", ".localhost", ".ts.net"}

type hostList struct {
	mu    sync.Mutex
	at    time.Time
	exact map[string]bool
	under []string // domínios cujos subdomínios valem (ex.: o domínio local do Runtipi)
}

func normHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if x, _, err := net.SplitHostPort(h); err == nil {
		h = x
	}
	h = strings.TrimSuffix(strings.Trim(h, "[]"), ".")
	return h
}

// splitHosts lê uma lista de endereços digitada (vírgula, espaço ou linha), sem porta nem http://.
func splitHosts(v string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == ';' }) {
		p = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(p), "https://"), "http://")
		p, _, _ = strings.Cut(p, "/")
		if p = normHost(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// refreshHosts monta a lista do momento (a cada 30 s, ou quando as preferências mudam).
func (s *server) refreshHosts(force bool) (map[string]bool, []string) {
	s.hosts.mu.Lock()
	defer s.hosts.mu.Unlock()
	if !force && s.hosts.exact != nil && time.Since(s.hosts.at) < 30*time.Second {
		return s.hosts.exact, s.hosts.under
	}
	exact, under := map[string]bool{}, []string{}
	for _, h := range splitHosts(os.Getenv("BRUNOS_HOSTS")) {
		exact[h] = true
	}
	if v, ok := s.readSettings()["allowedHosts"].(string); ok {
		for _, h := range splitHosts(v) {
			exact[h] = true
		}
	}
	apps, info := s.tipi.Apps()
	if d := normHost(info.LocalDomain); d != "" {
		under = append(under, d)
	}
	for _, a := range apps {
		if a.ID == selfApp {
			for _, d := range []string{a.LocalDomain, a.ExposedDomain} {
				if d = normHost(d); d != "" {
					exact[d] = true
				}
			}
		}
	}
	s.hosts.exact, s.hosts.under, s.hosts.at = exact, under, time.Now()
	return exact, under
}

// hostOK diz se o BrunOS deve responder a este nome de endereço.
func (s *server) hostOK(raw string) bool {
	h := normHost(raw)
	if h == "" || net.ParseIP(h) != nil || !strings.Contains(h, ".") {
		return true // sem nome, IP, ou nome sem ponto (localhost, server): não dá para um site de fora usar
	}
	for _, suf := range homeSuffixes {
		if strings.HasSuffix(h, suf) {
			return true
		}
	}
	exact, under := s.refreshHosts(false)
	if exact[h] {
		return true
	}
	for _, d := range under {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}

func (s *server) hostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.hostOK(r.Host) {
			next.ServeHTTP(w, r)
			return
		}
		msg := "Endereço não reconhecido pelo BrunOS: " + normHost(r.Host) + ". Para usar este endereço, abra o BrunOS pelo IP do servidor e acrescente-o em Configurações → Sistema → Endereços aceitos."
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusMisdirectedRequest, map[string]string{"error": msg})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusMisdirectedRequest)
		_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>BrunOS</title>` +
			`<body style="font-family:system-ui,sans-serif;max-width:560px;margin:15vh auto;padding:0 16px;line-height:1.5">` +
			`<h2>Endereço não reconhecido</h2><p>` + html.EscapeString(msg) + `</p></body>`))
	})
}
