package integrations

// Destinos proibidos para as integrações (auditoria de 19/11/2026). O painel PRECISA falar com os apps
// da rede (IPs 192.168.x, 10.x, 172.x, 100.64.x do Tailscale), então não dá para bloquear rede privada.
// Fica proibido só o que nunca é um app de verdade e daria poder demais a quem escolhe o endereço:
//   - o próprio container (127.0.0.0/8, ::1);
//   - endereços "link-local" (169.254.x, onde ficam os metadados de máquinas na nuvem, e fe80::);
//   - 0.0.0.0, multicast;
//   - o proxy do Docker (BlockedHosts, preenchido pelo main): por ele dá para parar e ligar containers.
// A conferência é feita no IP JÁ RESOLVIDO, na hora de conectar: um nome que muda de IP no meio
// (DNS rebinding) não escapa.

import (
	"errors"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"
)

// BlockedHosts devolve os nomes (ou IPs) que as integrações nunca podem alcançar.
var BlockedHosts = func() []string { return nil }

// allowLoopback só para os testes, cujos servidores falsos ficam em 127.0.0.1.
var allowLoopback = false

var errBlockedAddr = errors.New("endereço não permitido para integrações")

var blocked struct {
	mu  sync.Mutex
	at  time.Time
	ips []net.IP
}

// blockedIPs resolve BlockedHosts no máximo a cada minuto (o IP do proxy muda se o container recriar).
func blockedIPs() []net.IP {
	blocked.mu.Lock()
	defer blocked.mu.Unlock()
	if time.Since(blocked.at) < time.Minute {
		return blocked.ips
	}
	var ips []net.IP
	for _, h := range BlockedHosts() {
		if ip := net.ParseIP(h); ip != nil {
			ips = append(ips, ip)
			continue
		}
		if addrs, err := net.LookupIP(h); err == nil {
			ips = append(ips, addrs...)
		}
	}
	blocked.ips, blocked.at = ips, time.Now()
	return ips
}

func blockedIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsLoopback() && !allowLoopback {
		return true
	}
	for _, b := range blockedIPs() {
		if b.Equal(ip) {
			return true
		}
	}
	return false
}

// dialControl roda depois da resolução do nome, antes de conectar.
func dialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil || blockedIP(net.ParseIP(host)) {
		return errBlockedAddr
	}
	return nil
}

// safeDialer é o Dialer de toda conexão das integrações (HTTP e Tomada Tuya).
func safeDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second, Control: dialControl}
}

func safeTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil // nenhum proxy de ambiente desviando o pedido
	t.DialContext = safeDialer(10 * time.Second).DialContext
	return t
}
