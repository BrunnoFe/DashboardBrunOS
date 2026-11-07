package events

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDedupeAndResolve(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "events.json"))
	if !s.Emit(Event{Kind: "disk-unmounted", Level: Critico, Title: "HD sumiu", Key: "disk:a"}) {
		t.Fatal("primeiro aviso deveria entrar")
	}
	if s.Emit(Event{Kind: "disk-unmounted", Level: Critico, Title: "HD sumiu", Key: "disk:a"}) {
		t.Fatal("aviso repetido com o problema aberto não deveria entrar")
	}
	if !s.IsActive("disk:a") {
		t.Fatal("problema deveria estar aberto")
	}
	if !s.Resolve("disk:a", &Event{Kind: "disk-mounted", Title: "HD voltou"}) {
		t.Fatal("resolve deveria achar o problema aberto")
	}
	if s.Resolve("disk:a", &Event{Title: "de novo"}) {
		t.Fatal("resolver duas vezes não deveria registrar outro aviso")
	}
	list, unread := s.List()
	if len(list) != 2 || unread != 2 || list[0].Title != "HD voltou" || list[0].Key != "" {
		t.Fatalf("lista inesperada: %+v (não lidos %d)", list, unread)
	}
	// depois de resolvido, o mesmo problema avisa de novo
	if !s.Emit(Event{Title: "HD sumiu de novo", Key: "disk:a"}) {
		t.Fatal("problema novo deveria avisar")
	}
}

func TestPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	s := New(path)
	s.Emit(Event{Title: "um", Key: "k"})
	s.Emit(Event{Title: "dois"})
	list, _ := s.List()
	s.MarkRead([]string{list[0].ID})

	s2 := New(path)
	list2, unread := s2.List()
	if len(list2) != 2 || unread != 1 || !list2[0].Read {
		t.Fatalf("não recarregou do disco: %+v", list2)
	}
	if s2.Emit(Event{Title: "um", Key: "k"}) {
		t.Fatal("problema aberto deveria continuar lembrado depois de reiniciar")
	}
	s2.Clear()
	if l, _ := s2.List(); len(l) != 0 {
		t.Fatal("clear deveria esvaziar")
	}
	if !New(path).IsActive("k") {
		t.Fatal("clear não deveria esquecer problemas abertos")
	}
}

func TestRotation(t *testing.T) {
	s := New("")
	now := time.Unix(1_800_000_000, 0)
	s.Now = func() time.Time { return now }
	s.Emit(Event{Title: "velho"})
	now = now.Add(31 * 24 * time.Hour)
	for i := 0; i < maxEvents+20; i++ {
		s.Emit(Event{Title: "novo"})
	}
	list, _ := s.List()
	if len(list) != maxEvents {
		t.Fatalf("deveria manter %d, ficou %d", maxEvents, len(list))
	}
	for _, e := range list {
		if e.Title == "velho" {
			t.Fatal("aviso com mais de 30 dias deveria sair")
		}
	}
}

func TestSubscribe(t *testing.T) {
	s := New("")
	ch, stop := s.Subscribe()
	defer stop()
	s.Emit(Event{Title: "oi", Level: Aviso})
	select {
	case e := <-ch:
		if e.Title != "oi" || e.ID == "" || e.Time == 0 {
			t.Fatalf("evento incompleto: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("ouvinte não recebeu")
	}
}
