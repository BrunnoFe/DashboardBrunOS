package runtipi

// Catálogo da janela Loja (0.18.0): todos os apps das lojas que o Runtipi já baixou para o disco,
//
//	repos/<loja>/apps/<app>/config.json            nome, descrição curta, categorias, versão, datas
//	repos/<loja>/apps/<app>/metadata/logo.jpg      ícone
//	repos/<loja>/apps/<app>/metadata/description.md texto da página do app
//	repos/<loja>/.git/config                       endereço da loja
//
// Só leitura de arquivos, sem internet. As lojas são repositórios de terceiros: nada de seguir atalhos
// (symlink) lá dentro, senão uma loja maliciosa faria o BrunOS ler outro arquivo do servidor no lugar
// do logo ou da descrição.

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CatalogApp é um app de uma loja (instalado ou não).
type CatalogApp struct {
	URN        string   `json:"urn"`
	ID         string   `json:"id"`
	Store      string   `json:"store"`
	Name       string   `json:"name"`
	ShortDesc  string   `json:"shortDesc"`
	Categories []string `json:"categories"`
	Version    string   `json:"version"`
	Port       int      `json:"port,omitempty"`
	Author     string   `json:"author,omitempty"`
	Source     string   `json:"source,omitempty"`
	Website    string   `json:"website,omitempty"`
	Created    int64    `json:"created,omitempty"` // segundos
	Updated    int64    `json:"updated,omitempty"`
	Archs      []string `json:"archs,omitempty"`
	Deprecated bool     `json:"deprecated,omitempty"`
	NoGUI      bool     `json:"noGui,omitempty"`
	HasLogo    bool     `json:"hasLogo"`
	Installed  bool     `json:"installed,omitempty"`
	Have       string   `json:"have,omitempty"`   // versão instalada
	Update     bool     `json:"update,omitempty"` // a loja tem versão mais nova que a instalada
	Local      bool     `json:"local,omitempty"`  // instalado, mas fora das lojas (criado à mão ou loja removida)
	dir        string
	tipi       int
	hidden     bool // available: false ou descontinuado
}

// CatalogStore é uma loja do Runtipi. Name, Badge, Origin, License e About vêm do garimpo.json das lojas
// convertidas pelo Garimpo (0.19.0): o nome da loja de origem ("CasaOS") e o selo dos apps ("via CasaOS").
type CatalogStore struct {
	Slug    string `json:"slug"`
	URL     string `json:"url,omitempty"`
	Apps    int    `json:"apps"`
	Name    string `json:"name,omitempty"`
	Badge   string `json:"badge,omitempty"`
	Origin  string `json:"origin,omitempty"`
	License string `json:"license,omitempty"`
	About   string `json:"about,omitempty"`
}

// Catalog é o que a janela Loja recebe.
type Catalog struct {
	Found  bool           `json:"found"` // a pasta do Runtipi tem lojas baixadas
	Stores []CatalogStore `json:"stores"`
	Apps   []CatalogApp   `json:"apps"`
	At     int64          `json:"at"`
}

