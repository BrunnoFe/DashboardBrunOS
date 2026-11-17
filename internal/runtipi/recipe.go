package runtipi

// Receita dos apps (0.21.0, decisão 62): o docker-compose de cada app, lido para o selo "Requer
// privilégios" (catálogo e apps instalados) e para a janela de instalação já vir com o que a receita traz.
// Ler centenas de receitas a cada 2 min seria desperdício: o resultado fica guardado enquanto o arquivo
// não muda (mesmo tamanho e mesma data).

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"brunos/internal/appcfg"
)

type recipeEntry struct {
	mod  time.Time
	size int64
	r    appcfg.Recipe
	ok   bool
}

var (
	recipeMu    sync.Mutex
	recipeCache = map[string]recipeEntry{}
)

// recipeFile acha a receita na pasta de um app (o .yml tem preferência, como no Runtipi).
func recipeFile(dir string) (string, bool, bool) {
	for _, n := range []string{"docker-compose.yml", "docker-compose.json"} {
		p := filepath.Join(dir, n)
		if isFile(p, appcfg.MaxRecipe) {
			return p, n == "docker-compose.json", true
		}
	}
	return "", false, false
}

// readRecipe lê (ou pega do cache) a receita da pasta de um app.
func readRecipe(dir, id string) (appcfg.Recipe, bool) {
	p, isJSON, ok := recipeFile(dir)
	if !ok {
		return appcfg.Recipe{}, false
	}
	st, err := os.Lstat(p)
	if err != nil {
		return appcfg.Recipe{}, false
	}
	recipeMu.Lock()
	e, hit := recipeCache[p]
	recipeMu.Unlock()
	if hit && e.mod.Equal(st.ModTime()) && e.size == st.Size() {
		return e.r, e.ok
	}
	b, ok := readLimited(p, appcfg.MaxRecipe)
	var r appcfg.Recipe
	if ok {
		var err error
		r, err = appcfg.ParseRecipe(b, isJSON, id)
		ok = err == nil
	}
	recipeMu.Lock()
	if len(recipeCache) > 20000 { // muitas lojas removidas: recomeça
		recipeCache = map[string]recipeEntry{}
	}
	recipeCache[p] = recipeEntry{mod: st.ModTime(), size: st.Size(), r: r, ok: ok}
	recipeMu.Unlock()
	return r, ok
}

// Recipe devolve a receita de um app do catálogo: a da cópia instalada, se o app está instalado (é a que
// roda), senão a da loja.
func (d *Discoverer) Recipe(urn string) (appcfg.Recipe, bool) {
	a, ok := d.catalogApp(urn)
	if !ok {
		return appcfg.Recipe{}, false
	}
	if a.Installed {
		if r, ok := readRecipe(filepath.Join(d.dir, "apps", a.Store, a.ID), a.ID); ok {
			return r, true
		}
	}
	if a.dir == "" {
		return appcfg.Recipe{}, false
	}
	return readRecipe(a.dir, a.ID)
}
