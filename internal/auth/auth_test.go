package auth

import (
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// Bloqueio com senha: fica gravado, só a senha certa desbloqueia e errar demais encerra a Sessão.
func TestLockUnlock(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	token, err := m.Setup("bruno", "Bruno", "senha-forte-1", "192.168.0.2", "teste")
	if err != nil {
		t.Fatal(err)
	}
	s, ok := m.Validate(token)
	if !ok || s.ID != SessionID(token) || s.Locked {
		t.Fatalf("sessão inesperada: %+v", s)
	}
	if err := m.Lock(s.ID); err != nil {
		t.Fatal(err)
	}
	if !m.IsLocked(s.ID) {
		t.Fatal("deveria estar bloqueada")
	}
	// recarregar do disco mantém o Bloqueio
	m2, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m2.IsLocked(s.ID) {
		t.Fatal("o Bloqueio deveria sobreviver ao reinício")
	}
	if err := m2.Unlock(s.ID, "errada", "192.168.0.2"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("senha errada: veio %v", err)
	}
	if err := m2.Unlock(s.ID, "senha-forte-1", "192.168.0.2"); err != nil {
		t.Fatal(err)
	}
	if m2.IsLocked(s.ID) {
		t.Fatal("deveria estar desbloqueada")
	}

	// errar 5 vezes: a Sessão acaba
	_ = m2.Lock(s.ID)
	var last error
	for range 5 {
		last = m2.Unlock(s.ID, "errada", "10.0.0.9")
	}
	if !errors.Is(last, ErrSessionEnded) {
		t.Fatalf("depois de 5 erros deveria encerrar, veio %v", last)
	}
	if _, ok := m2.Validate(token); ok {
		t.Fatal("a Sessão deveria ter sido encerrada")
	}
	if !m2.IsLocked("nao-existe") {
		t.Fatal("sessão que não existe conta como bloqueada")
	}
}

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token, err := m.Setup("bruno", "Bruno", "senha-forte-1", "192.168.0.2", "teste")
	if err != nil {
		t.Fatal(err)
	}
	return m, token
}

// Pedidos em paralelo do mesmo IP não passam do limite de 5 conferências (antes, 20 juntos eram
// todos conferidos porque o erro só era contado depois do hash).
func TestLoginParallelLimit(t *testing.T) {
	m, _ := newTestManager(t)
	var wg sync.WaitGroup
	var checked atomic.Int32
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.Login("bruno", "errada", "10.77.0.1", "t"); errors.Is(err, ErrInvalidCredentials) {
				checked.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := checked.Load(); n > maxFails {
		t.Fatalf("%d senhas conferidas em paralelo; o limite é %d", n, maxFails)
	}
	if _, err := m.Login("bruno", "senha-forte-1", "10.77.0.1", "t"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("o IP deveria estar bloqueado, veio %v", err)
	}
}

// Um terceiro que bloqueia o IP do dono (errando de propósito) não derruba a Sessão do dono no
// desbloqueio: a senha certa recebe "aguarde" e a Sessão continua.
func TestUnlockBlockedByOther(t *testing.T) {
	m, token := newTestManager(t)
	id := SessionID(token)
	_ = m.Lock(id)
	for range maxFails {
		_, _ = m.Login("bruno", "errada", "192.168.0.2", "atacante")
	}
	if err := m.Unlock(id, "senha-forte-1", "192.168.0.2"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("IP bloqueado por outro: esperava ErrRateLimited, veio %v", err)
	}
	if _, ok := m.Validate(token); !ok {
		t.Fatal("a Sessão do dono não pode acabar por causa do erro de outra pessoa")
	}
	if err := m.Unlock(id, "senha-forte-1", "192.168.0.3"); err != nil || m.IsLocked(id) {
		t.Fatalf("de outro IP a senha certa desbloqueia: %v", err)
	}
}

// Errar 5 vezes numa Sessão encerra a Sessão, mesmo trocando o IP a cada tentativa.
func TestVerifySessionEndsAfterFails(t *testing.T) {
	m, token := newTestManager(t)
	id := SessionID(token)
	var last error
	for i := range maxSessionFails {
		last = m.VerifySession(id, "chute", "203.0.113."+strconv.Itoa(i))
	}
	if !errors.Is(last, ErrSessionEnded) {
		t.Fatalf("esperava ErrSessionEnded, veio %v", last)
	}
	if _, ok := m.Validate(token); ok {
		t.Fatal("a Sessão deveria ter sido encerrada")
	}
}

// Acertar zera a contagem da Sessão.
func TestVerifySessionResets(t *testing.T) {
	m, token := newTestManager(t)
	id := SessionID(token)
	for range maxSessionFails - 1 {
		_ = m.VerifySession(id, "chute", "10.0.0.1")
	}
	if err := m.VerifySession(id, "senha-forte-1", "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifySession(id, "chute", "10.0.0.3"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("depois de acertar, um erro não encerra: %v", err)
	}
}

// IPs inventados não fazem o mapa do limitador crescer sem fim.
func TestLimiterCap(t *testing.T) {
	l := newLimiter()
	for i := range maxEntries + 500 {
		l.fail("ip-" + strconv.Itoa(i))
	}
	if n := len(l.m); n > maxEntries {
		t.Fatalf("o limitador tem %d entradas; o teto é %d", n, maxEntries)
	}
}
