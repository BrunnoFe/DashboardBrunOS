package integrations

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A tomada só atende uma conexão por vez: pedidos ao mesmo tempo (widgets + Medidor) entram em fila,
// e uma falha ganha mais uma tentativa.
func TestTuyaOneConnectionAtATime(t *testing.T) {
	old := tuyaRetry
	tuyaRetry = 50 * time.Millisecond
	defer func() { tuyaRetry = old }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var now, peak, total atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n := now.Add(1)
			total.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			go func() { // segura um pouco e fecha sem responder (a leitura falha)
				time.Sleep(80 * time.Millisecond)
				now.Add(-1)
				c.Close()
			}()
		}
	}()

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = TuyaStatus(context.Background(), ln.Addr().String(), "dev", "0123456789abcdef", "3.5")
		}()
	}
	wg.Wait()
	for _, e := range errs {
		if e == nil {
			t.Fatal("a tomada falsa não responde: deveria dar erro")
		}
	}
	if p := peak.Load(); p > 1 {
		t.Fatalf("abriu %d conexões ao mesmo tempo (a tomada só aceita uma)", p)
	}
	if n := total.Load(); n != 6 {
		t.Fatalf("esperava 3 pedidos × 2 tentativas = 6 conexões, abriu %d", n)
	}
	// chave errada não tenta de novo nem conecta
	before := total.Load()
	if _, err := TuyaStatus(context.Background(), ln.Addr().String(), "dev", "curta", "3.5"); err == nil || !strings.Contains(err.Error(), "16") {
		t.Fatalf("chave curta: %v", err)
	}
	if total.Load() != before {
		t.Fatal("chave errada não deveria abrir conexão")
	}
}

func TestTuyaRefusedMessage(t *testing.T) {
	old := tuyaRetry
	tuyaRetry = time.Millisecond
	defer func() { tuyaRetry = old }()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close() // porta fechada: conexão recusada
	_, err := TuyaStatus(context.Background(), addr, "dev", "0123456789abcdef", "")
	if err == nil || !strings.Contains(err.Error(), "recusou") {
		t.Fatalf("esperava a mensagem de recusa, veio: %v", err)
	}
}