// FormField é uma pergunta que o Runtipi faz ao instalar o app.
type FormField struct {
	Label    string `json:"label"`
	Hint     string `json:"hint,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// CatalogDetail é a página de um app: o resumo, a descrição (Markdown) e o que a instalação pergunta.
type CatalogDetail struct {
	CatalogApp
	Description string      `json:"description"`
	Fields      []FormField `json:"fields"`
}

const (
	catalogTTL    = 2 * time.Minute
	maxConfig     = 256 << 10
	maxDescMD     = 64 << 10
	maxLogo       = 2 << 20
	maxStoreApps  = 5000
	maxFormFields = 40
)

type catalogConfig struct {
	Name       string   `json:"name"`
	ShortDesc  string   `json:"short_desc"`
	Categories []string `json:"categories"`
	Version    string   `json:"version"`
	Tipi       int      `json:"tipi_version"`
	Port       int      `json:"port"`
	Author     string   `json:"author"`
	Source     string   `json:"source"`
	Website    string   `json:"website"`
	Available  *bool    `json:"available"`
	Deprecated bool     `json:"deprecated"`
	NoGUI      bool     `json:"no_gui"`
	Archs      []string `json:"supported_architectures"`
	Created    float64  `json:"created_at"` // milissegundos
	Updated    float64  `json:"updated_at"`
	Fields     []struct {
		Label    string `json:"label"`
		Hint     string `json:"hint"`
		Required bool   `json:"required"`
		Type     string `json:"type"`
	} `json:"form_fields"`
}

// isDir e isFile não seguem atalhos: um symlink não é pasta nem arquivo para o catálogo.
func isDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir()
}

func isFile(p string, max int64) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode().IsRegular() && st.Size() <= max
}

func readLimited(p string, max int64) ([]byte, bool) {
	if !isFile(p, max) {
		return nil, false
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max))
	return b, err == nil
}

// nameOK: nome de loja ou de app como o Runtipi usa (vira parte de caminho e de pedido ao servidor).
func nameOK(s string) bool {
	if s == "" || len(s) > 64 || s[0] == '.' || s[0] == '-' || s[0] == '_' {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
			return false
		}
	}
	return !strings.Contains(s, "..")
}

func httpURL(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 || !(strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")) {
		return ""
	}
	return s
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = strings.ToValidUTF8(s[:n], "") + "…"
	}
	return s
}

// storeURL lê o endereço da loja no .git/config dela, sem usuário nem senha. Loja que é um ramo do
// repositório (o Runtipi aceita ".../tree/<ramo>") ganha o ramo no endereço, lido do .git/HEAD.
func storeURL(repo string) string {
	u := storeRepoURL(repo)
	if u == "" {
		return ""
	}
	if b, ok := readLimited(filepath.Join(repo, ".git", "HEAD"), 1<<10); ok {
		ramo, achou := strings.CutPrefix(strings.TrimSpace(string(b)), "ref: refs/heads/")
		if achou && ramo != "main" && ramo != "master" && branchOK(ramo) {
			u += "/tree/" + ramo
		}
	}
	return u
}

func branchOK(s string) bool {
	if s == "" || len(s) > 64 || strings.Contains(s, "..") {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// garimpoInfo é o garimpo.json na raiz de uma loja convertida pelo Garimpo.
type garimpoInfo struct {
	Nome    string `json:"nome"`
	Selo    string `json:"selo"`
	Fonte   string `json:"fonte"`
	Licenca string `json:"licenca"`
	Sobre   string `json:"sobre"`
}

// storeInfo completa a loja com o garimpo.json dela, se existir (só textos curtos e endereço http(s)).
func storeInfo(repo string, st *CatalogStore) {
	b, ok := readLimited(filepath.Join(repo, "garimpo.json"), 16<<10)
	var g garimpoInfo
	if !ok || json.Unmarshal(b, &g) != nil {
		return
	}
	st.Name, st.Badge, st.Origin = clip(g.Nome, 40), clip(g.Selo, 40), httpURL(g.Fonte)
	st.License, st.About = clip(g.Licenca, 40), clip(g.Sobre, 300)
}

func storeRepoURL(repo string) string {
	b, ok := readLimited(filepath.Join(repo, ".git", "config"), 64<<10)
	if !ok || !isDir(filepath.Join(repo, ".git")) {
		return ""
	}
	for _, ln := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(ln), "=")
		if !ok || strings.TrimSpace(k) != "url" {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(v))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return ""
		}
		u.User, u.RawQuery, u.Fragment = nil, "", ""
		return strings.TrimSuffix(u.String(), ".git")
	}
	return ""
}

// Invalidate esquece o que está guardado (depois de instalar, desinstalar ou mexer nas lojas).
func (d *Discoverer) Invalidate() {
	d.mu.Lock()
	d.at, d.catAt = time.Time{}, time.Time{}
	d.mu.Unlock()
}

// Catalog devolve todos os apps de todas as lojas baixadas, com o que está instalado marcado. A leitura
// das lojas (centenas de arquivos) fica guardada por 2 min; o "instalado" vem da lista de apps, que se
// renova a cada 20 s, para um app recém-instalado aparecer logo.
func (d *Discoverer) Catalog() Catalog {
	installed, _ := d.Apps()
	d.mu.Lock()
	if d.cat == nil || time.Since(d.catAt) >= catalogTTL {
		c := d.scanStores()
		d.cat, d.catAt = &c, time.Now()
	}
	base := *d.cat
	d.mu.Unlock()

	c := Catalog{Found: base.Found, At: base.At, Stores: make([]CatalogStore, len(base.Stores)), Apps: make([]CatalogApp, 0, len(base.Apps)+4)}
	copy(c.Stores, base.Stores)
	inst := map[string]App{}
	for _, a := range installed {
		inst[a.URN] = a
	}
	count := map[string]int{}
	seen := map[string]bool{}
	for _, a := range base.Apps {
		have, isInst := inst[a.URN]
		if !isInst && a.hidden { // fora da loja ou descontinuado: só aparece se já estiver instalado
			continue
		}
		if isInst {
			a.Installed, a.Have = true, have.Version
			t := d.installedTipi(a.Store, a.ID)
			a.Update = t > 0 && a.tipi > t
		}
		seen[a.URN] = true
		count[a.Store]++
		c.Apps = append(c.Apps, a)
	}
	for i := range c.Stores {
		c.Stores[i].Apps = count[c.Stores[i].Slug]
	}
	// instalados que não estão em nenhuma loja baixada (criados à mão, loja removida)
	for _, a := range installed {
		if seen[a.URN] {
			continue
		}
		c.Apps = append(c.Apps, CatalogApp{URN: a.URN, ID: a.ID, Store: a.Store, Name: a.Name, ShortDesc: a.ShortDesc, Categories: cleanList(a.Categories, 8),
			Version: a.Version, Port: a.Port, HasLogo: a.HasLogo, Installed: true, Have: a.Version, Local: true})
	}
	sort.SliceStable(c.Apps, func(i, j int) bool {
		x, y := strings.ToLower(c.Apps[i].Name), strings.ToLower(c.Apps[j].Name)
		if x != y {
			return x < y
		}
		return c.Apps[i].URN < c.Apps[j].URN
	})
	return c
}

// scanStores lê as lojas do disco (sem saber o que está instalado).
func (d *Discoverer) scanStores() Catalog {
	c := Catalog{Stores: []CatalogStore{}, Apps: []CatalogApp{}, At: time.Now().Unix()}
	if d.dir == "" {
		return c
	}
	repos := filepath.Join(d.dir, "repos")
	stores, _ := os.ReadDir(repos)
	for _, s := range stores {
		slug := s.Name()
		appsDir := filepath.Join(repos, slug, "apps")
		if !nameOK(slug) || !isDir(filepath.Join(repos, slug)) || !isDir(appsDir) {
			continue
		}
		c.Found = true
		st := CatalogStore{Slug: slug, URL: storeURL(filepath.Join(repos, slug))}
		storeInfo(filepath.Join(repos, slug), &st)
		entries, _ := os.ReadDir(appsDir)
		for _, e := range entries {
			id := e.Name()
			dir := filepath.Join(appsDir, id)
			if st.Apps >= maxStoreApps || !nameOK(id) || !isDir(dir) {
				continue
			}
			var cfg catalogConfig
			b, ok := readLimited(filepath.Join(dir, "config.json"), maxConfig)
			if !ok || json.Unmarshal(b, &cfg) != nil {
				continue
			}
			a := CatalogApp{URN: id + ":" + slug, ID: id, Store: slug, hidden: cfg.Deprecated || (cfg.Available != nil && !*cfg.Available), Name: clip(cfg.Name, 80), ShortDesc: clip(cfg.ShortDesc, 240),
				Categories: cleanList(cfg.Categories, 8), Version: clip(cfg.Version, 40), Port: cfg.Port, Author: clip(cfg.Author, 80),
				Source: httpURL(cfg.Source), Website: httpURL(cfg.Website), Created: int64(cfg.Created / 1000), Updated: int64(cfg.Updated / 1000),
				Archs: cleanList(cfg.Archs, 6), Deprecated: cfg.Deprecated, NoGUI: cfg.NoGUI, dir: dir, tipi: cfg.Tipi}
			if a.Name == "" {
				a.Name = id
			}
			a.HasLogo = isDir(filepath.Join(dir, "metadata")) && isFile(filepath.Join(dir, "metadata", "logo.jpg"), maxLogo)
			st.Apps++
			c.Apps = append(c.Apps, a)
		}
		c.Stores = append(c.Stores, st)
	}
	sort.Slice(c.Stores, func(i, j int) bool { return c.Stores[i].Slug < c.Stores[j].Slug })
	return c
}

// installedTipi lê o tipi_version da cópia instalada (0 = não deu para ler).
func (d *Discoverer) installedTipi(store, id string) int {
	var cfg configJSON
	b, err := os.ReadFile(filepath.Join(d.dir, "apps", store, id, "config.json"))
	if err != nil || json.Unmarshal(b, &cfg) != nil {
		return 0
	}
	return cfg.Tipi
}

func cleanList(in []string, max int) []string {
	out := []string{}
	for _, s := range in {
		if s = clip(strings.ToLower(s), 32); s != "" && len(out) < max {
			out = append(out, s)
		}
	}
	return out
}

func (d *Discoverer) catalogApp(urn string) (CatalogApp, bool) {
	for _, a := range d.Catalog().Apps {
		if a.URN == urn {
			return a, true
		}
	}
	return CatalogApp{}, false
}

// CatalogDetail devolve a página de um app do catálogo.
func (d *Discoverer) CatalogDetail(urn string) (CatalogDetail, bool) {
	a, ok := d.catalogApp(urn)
	if !ok {
		return CatalogDetail{}, false
	}
	out := CatalogDetail{CatalogApp: a, Fields: []FormField{}}
	if a.dir == "" { // instalado fora das lojas: só o resumo
		return out, true
	}
	if b, ok := readLimited(filepath.Join(a.dir, "metadata", "description.md"), maxDescMD); ok && isDir(filepath.Join(a.dir, "metadata")) {
		out.Description = strings.ToValidUTF8(string(b), "")
	}
	var cfg catalogConfig
	if b, ok := readLimited(filepath.Join(a.dir, "config.json"), maxConfig); ok && json.Unmarshal(b, &cfg) == nil {
		for _, f := range cfg.Fields {
			if f.Type == "random" || strings.TrimSpace(f.Label) == "" || len(out.Fields) >= maxFormFields {
				continue // "random" o Runtipi gera sozinho, não pergunta
			}
			out.Fields = append(out.Fields, FormField{Label: clip(f.Label, 120), Hint: clip(f.Hint, 240), Required: f.Required})
		}
	}
	return out, true
}

// CatalogLogo devolve o caminho do logo de um app do catálogo (instalado fora das lojas: o da cópia instalada).
func (d *Discoverer) CatalogLogo(urn string) (string, bool) {
	a, ok := d.catalogApp(urn)
	if !ok || !a.HasLogo {
		return "", false
	}
	if a.dir == "" {
		return d.LogoPath(urn)
	}
	p := filepath.Join(a.dir, "metadata", "logo.jpg")
	return p, isFile(p, maxLogo) // confere de novo: a loja pode ter mudado desde a leitura
}
