package auth

import (
	"errors"
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
