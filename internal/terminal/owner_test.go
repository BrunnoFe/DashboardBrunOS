package terminal

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// Abas por Sessão do BrunOS (revisão de segurança, lote 4): uma Sessão não lê nem digita na Aba de outra.
func TestAbasPorSessao(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "known.json"))
	now := time.Now()
	m.sessions["a1"] = &Session{ID: "a1", Addr: "srv:22", User: "u", Owner: "sessA", Created: now, running: []string{"htop"}}
	m.sessions["b1"] = &Session{ID: "b1", Addr: "srv:22", User: "u", Owner: "sessB", Created: now.Add(time.Second), running: []string{"senha-no-comando"}}

	if _, err := m.GetFor("a1", "sessA"); err != nil {
		t.Fatalf("a dona não achou a própria Aba: %v", err)
	}
	if _, err := m.GetFor("b1", "sessA"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Aba de outra Sessão deveria parecer inexistente, veio %v", err)
	}

	list := m.ListFor("sessA")
	if len(list) != 2 || list[0].ID != "a1" || !list[0].Mine || list[1].Mine {
		t.Fatalf("lista errada: %+v", list)
	}
	if list[1].Running != nil {
		t.Fatalf("a lista mostrou o que roda na Aba de outra Sessão: %+v", list[1])
	}

	if err := m.Adopt("b1", "sessA"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetFor("b1", "sessA"); err != nil {
		t.Fatalf("depois de trazer a Aba, a nova dona não achou: %v", err)
	}
	if _, err := m.GetFor("b1", "sessB"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a dona antiga continuou com a Aba: %v", err)
	}
	if err := m.Adopt("nao-existe", "sessA"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("trazer Aba inexistente: %v", err)
	}
}
