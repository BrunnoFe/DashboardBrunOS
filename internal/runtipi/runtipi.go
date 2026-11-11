// Package runtipi descobre os apps instalados lendo a pasta do Runtipi (montada só leitura):
//
//	apps/<loja>/<app>/config.json         nome, porta, descrição
//	apps/<loja>/<app>/metadata/logo.jpg   ícone
//	app-data/<loja>/<app>/app.env         porta escolhida, domínio local, protocolo
//
// app.env também guarda senhas geradas pelos apps; este pacote lê SÓ as chaves
// da lista abaixo e nunca devolve o arquivo nem outras variáveis.
package runtipi

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var allowedEnv = map[string]bool{
	"APP_PORT": true, "APP_DOMAIN": true, "APP_LOCAL_DOMAIN": true, "APP_PROTOCOL": true,
	"APP_EXPOSED": true, "APP_EXPOSED_DOMAIN": true, "LOCAL_DOMAIN": true, "INTERNAL_IP": true, "NGINX_PORT": true,
}

type App struct {
	URN           string   `json:"urn"`
	ID            string   `json:"id"`
	Store         string   `json:"store"`
	Name          string   `json:"name"`
	ShortDesc     string   `json:"shortDesc"`
	Categories    []string `json:"categories"`
	Version       string   `json:"version"`
	Port          int      `json:"port"`
	LocalDomain   string   `json:"localDomain"`   // ex.: syncthing-migrated.server.lan
	ExposedDomain string   `json:"exposedDomain"` // domínio público, se exposto
	Protocol      string   `json:"protocol"`
	Project       string   `json:"project"` // projeto do docker compose
	HasLogo       bool     `json:"hasLogo"`
	logoPath      string
}

type Info struct {
	Found       bool   `json:"found"`
	LocalDomain string `json:"localDomain"`
	InternalIP  string `json:"internalIp"`
	Port        int    `json:"port"` // porta do painel do Runtipi (padrão 80)
}

type Discoverer struct {
	dir  string
	mu   sync.Mutex
	apps []App
	info Info
	at   time.Time
}

func New(dir string) *Discoverer { return &Discoverer{dir: dir} }

const cacheTTL = 20 * time.Second

func (d *Discoverer) Apps() ([]App, Info) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Since(d.at) < cacheTTL && d.apps != nil {
		return d.apps, d.info
	}
	d.apps, d.info = d.scan()
	d.at = time.Now()
	return d.apps, d.info
}

func (d *Discoverer) LogoPath(urn string) (string, bool) {
	apps, _ := d.Apps()
	for _, a := range apps {
		if a.URN == urn && a.HasLogo {
			return a.logoPath, true
		}
	}
	return "", false
}

type configJSON struct {
	Name       string   `json:"name"`
	ID         string   `json:"id"`
	Port       int      `json:"port"`
	ShortDesc  string   `json:"short_desc"`
	Categories []string `json:"categories"`
	Version    string   `json:"version"`
	Tipi       int      `json:"tipi_version"`
}

// Update é uma atualização disponível de um app (A1, decisão 44).
type Update struct {
	URN       string `json:"urn"`
	ID        string `json:"id"`
	Store     string `json:"store"`
	Name      string `json:"name"`
	Installed string `json:"installed"` // versão instalada
	Available string `json:"available"` // versão na loja
}

// CheckUpdates compara a cópia instalada de cada app (apps/<loja>/<app>/config.json) com a loja que o
// Runtipi baixou (repos/<loja>/apps/<app>/config.json), pelo tipi_version, que só sobe. Apps criados
// à mão (_user) e o layout antigo, sem lojas, ficam de fora. Só lê arquivos (nada de Postgres).
func (d *Discoverer) CheckUpdates() []Update {
	out := []Update{}
	if d.dir == "" {
		return out
	}
	read := func(p string) (configJSON, bool) {
		var c configJSON
		b, err := os.ReadFile(p)
		return c, err == nil && json.Unmarshal(b, &c) == nil
	}
	stores, _ := os.ReadDir(filepath.Join(d.dir, "apps"))
	for _, s := range stores {
		if !s.IsDir() || strings.HasPrefix(s.Name(), ".") || strings.HasPrefix(s.Name(), "_") {
			continue
		}
		apps, _ := os.ReadDir(filepath.Join(d.dir, "apps", s.Name()))
		for _, a := range apps {
			if !a.IsDir() {
				continue
			}
			inst, ok := read(filepath.Join(d.dir, "apps", s.Name(), a.Name(), "config.json"))
			if !ok {
				continue
			}
			repo, ok := read(filepath.Join(d.dir, "repos", s.Name(), "apps", a.Name(), "config.json"))
			if !ok || repo.Tipi <= inst.Tipi || inst.Tipi == 0 {
				continue
			}
			name := inst.Name
			if name == "" {
				name = a.Name()
			}
			out = append(out, Update{URN: a.Name() + ":" + s.Name(), ID: a.Name(), Store: s.Name(), Name: name, Installed: inst.Version, Available: repo.Version})
		}
	}
	return out
}

