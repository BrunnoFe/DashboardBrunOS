package auth

import "testing"

// Código da aba (revisão de segurança, lote 4): sem ele o cookie sozinho não vale; sobrevive ao reinício
// (só o hash fica gravado) e um código novo invalida o anterior.
func TestCodigoDaAba(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	token, err := m.Setup("bruno", "Bruno", "senha-forte-1", "192.168.0.2", "teste")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := m.Validate(token)
	if KeyOK(s, "") || KeyOK(s, "1") {
		t.Fatal("Sessão sem código aceitou pedido sem código")
	}
	key, err := m.IssueKey(s.ID)
	if err != nil || len(key) < 40 {
		t.Fatalf("código: %q %v", key, err)
	}
	s, _ = m.Validate(token)
	if !KeyOK(s, key) || KeyOK(s, key+"x") || KeyOK(s, "") || KeyOK(nil, key) {
		t.Fatal("conferência do código errada")
	}
	if s.Key == key {
		t.Fatal("o código ficou gravado em texto (deveria ser só o hash)")
	}
	m2, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s2, ok := m2.Validate(token)
	if !ok || !KeyOK(s2, key) {
		t.Fatal("o código não sobreviveu ao reinício")
	}
	novo, _ := m2.IssueKey(s2.ID)
	s2, _ = m2.Validate(token)
	if KeyOK(s2, key) || !KeyOK(s2, novo) {
		t.Fatal("o código novo deveria substituir o anterior")
	}
	if _, err := m2.IssueKey("nao-existe"); err == nil {
		t.Fatal("código para Sessão que não existe")
	}
}
