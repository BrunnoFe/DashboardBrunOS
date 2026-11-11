package runtipi

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A1: instalada × loja baixada, pelo tipi_version; _user e apps sem loja ficam de fora.
func TestCheckUpdates(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "apps/migrated/plex/config.json"), `{"name":"Plex","version":"1.41.0","tipi_version":10}`)
	write(t, filepath.Join(dir, "repos/migrated/apps/plex/config.json"), `{"name":"Plex","version":"1.42.1","tipi_version":12}`)
	write(t, filepath.Join(dir, "apps/personal-store/brunos/config.json"), `{"name":"BrunOS","version":"0.10.1","tipi_version":15}`)
	write(t, filepath.Join(dir, "repos/personal-store/apps/brunos/config.json"), `{"name":"BrunOS","version":"0.10.1","tipi_version":15}`)
	write(t, filepath.Join(dir, "apps/_user/meuapp/config.json"), `{"name":"Meu","version":"1","tipi_version":1}`)
	write(t, filepath.Join(dir, "apps/migrated/sumiu/config.json"), `{"name":"Sumiu","version":"1","tipi_version":3}`) // fora da loja
	up := New(dir).CheckUpdates()
	if len(up) != 1 || up[0].URN != "plex:migrated" || up[0].Installed != "1.41.0" || up[0].Available != "1.42.1" {
		t.Fatalf("atualizações: %+v", up)
	}
	if got := New("").CheckUpdates(); len(got) != 0 {
		t.Fatal("sem pasta do Runtipi: lista vazia")
	}
}
