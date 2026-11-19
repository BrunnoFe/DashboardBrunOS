package runtipi

// Loja hostil (auditoria de 19/11/2026): atalhos dentro de apps/ e repos/ não podem ser seguidos.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstalledAppLinksNotFollowed(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "apps", "loja", "mau")
	if err := os.MkdirAll(filepath.Join(app, "metadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(app, "config.json"), []byte(`{"name":"Mau","id":"mau","port":1,"tipi_version":1}`), 0o644)
	segredo := filepath.Join(dir, "segredo.txt")
	os.WriteFile(segredo, []byte("chave"), 0o600)
	if err := os.Symlink(segredo, filepath.Join(app, "metadata", "logo.jpg")); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repos", "loja", "apps", "mau")
	os.MkdirAll(repo, 0o755)
	if err := os.Symlink("/dev/zero", filepath.Join(repo, "config.json")); err != nil {
		t.Fatal(err)
	}

	d := New(dir)
	apps, _ := d.Apps()
	if len(apps) != 1 || apps[0].HasLogo {
		t.Fatalf("logo que é atalho não pode contar como logo: %+v", apps)
	}
	if _, ok := d.LogoPath("mau:loja"); ok {
		t.Fatal("LogoPath não pode entregar um atalho")
	}
	done := make(chan []Update, 1)
	go func() { done <- d.CheckUpdates() }()
	select {
	case u := <-done:
		if len(u) != 0 {
			t.Fatalf("config.json que é atalho não pode virar atualização: %v", u)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CheckUpdates ficou lendo um atalho para /dev/zero")
	}
}
