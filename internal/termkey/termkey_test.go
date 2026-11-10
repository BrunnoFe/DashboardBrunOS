package termkey

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "terminal_key.json")
	v := New(p)
	if v.Exists() || v.Info("s1").Exists {
		t.Fatal("cofre novo deveria estar vazio")
	}
	in, err := v.Create("s1", "senha-do-brunos")
	if err != nil {
		t.Fatal(err)
	}
	if !in.Exists || !in.Unlocked || !strings.HasPrefix(in.Public, "ssh-ed25519 ") || !strings.HasSuffix(in.Public, " "+Comment) {
		t.Fatalf("info inesperada: %+v", in)
	}
	if _, err := v.Create("s1", "x"); !errors.Is(err, ErrExists) {
		t.Fatal("não deveria criar duas chaves")
	}
	// o arquivo não pode ter a chave em texto puro
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "PRIVATE KEY") {
		t.Fatal("chave privada gravada sem cifrar")
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("permissão %v", st.Mode().Perm())
	}
	if v.Signer("s2") != nil {
		t.Fatal("outra Sessão não deveria ter a chave aberta")
	}

	// reinício: nada aberto; senha errada recusa; certa abre
	v2 := New(p)
	if v2.Signer("s1") != nil {
		t.Fatal("depois de reiniciar a chave deveria estar trancada")
	}
	if err := v2.Unlock("s1", "errada"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("senha errada: %v", err)
	}
	if err := v2.Unlock("s1", "senha-do-brunos"); err != nil {
		t.Fatal(err)
	}
	s := v2.Signer("s1")
	if s == nil || !strings.Contains(in.Public, strings.TrimSpace(strings.SplitN(in.Public, " ", 3)[1])) {
		t.Fatal("deveria abrir")
	}

	// trocar a senha: a antiga para de funcionar, a nova abre a mesma chave
	if err := v2.Rekey("senha-do-brunos", "senha-nova-123"); err != nil {
		t.Fatal(err)
	}
	v3 := New(p)
	if err := v3.Unlock("s9", "senha-do-brunos"); !errors.Is(err, ErrWrongPassword) {
		t.Fatal("a senha antiga não deveria abrir mais")
	}
	if err := v3.Unlock("s9", "senha-nova-123"); err != nil {
		t.Fatal(err)
	}
	if string(v3.Signer("s9").PublicKey().Marshal()) != string(s.PublicKey().Marshal()) {
		t.Fatal("a chave deveria continuar a mesma")
	}
	v3.Forget("s9")
	if v3.Signer("s9") != nil {
		t.Fatal("Forget deveria trancar")
	}
	if err := v3.Delete(); err != nil || v3.Exists() {
		t.Fatal("Delete deveria apagar")
	}
	if err := v3.Unlock("s1", "senha-nova-123"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("sem chave: %v", err)
	}
}
