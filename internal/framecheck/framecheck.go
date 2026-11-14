// Package framecheck descobre se um app aceita ser mostrado dentro de outra página (Janela de app).
//
// Cada app decide isso pelos cabeçalhos X-Frame-Options e Content-Security-Policy (frame-ancestors), e o
// navegador não conta para a página quando um iframe foi recusado. Por isso o BrunOS pergunta ao app de
// vez em quando (um GET na raiz, só cabeçalhos) e guarda a resposta: "ok" (abre em janela), "no" (Só em
// aba) ou "" (não deu para saber: a interface tenta a janela). Decisão 58.
package framecheck

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	OK      = "ok"
	Refuse  = "no"
	Unknown = ""
)

// Verdict lê os cabeçalhos de uma resposta. O app fica em outro endereço (outra porta ou domínio) que o
// BrunOS, então "SAMEORIGIN" também recusa. Quando há frame-ancestors na CSP, os navegadores ignoram o
// X-Frame-Options; endereços listados um a um contam como recusa (não dá para saber de antemão por qual
// endereço o Bruno abre o BrunOS).
func Verdict(h http.Header) string {
	found := false
	for _, policy := range h.Values("Content-Security-Policy") {
		for _, dir := range strings.Split(policy, ";") {
			f := strings.Fields(strings.ToLower(dir))
			if len(f) == 0 || f[0] != "frame-ancestors" {
				continue
			}
			found = true
			if !anySource(f[1:]) {
				return Refuse
			}
		}
	}
	if found {
		return OK
	}
	for _, v := range h.Values("X-Frame-Options") {
		for _, p := range strings.Split(v, ",") {
			switch strings.ToLower(strings.TrimSpace(p)) {
			case "deny", "sameorigin":
				return Refuse
			}
			// ALLOW-FROM e valores inválidos são ignorados pelos navegadores atuais
		}
	}
	return OK
}

// anySource diz se a lista de origens de um frame-ancestors aceita qualquer página http(s).
func anySource(src []string) bool {
	for _, s := range src {
		switch s {
		case "*", "http:", "https:", "http://*", "https://*":
			return true
		}
	}
	return false
}

// Client faz a pergunta ao app: sem seguir redirecionamentos sozinho (o Check segue só os do mesmo
// endereço) e sem guardar conexões.
var Client = &http.Client{
	Timeout:       8 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Transport:     &http.Transport{DisableKeepAlives: true, ResponseHeaderTimeout: 8 * time.Second},
}

// Check pede a raiz do app em host:port e devolve o veredito. Segue até 3 redirecionamentos dentro do
// mesmo endereço (ex.: "/" → "/login"); redirecionamento para outro lugar (um login externo) ou erro de
// rede devolvem Unknown.
func Check(ctx context.Context, c *http.Client, host string, port int) string {
	base := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: "/"}
	u := base
	for i := 0; i < 4; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return Unknown
		}
		req.Header.Set("User-Agent", "BrunOS-monitor")
		resp, err := c.Do(req)
		if err != nil {
			return Unknown
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			return Verdict(resp.Header)
		}
		loc, err := u.Parse(resp.Header.Get("Location"))
		if err != nil || loc.Host != base.Host || loc.Scheme != base.Scheme {
			if v := Verdict(resp.Header); v == Refuse {
				return Refuse // o próprio redirecionamento já recusa
			}
			return Unknown
		}
		u = loc
	}
	return Unknown
}

// Tempos de validade de uma resposta guardada.
const (
	KnownTTL   = 6 * time.Hour   // "ok"/"no" valem por 6 h (ou até o app mudar de versão)
	UnknownTTL = 5 * time.Minute // "não deu para saber": tenta de novo logo
)

type entry struct {
	verdict string
	version string
	at      time.Time
}

// Cache guarda o veredito de cada app (por URN), só em memória: refazer depois de reiniciar é barato.
type Cache struct {
	mu sync.Mutex
	m  map[string]entry
}

func NewCache() *Cache { return &Cache{m: map[string]entry{}} }

// Need diz se o app precisa ser perguntado de novo.
func (c *Cache) Need(urn, version string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[urn]
	if !ok || e.version != version {
		return true
	}
	ttl := KnownTTL
	if e.verdict == Unknown {
		ttl = UnknownTTL
	}
	return now.Sub(e.at) >= ttl
}

// Set guarda o veredito. Um "não deu para saber" não apaga um veredito anterior da mesma versão (o app
// pode só estar reiniciando).
func (c *Cache) Set(urn, version, verdict string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.m[urn]; ok && verdict == Unknown && old.version == version && old.verdict != Unknown {
		old.at = now.Add(UnknownTTL - KnownTTL) // mantém o antigo e pergunta de novo em 5 min
		c.m[urn] = old
		return
	}
	c.m[urn] = entry{verdict: verdict, version: version, at: now}
}

// Get devolve o veredito guardado ("" quando não há).
func (c *Cache) Get(urn string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[urn].verdict
}

// Forget apaga os apps que não existem mais.
func (c *Cache) Forget(alive map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if !alive[k] {
			delete(c.m, k)
		}
	}
}
