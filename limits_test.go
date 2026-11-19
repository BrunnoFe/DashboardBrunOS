package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func withBodyTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := bodyTimeout
	bodyTimeout = d
	t.Cleanup(func() { bodyTimeout = old })
}

// Corpo que nunca termina de chegar (sem login, conexão lenta) é cortado no prazo, em vez de prender
// a conexão para sempre.
func TestBodyDeadlineCutsSlowBody(t *testing.T) {
	withBodyTimeout(t, 300*time.Millisecond)
	ts := httptest.NewServer(bodyDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "lento", 400)
			return
		}
		w.Write([]byte("ok"))
	})))
	defer ts.Close()
	c, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("POST /api/auth/login HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\n{"))
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("o servidor continuou esperando o corpo depois do prazo")
	}
	if err == nil && !strings.Contains(line, "400") {
		t.Fatalf("esperava 400 pelo corpo lento, veio %q", line)
	}
}

// Depois que o corpo chega, o prazo sai: um pedido que demora para responder (instalar um app) não é
// cancelado.
func TestBodyDeadlineClearedAfterBody(t *testing.T) {
	withBodyTimeout(t, 200*time.Millisecond)
	ts := httptest.NewServer(bodyDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		time.Sleep(700 * time.Millisecond)
		if r.Context().Err() != nil {
			http.Error(w, "cancelado", 500)
			return
		}
		w.Write([]byte("ok"))
	})))
	defer ts.Close()
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "ok" {
		t.Fatalf("pedido longo foi cancelado: %d %q", resp.StatusCode, b)
	}
}

// Envio de arquivo: o prazo anda junto com os dados (pedaços chegando devagar, mas sem parar).
func TestBodyDeadlineSlidesOnUpload(t *testing.T) {
	withBodyTimeout(t, 300*time.Millisecond)
	ts := httptest.NewServer(bodyDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Write([]byte(strings.Repeat("x", len(b))))
	})))
	defer ts.Close()
	pr, pw := io.Pipe()
	go func() {
		for range 5 { // 5 pedaços a cada 150 ms: 750 ms no total, mais que o prazo de 300 ms
			pw.Write([]byte("abc"))
			time.Sleep(150 * time.Millisecond)
		}
		pw.Close()
	}()
	req, _ := http.NewRequest("PUT", ts.URL+"/api/files/upload/chunk?id=x", pr)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(b) != 15 {
		t.Fatalf("envio lento mas contínuo foi cortado: %d %q", resp.StatusCode, b)
	}
}

// Pedidos sem corpo (os SSE) não ganham prazo de leitura.
func TestBodyDeadlineIgnoresGET(t *testing.T) {
	withBodyTimeout(t, 200*time.Millisecond)
	ts := httptest.NewServer(bodyDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(600 * time.Millisecond)
		if r.Context().Err() != nil {
			http.Error(w, "cancelado", 500)
			return
		}
		w.Write([]byte("ok"))
	})))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/stats/stream")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "ok" {
		t.Fatalf("GET foi cancelado: %q", b)
	}
}

// O teto de conexões segura as próximas até alguma fechar.
func TestLimitListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := newLimitListener(ln, 2)
	defer l.Close()
	var accepted atomic.Int32
	conns := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			conns <- c
		}
	}()
	for range 3 {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
	}
	time.Sleep(200 * time.Millisecond)
	if n := accepted.Load(); n != 2 {
		t.Fatalf("aceitou %d conexões; o teto é 2", n)
	}
	(<-conns).Close()
	time.Sleep(200 * time.Millisecond)
	if n := accepted.Load(); n != 3 {
		t.Fatalf("depois de fechar uma, a fila deveria andar (aceitou %d)", n)
	}
}
