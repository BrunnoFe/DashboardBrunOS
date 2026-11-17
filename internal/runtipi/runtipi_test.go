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

// Janela Loja (0.18.0): catálogo das lojas baixadas, com o instalado marcado, sem seguir atalhos.
// Loja convertida pelo Garimpo (0.19.0): um ramo do repositório, com garimpo.json dizendo a origem.
func TestCatalogGarimpo(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repos/casaos")
	write(t, filepath.Join(repo, ".git/config"), "[remote \"origin\"]\n\turl = https://github.com/exemplo/Garimpo\n")
	write(t, filepath.Join(repo, ".git/HEAD"), "ref: refs/heads/casaos\n")
	write(t, filepath.Join(repo, "garimpo.json"), `{"brunos":1,"nome":"CasaOS","selo":"via CasaOS","fonte":"https://github.com/IceWhaleTech/CasaOS-AppStore",
		"licenca":"Apache-2.0","sobre":"Apps do CasaOS."}`)
	write(t, filepath.Join(repo, "apps/meuapp/config.json"), `{"name":"Meu App","version":"1","tipi_version":1,"available":true}`)
	// loja comum no ramo principal, com um garimpo.json malfeito (endereço que não é http)
	outra := filepath.Join(dir, "repos/outra")
	write(t, filepath.Join(outra, ".git/config"), "[remote \"origin\"]\n\turl = https://github.com/exemplo/outra.git\n")
	write(t, filepath.Join(outra, ".git/HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(outra, "garimpo.json"), `{"nome":"Outra","fonte":"javascript:alert(1)"}`)
	write(t, filepath.Join(outra, "apps/x/config.json"), `{"name":"X","version":"1","tipi_version":1,"available":true}`)
	// ramo com nome estranho não entra no endereço
	ruim := filepath.Join(dir, "repos/ruim")
	write(t, filepath.Join(ruim, ".git/config"), "[remote \"origin\"]\n\turl = https://github.com/exemplo/ruim\n")
	write(t, filepath.Join(ruim, ".git/HEAD"), "ref: refs/heads/a/../b\n")
	write(t, filepath.Join(ruim, "apps/y/config.json"), `{"name":"Y","version":"1","tipi_version":1,"available":true}`)

	c := New(dir).Catalog()
	by := map[string]CatalogStore{}
	for _, s := range c.Stores {
		by[s.Slug] = s
	}
	g := by["casaos"]
	if g.URL != "https://github.com/exemplo/Garimpo/tree/casaos" || g.Name != "CasaOS" || g.Badge != "via CasaOS" ||
		g.Origin != "https://github.com/IceWhaleTech/CasaOS-AppStore" || g.License != "Apache-2.0" || g.About != "Apps do CasaOS." {
		t.Fatalf("loja do Garimpo: %+v", g)
	}
	if o := by["outra"]; o.URL != "https://github.com/exemplo/outra" || o.Name != "Outra" || o.Origin != "" || o.Badge != "" {
		t.Fatalf("loja comum: %+v", o)
	}
	if r := by["ruim"]; r.URL != "https://github.com/exemplo/ruim" {
		t.Fatalf("ramo estranho: %+v", r)
	}
}

func TestCatalog(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repos/migrated")
	write(t, filepath.Join(repo, ".git/config"), "[remote \"origin\"]\n\turl = https://usuario:segredo@github.com/exemplo/loja.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n")
	write(t, filepath.Join(repo, "apps/plex/config.json"), `{"name":"Plex","short_desc":"Filmes","categories":["Media"],"version":"1.42.1","tipi_version":12,"port":32400,
		"author":"plex","source":"https://github.com/plexinc/pms-docker","website":"javascript:alert(1)","available":true,"created_at":1700000000000,"updated_at":1750000000000,
		"supported_architectures":["amd64","arm64"],"form_fields":[{"type":"text","label":"Token","hint":"do site","required":true,"env_variable":"X"},{"type":"random","label":"Segredo","env_variable":"Y"}]}`)
	write(t, filepath.Join(repo, "apps/plex/metadata/logo.jpg"), "jpeg")
	write(t, filepath.Join(repo, "apps/plex/metadata/description.md"), "# Plex\nFilmes e séries.")
	write(t, filepath.Join(repo, "apps/novo/config.json"), `{"name":"Novo","version":"2","tipi_version":1,"available":true}`)
	write(t, filepath.Join(repo, "apps/oculto/config.json"), `{"name":"Oculto","version":"1","tipi_version":1,"available":false}`)
	write(t, filepath.Join(repo, "apps/velho/config.json"), `{"name":"Velho","version":"1","tipi_version":1,"available":true,"deprecated":true}`)
	write(t, filepath.Join(repo, "apps/quebrado/config.json"), `{nao é json`)
	write(t, filepath.Join(repo, "apps/Maiusculo/config.json"), `{"name":"X","available":true}`)
	write(t, filepath.Join(dir, "apps/migrated/plex/config.json"), `{"name":"Plex","version":"1.41.0","tipi_version":10}`)
	write(t, filepath.Join(dir, "apps/_user/meuapp/config.json"), `{"name":"Meu app","version":"1","tipi_version":1}`)
	// loja maliciosa: logo, descrição e a própria pasta do app como atalhos para fora
	secret := filepath.Join(dir, "segredo.txt")
	write(t, secret, "senha do servidor")
	write(t, filepath.Join(repo, "apps/atalho/config.json"), `{"name":"Atalho","version":"1","tipi_version":1,"available":true}`)
	links := os.MkdirAll(filepath.Join(repo, "apps/atalho/metadata"), 0o755) == nil &&
		os.Symlink(secret, filepath.Join(repo, "apps/atalho/metadata/logo.jpg")) == nil &&
		os.Symlink(secret, filepath.Join(repo, "apps/atalho/metadata/description.md")) == nil &&
		os.Symlink(filepath.Join(repo, "apps/plex"), filepath.Join(repo, "apps/copia")) == nil

	d := New(dir)
	c := d.Catalog()
	if !c.Found || len(c.Stores) != 1 || c.Stores[0].Slug != "migrated" || c.Stores[0].URL != "https://github.com/exemplo/loja" {
		t.Fatalf("lojas: %+v", c.Stores)
	}
	by := map[string]CatalogApp{}
	for _, a := range c.Apps {
		by[a.URN] = a
	}
	for _, gone := range []string{"oculto:migrated", "velho:migrated", "quebrado:migrated", "Maiusculo:migrated", "copia:migrated"} {
		if _, ok := by[gone]; ok {
			t.Errorf("%s não deveria aparecer no catálogo", gone)
		}
	}
	p := by["plex:migrated"]
	if !p.Installed || !p.Update || p.Have != "1.41.0" || p.Version != "1.42.1" || !p.HasLogo || p.Created != 1700000000 || p.Website != "" ||
		p.Source == "" || len(p.Categories) != 1 || p.Categories[0] != "media" || len(p.Archs) != 2 {
		t.Fatalf("plex: %+v", p)
	}
	if n := by["novo:migrated"]; n.Installed || n.Update || n.HasLogo || n.Name != "Novo" {
		t.Fatalf("novo: %+v", n)
	}
	if u := by["meuapp:_user"]; !u.Installed || !u.Local {
		t.Fatalf("instalado fora das lojas: %+v", u)
	}
	det, ok := d.CatalogDetail("plex:migrated")
	if !ok || det.Description != "# Plex\nFilmes e séries." || len(det.Fields) != 1 || det.Fields[0].Label != "Token" || !det.Fields[0].Required {
		t.Fatalf("detalhe: %+v", det)
	}
	if lp, ok := d.CatalogLogo("plex:migrated"); !ok || lp != filepath.Join(repo, "apps/plex/metadata/logo.jpg") {
		t.Fatalf("logo: %q %v", lp, ok)
	}
	for _, bad := range []string{"", "nada:migrated", "../x:migrated", "plex:../migrated", "oculto:migrated"} {
		if _, ok := d.CatalogDetail(bad); ok {
			t.Errorf("CatalogDetail(%q) deveria falhar", bad)
		}
		if _, ok := d.CatalogLogo(bad); ok {
			t.Errorf("CatalogLogo(%q) deveria falhar", bad)
		}
	}
	if links { // onde dá para criar atalhos (Linux, CI)
		a, ok := by["atalho:migrated"]
		if !ok || a.HasLogo {
			t.Fatalf("logo que é atalho não conta: %+v", a)
		}
		if det, _ := d.CatalogDetail("atalho:migrated"); det.Description != "" {
			t.Fatalf("a descrição seguiu o atalho: %q", det.Description)
		}
		if _, ok := d.CatalogLogo("atalho:migrated"); ok {
			t.Fatal("o logo seguiu o atalho")
		}
	}
	// desinstalou e a loja ganhou um app: depois do Invalidate o catálogo muda na hora
	os.RemoveAll(filepath.Join(dir, "apps/migrated/plex"))
	write(t, filepath.Join(repo, "apps/chegou/config.json"), `{"name":"Chegou","version":"1","tipi_version":1,"available":true}`)
	if x, _ := d.catalogApp("plex:migrated"); !x.Installed {
		t.Fatal("antes do Invalidate vale o que estava guardado")
	}
	d.Invalidate()
	if x, _ := d.catalogApp("plex:migrated"); x.Installed || x.Update {
		t.Fatal("depois do Invalidate o app não está mais instalado")
	}
	if _, ok := d.catalogApp("chegou:migrated"); !ok || d.Catalog().Stores[0].Apps != 4 {
		t.Fatalf("depois do Invalidate o app novo aparece: %+v", d.Catalog().Stores)
	}
	if c := New("").Catalog(); c.Found || len(c.Apps) != 0 || c.Apps == nil || c.Stores == nil {
		t.Fatalf("sem pasta do Runtipi: %+v", c)
	}
}
