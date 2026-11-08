package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	b, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != keySize {
		t.Fatalf("chave com %d bytes", info.Size())
	}

	sealed, err := b.Seal("token-super-secreto")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "token") {
		t.Fatal("texto cifrado contém o original")
	}
	again, _ := b.Seal("token-super-secreto")
	if again == sealed {
		t.Fatal("dois Seal iguais: nonce repetido")
	}

	// recarregar a mesma chave decifra
	b2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := b2.Open(sealed)
	if err != nil || plain != "token-super-secreto" {
		t.Fatalf("Open = %q, %v", plain, err)
	}

	if s, _ := b.Seal(""); s != "" {
		t.Fatal("vazio deveria continuar vazio")
	}
	if p, err := b.Open(""); p != "" || err != nil {
		t.Fatal("Open vazio")
	}
}

func TestWrongKey(t *testing.T) {
	a, _ := New(make([]byte, keySize))
	k := make([]byte, keySize)
	k[0] = 1
	b, _ := New(k)
	sealed, _ := a.Seal("x")
	if _, err := b.Open(sealed); err != ErrDecrypt {
		t.Fatalf("esperava ErrDecrypt, veio %v", err)
	}
	for _, bad := range []string{"!!!", "AAAA", sealed[:len(sealed)-4]} {
		if _, err := a.Open(bad); err != ErrDecrypt {
			t.Fatalf("Open(%q) = %v", bad, err)
		}
	}
}

func TestBadKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	if err := os.WriteFile(path, []byte("curta"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("chave curta deveria falhar")
	}
}
