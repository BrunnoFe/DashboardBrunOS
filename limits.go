package main

// Limites contra conexões lentas ou em excesso (auditoria de 19/11/2026).
//
// O http.Server não tem ReadTimeout: um prazo único para o pedido inteiro mataria os envios grandes
// e as transmissões ao vivo (SSE). Em vez disso:
//   - o CORPO do pedido tem prazo (bodyTimeout). Nos envios de arquivo o prazo anda junto com os dados
//     (cada pedaço que chega dá mais bodyTimeout); nos demais pedidos é um prazo fixo. Quando o corpo
//     termina de chegar, o prazo sai, para não derrubar pedidos que demoram para responder (instalar
//     um app pode levar minutos);
//   - os SSE ganham prazo de escrita a cada envio (sseWriteTimeout): quem para de ler é desligado;
//   - o número de conexões abertas tem teto (maxConns).

import (
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	bodyTimeout     = 60 * time.Second
	sseWriteTimeout = 30 * time.Second
)

const maxConns = 512

// slidingBody: rotas de envio de arquivo, em que o prazo anda junto com os dados.
var slidingBody = map[string]bool{
	"/api/files/upload": true, "/api/files/upload/chunk": true, "/api/files/text": true,
	"/api/wallpapers": true, "/api/icons": true,
}

func bodyDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0 {
			rc := http.NewResponseController(w)
			if rc.SetReadDeadline(time.Now().Add(bodyTimeout)) == nil {
				r.Body = &deadlineBody{ReadCloser: r.Body, rc: rc, slide: slidingBody[r.URL.Path]}
			}
		}
		next.ServeHTTP(w, r)
	})
}

type deadlineBody struct {
	io.ReadCloser
	rc    *http.ResponseController
	slide bool
	once  sync.Once
}

// Read tira o prazo só quando o corpo termina de chegar (EOF). Em erro (prazo estourado) o prazo fica:
// depois do pedido o servidor tenta descartar o resto do corpo, e sem prazo ficaria esperando para sempre.
func (b *deadlineBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	switch {
	case err == io.EOF:
		b.once.Do(func() { _ = b.rc.SetReadDeadline(time.Time{}) })
	case err == nil && n > 0 && b.slide:
		_ = b.rc.SetReadDeadline(time.Now().Add(bodyTimeout))
	}
	return n, err
}

// sseDeadline dá prazo para a próxima escrita de um SSE (chamar antes de cada envio).
func sseDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(sseWriteTimeout))
}

// limitListener aceita no máximo n conexões abertas ao mesmo tempo; as próximas esperam na fila do
// sistema até alguma fechar.
type limitListener struct {
	net.Listener
	sem chan struct{}
}

func newLimitListener(l net.Listener, n int) net.Listener {
	return &limitListener{Listener: l, sem: make(chan struct{}, n)}
}

func (l *limitListener) Accept() (net.Conn, error) {
	l.sem <- struct{}{}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.sem
		return nil, err
	}
	return &limitConn{Conn: c, release: func() { <-l.sem }}, nil
}

type limitConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
