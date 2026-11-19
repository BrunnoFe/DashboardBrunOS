package runtipi

// Receita da loja (auditoria de 19/11/2026): .yml de fachada não esconde o .json, e receita que não dá
// para conferir conta como privilégio.

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRecipeYmlAndJsonSummed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  a:\n    image: x:1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "docker-compose.json"), []byte(`{"services":[{"name":"a","image":"x:1","isMain":true,"privileged":true}]}`), 0o644)
	r, ok := readRecipe(dir, "a")
	if !ok || len(r.Privileges) == 0 {
		t.Fatalf(".json com privileged escondido atrás de um .yml inofensivo: %+v", r)
	}
	if r.Image != "x:1" {
		t.Fatalf("a janela de instalação continua usando o .yml: %+v", r)
	}
}

func TestRecipeUnreadableFailsClosed(t *testing.T) {
	dir := t.TempDir()
	fora := filepath.Join(t.TempDir(), "x.yml")
	os.WriteFile(fora, []byte("services:\n  a:\n    image: x\n"), 0o644)
	if err := os.Symlink(fora, filepath.Join(dir, "docker-compose.yml")); err != nil {
		t.Fatal(err)
	}
	r, ok := readRecipe(dir, "a")
	if !ok || !slices.Contains(r.Privileges, UnreadableRecipe) {
		t.Fatalf("receita que é atalho deveria contar como privilégio: ok=%v %+v", ok, r)
	}
	big := t.TempDir()
	os.WriteFile(filepath.Join(big, "docker-compose.yml"), make([]byte, 300<<10), 0o644)
	if r, ok := readRecipe(big, "a"); !ok || !slices.Contains(r.Privileges, UnreadableRecipe) {
		t.Fatalf("receita grande demais deveria contar como privilégio: ok=%v %+v", ok, r)
	}
	if _, ok := readRecipe(t.TempDir(), "a"); ok {
		t.Fatal("sem receita nenhuma: continua 'não tem receita'")
	}
}
