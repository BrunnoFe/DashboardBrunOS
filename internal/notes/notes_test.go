package notes

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.json")
	s := New(p)
	ch, stop := s.Subscribe()
	defer stop()
	if _, ok := s.Get("w1"); ok {
		t.Fatal("nota nova não existe")
	}
	n, err := s.Put("w1", "[ ] pão\n[x] café")
	if err != nil || n.At == 0 {
		t.Fatal(err)
	}
	if c := <-ch; c.ID != "w1" || c.At != n.At {
		t.Fatalf("aviso inesperado %+v", c)
	}
	if got, _ := New(p).Get("w1"); got.Text != "[ ] pão\n[x] café" {
		t.Fatalf("não gravou: %q", got.Text)
	}
	if _, err := s.Put("w1", strings.Repeat("á", MaxChars+1)); !errors.Is(err, ErrTooLong) {
		t.Fatal("deveria recusar nota grande demais")
	}
	if _, err := s.Put("../x", "a"); !errors.Is(err, ErrBadNote) {
		t.Fatal("id inválido")
	}
	if err := s.Delete("w1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("w1"); ok {
		t.Fatal("deveria apagar")
	}
}
