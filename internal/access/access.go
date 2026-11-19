// Package access testa a Forma de acesso Domínio de um app do Runtipi (0.26.0, decisão 65).
//
// Um app pode ser aberto pelo Domínio (`https://app.server.lan`, que passa pelo Traefik do Runtipi) ou
// pela Porta (`server.lan:8384`). A Porta o vigia de apps já testa a cada minuto; aqui fica o teste do
// Domínio, feito a cada ~10 min: o BrunOS conecta no Traefik (IP do servidor, porta https) dizendo o nome
// do app, sem depender do DNS de dentro do container, e vê se o Traefik entrega o pedido ao app.
package access

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	Up      = "up"   // o Traefik entregou o pedido ao app
	Down    = "down" // o Traefik não conhece o nome ou não alcança o app
	Unknown = ""     // ainda não testado
)

// Tempos entre um teste e outro do mesmo app.
const (
	TTL      = 10 * time.Minute
	RetryTTL = 2 * time.Minute // depois de uma falha (o app pode estar só reiniciando)
)

// traefik404 é o corpo da página que o Traefik devolve quando nenhum app usa aquele nome.
const traefik404 = "404 page not found"

// Check pergunta ao Traefik em ip:port (https) pelo endereço https://domain/. Conta como Up qualquer
// resposta do próprio app (até 401, 403 ou um 404 dele); Down quando o Traefik responde que não conhece o
// nome (404 com a página padrão dele), não alcança o app (502, 503, 504) ou nem responde.
func Check(ctx context.Context, ip string, port int, domain string) string {
	if ip == "" || domain == "" || port <= 0 {
		return Unknown
	}
	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	tr := &http.Transport{
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 8 * time.Second,
		// O certificado do domínio local é o do próprio Runtipi (não é de uma autoridade conhecida). Aqui só
		// se confere se o Traefik entrega o pedido ao app: nenhum dado sensível é enviado nem lido.
		TLSClientConfig: &tls.Config{ServerName: domain, InsecureSkipVerify: true}, //nolint:gosec // teste de rota, sem dados
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 4 * time.Second}
			return d.DialContext(ctx, network, addr) // sempre o Traefik, sem DNS
		},
	}
	c := &http.Client{
		Timeout:       10 * time.Second,
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/", nil)
	if err != nil {
		return Unknown
	}
	req.Header.Set("User-Agent", "BrunOS-monitor")
	resp, err := c.Do(req)
	if err != nil {
		return Down
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return Down
	case http.StatusNotFound:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		if strings.TrimSpace(string(b)) == traefik404 {
			return Down
		}
	}
	return Up
}

type entry struct {
	status string
	domain string
	at     time.Time
}

// Cache guarda o resultado de cada app (por URN), só em memória.
type Cache struct {
	mu sync.Mutex
	m  map[string]entry
}

func NewCache() *Cache { return &Cache{m: map[string]entry{}} }

// Need diz se o Domínio do app precisa ser testado de novo.
func (c *Cache) Need(urn, domain string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[urn]
	if !ok || e.domain != domain {
		return true
	}
	ttl := TTL
	if e.status != Up {
		ttl = RetryTTL
	}
	return now.Sub(e.at) >= ttl
}

// Set guarda o resultado do teste.
func (c *Cache) Set(urn, domain, status string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[urn] = entry{status: status, domain: domain, at: now}
}

// Get devolve o último resultado ("" quando ainda não testou).
func (c *Cache) Get(urn string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[urn].status
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