func (d *Discoverer) scan() ([]App, Info) {
	info := Info{LocalDomain: "tipi.lan"}
	if d.dir == "" {
		return []App{}, info
	}
	appsDir := filepath.Join(d.dir, "apps")
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return []App{}, info
	}
	info.Found = true
	d.readGlobal(&info)

	var apps []App
	add := func(store, id, dir string) {
		var cfg configJSON
		data, err := os.ReadFile(filepath.Join(dir, "config.json"))
		if err != nil || json.Unmarshal(data, &cfg) != nil {
			return
		}
		a := App{ID: id, Store: store, Name: cfg.Name, ShortDesc: cfg.ShortDesc, Categories: cfg.Categories, Version: cfg.Version, Port: cfg.Port, Protocol: "http"}
		if a.Name == "" {
			a.Name = id
		}
		if store != "" {
			a.URN, a.Project = id+":"+store, id+"_"+store
		} else {
			a.URN, a.Project = id, id
		}
		envPath := filepath.Join(d.dir, "app-data", store, id, "app.env")
		env := readEnv(envPath)
		if p, err := strconv.Atoi(env["APP_PORT"]); err == nil && p > 0 {
			a.Port = p
		}
		a.LocalDomain = env["APP_LOCAL_DOMAIN"]
		if env["APP_EXPOSED"] == "true" {
			a.ExposedDomain = env["APP_EXPOSED_DOMAIN"]
		}
		if v := env["APP_PROTOCOL"]; v != "" {
			a.Protocol = v
		}
		if info.LocalDomain == "tipi.lan" && env["LOCAL_DOMAIN"] != "" {
			info.LocalDomain = env["LOCAL_DOMAIN"]
		}
		if info.InternalIP == "" && env["INTERNAL_IP"] != "" {
			info.InternalIP = env["INTERNAL_IP"]
		}
		logo := filepath.Join(dir, "metadata", "logo.jpg")
		if st, err := os.Stat(logo); err == nil && st.Mode().IsRegular() {
			a.HasLogo, a.logoPath = true, logo
		}
		apps = append(apps, a)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(appsDir, e.Name())
		if _, err := os.Stat(filepath.Join(p, "config.json")); err == nil {
			add("", e.Name(), p) // layout antigo, sem lojas
			continue
		}
		subs, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		for _, s := range subs {
			if s.IsDir() && !strings.HasPrefix(s.Name(), ".") {
				add(e.Name(), s.Name(), filepath.Join(p, s.Name()))
			}
		}
	}
	sort.Slice(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })
	if apps == nil {
		apps = []App{}
	}
	return apps, info
}

func (d *Discoverer) readGlobal(info *Info) {
	var settings struct {
		LocalDomain string `json:"localDomain"`
		InternalIP  string `json:"internalIp"`
		Port        int    `json:"port"`
	}
	if data, err := os.ReadFile(filepath.Join(d.dir, "state", "settings.json")); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	env := readEnv(filepath.Join(d.dir, ".env"))
	if settings.LocalDomain != "" {
		info.LocalDomain = settings.LocalDomain
	} else if env["LOCAL_DOMAIN"] != "" {
		info.LocalDomain = env["LOCAL_DOMAIN"]
	}
	info.InternalIP = settings.InternalIP
	if info.InternalIP == "" {
		info.InternalIP = env["INTERNAL_IP"]
	}
	info.Port = settings.Port
	if info.Port == 0 {
		info.Port, _ = strconv.Atoi(env["NGINX_PORT"])
	}
	if info.Port <= 0 || info.Port > 65535 {
		info.Port = 80
	}
}

// readEnv lê um arquivo .env e devolve SOMENTE as chaves permitidas.
func readEnv(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(strings.TrimPrefix(k, "export "))
		if !ok || !allowedEnv[k] {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		out[k] = v
	}
	return out
}
