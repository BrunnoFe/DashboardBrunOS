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

// UnreadableRecipe: a receita existe, mas o BrunOS não consegue conferir (grande demais, atalho ou com
// erro). Conta como "Requer privilégios": o Runtipi usaria o arquivo mesmo assim (falha fechada,
// auditoria de 19/11/2026; antes o app passava como comum).
const UnreadableRecipe = "Receita que o BrunOS não consegue conferir (grande demais, atalho ou com erro): o app pode pedir qualquer poder sobre o servidor"

var recipeNames = []string{"docker-compose.yml", "docker-compose.json"}

// parseRecipeFile lê (ou pega do cache) uma receita. present = o arquivo existe (mesmo que seja atalho).
func parseRecipeFile(p string, isJSON bool, id string) (r appcfg.Recipe, ok, present bool) {
	st, err := os.Lstat(p)
	if err != nil {
		return r, false, false
	}
	if !st.Mode().IsRegular() || st.Size() > appcfg.MaxRecipe {
		return r, false, true
	}
	recipeMu.Lock()
	e, hit := recipeCache[p]
	recipeMu.Unlock()
	if hit && e.mod.Equal(st.ModTime()) && e.size == st.Size() {
		return e.r, e.ok, true
	}
	b, ok := readLimited(p, appcfg.MaxRecipe)
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
	return r, ok, true
}

// readRecipe lê a receita da pasta de um app. O .yml tem preferência para preencher a janela de
// instalação, mas os privilégios são a SOMA do .yml e do .json: uma loja não consegue mostrar um .yml
// inofensivo enquanto o Runtipi usa um .json com privilégios. Receita que existe e não dá para conferir
// conta como privilégio (UnreadableRecipe).
func readRecipe(dir, id string) (appcfg.Recipe, bool) {
	var base appcfg.Recipe
	have, found, unreadable := false, false, false
	var privs []string
	seen := map[string]bool{}
	for _, n := range recipeNames {
		r, ok, present := parseRecipeFile(filepath.Join(dir, n), n == "docker-compose.json", id)
		if !present {
			continue
		}
		found = true
		if !ok {
			unreadable = true
			continue
		}
		if !have {
			base, have = r, true
		}
		for _, p := range r.Privileges {
			if !seen[p] {
				seen[p] = true
				privs = append(privs, p)
			}
		}
	}
	if !found {
		return appcfg.Recipe{}, false
	}
	if unreadable {
		privs = append(privs, UnreadableRecipe)
	}
	if !have {
		base = appcfg.Recipe{Env: []appcfg.Var{}, Mounts: []appcfg.Mount{}, Ports: []string{}}
	}
	if privs == nil {
		privs = []string{}
	}
	base.Privileges = privs
	return base, true
}

// NewPrivileges devolve os privilégios que a versão da loja de um app instalado pede a mais que a versão
// instalada: atualizar traria esses poderes sem a pessoa ver (auditoria de 19/11/2026).
func (d *Discoverer) NewPrivileges(urn string) []string {
	a, ok := d.catalogApp(urn)
	if !ok || !a.Installed || a.dir == "" {
		return nil
	}
	inst, _ := readRecipe(filepath.Join(d.dir, "apps", a.Store, a.ID), a.ID)
	repo, ok := readRecipe(a.dir, a.ID)
	if !ok {
		return nil
	}
	had := map[string]bool{}
	for _, p := range inst.Privileges {
		had[p] = true
	}
	var out []string
	for _, p := range repo.Privileges {
		if !had[p] {
			out = append(out, p)
		}
	}
	return out
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
