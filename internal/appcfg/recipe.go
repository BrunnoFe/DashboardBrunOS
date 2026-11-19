package appcfg

// Receita de um app (0.21.0, decisão 62): o docker-compose da loja (docker-compose.yml ou o formato
// docker-compose.json do Runtipi). Serve para duas coisas:
//
//   - Privilégios: a mesma régua do Garimpo, para o selo "Requer privilégios" em apps de qualquer loja.
//   - Os Ajustes da janela de instalação já vêm com o que a receita traz (variáveis, pastas, portas).

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxRecipe é o tamanho máximo de uma receita lida.
const MaxRecipe = 256 << 10

// Recipe é o que a tela precisa saber da receita de um app.
type Recipe struct {
	Main       string   `json:"main"`       // nome do serviço principal
	Image      string   `json:"image"`      // imagem do serviço principal (repositório:versão)
	Env        []Var    `json:"env"`        // variáveis do serviço principal, como a receita escreve
	Mounts     []Mount  `json:"mounts"`     // pastas do serviço principal
	Ports      []string `json:"ports"`      // portas extras publicadas no servidor (texto para mostrar)
	Restart    string   `json:"restart"`    // política de reinício da receita ("" = a do Runtipi)
	ShmMB      int      `json:"shmMB"`      // memória compartilhada da receita (0 = a do Docker, 64 MB)
	Desktop    bool     `json:"desktop"`    // app de área de trabalho no navegador (Brave, Firefox...): precisa de shm maior
	Services   int      `json:"services"`   // quantos serviços a receita tem
	Privileges []string `json:"privileges"` // o que a receita pede de poder sobre o servidor, em português
}

// livres são pastas do sistema que qualquer app pode montar (só para ler a hora certa).
var livres = map[string]bool{"/etc/localtime": true, "/etc/timezone": true}

// capsRecipe são os poderes do Linux que fazem um app "requerer privilégios" (régua do Garimpo).
var capsRecipe = map[string]bool{"ALL": true, "SYS_ADMIN": true, "NET_ADMIN": true, "SYS_MODULE": true, "SYS_RAWIO": true,
	"SYS_PTRACE": true, "SYS_BOOT": true, "SYS_TIME": true, "DAC_READ_SEARCH": true, "LINUX_IMMUTABLE": true,
	"MAC_ADMIN": true, "MAC_OVERRIDE": true, "SYSLOG": true, "BPF": true, "PERFMON": true}

// recipeHostOK diz se uma pasta da receita é das que todo app usa (do próprio app, mídia do Runtipi,
// discos e pastas pessoais, hora certa, placa de vídeo) ou um volume com nome.
//
// Auditoria de 19/11/2026: o ".." é conferido ANTES de aceitar caminho relativo (com ".." suficientes
// "../../../.." vira a raiz do servidor), "~" (pasta pessoal) não é aceito, a pasta app-data inteira do
// Runtipi não é mais "comum" (tem os dados e as senhas de todos os apps, inclusive as chaves do BrunOS)
// e, dentro de /home, a pasta pessoal inteira e as pastas ocultas (.ssh, .config...) contam como privilégio.
func recipeHostOK(h string) bool {
	if strings.Contains(h, "docker.sock") || strings.Contains(h, "..") || strings.HasPrefix(h, "~") {
		return false
	}
	if !strings.HasPrefix(h, "/") && !strings.HasPrefix(h, "$") {
		return true // volume com nome (conferido à parte) ou caminho relativo à pasta do app
	}
	for _, p := range []string{"${APP_DATA_DIR}", "$APP_DATA_DIR", "${ROOT_FOLDER_HOST}/media"} {
		if h == p || strings.HasPrefix(h, p+"/") {
			return true
		}
	}
	if strings.HasPrefix(h, "$") {
		return false // outra variável: não dá para saber onde cai
	}
	c := path.Clean(h)
	if rest, ok := strings.CutPrefix(c, "/home/"); ok {
		parts := strings.Split(rest, "/")
		if len(parts) < 2 { // /home/<usuário> inteira: tem .ssh, .bashrc...
			return false
		}
		for _, p := range parts {
			if strings.HasPrefix(p, ".") {
				return false
			}
		}
		return true
	}
	return livres[c] || strings.HasPrefix(c, "/mnt/") || strings.HasPrefix(c, "/dev/dri")
}

// fileHostOK confere um arquivo do servidor citado pela receita (env_file, secrets/configs file:):
// só arquivos da própria pasta do app (caminho relativo sem "..").
func fileHostOK(h string) bool {
	return h != "" && !strings.HasPrefix(h, "/") && !strings.HasPrefix(h, "$") && !strings.HasPrefix(h, "~") && !strings.Contains(h, "..")
}

// maxResolved: quantos nós a árvore pode ter depois de resolver as âncoras do YAML (contra "bomba" de aliases).
const maxResolved = 200000

// resolveYAML devolve uma cópia da árvore com as âncoras (&x / *x) trocadas pelo conteúdo e as chaves de
// mescla (<<) aplicadas, como o docker compose faz. Sem isso, "privileged: *t" ou "<<: *perigoso"
// passavam despercebidos. ok = false quando a árvore resolvida passa de maxResolved nós.
func resolveYAML(n *yaml.Node) (*yaml.Node, bool) {
	budget := maxResolved
	var walk func(n *yaml.Node, depth int) *yaml.Node
	walk = func(n *yaml.Node, depth int) *yaml.Node {
		budget--
		if n == nil || budget < 0 || depth > 100 {
			return nil
		}
		if n.Kind == yaml.AliasNode {
			return walk(n.Alias, depth+1)
		}
		cp := *n
		cp.Content = nil
		cp.Anchor = ""
		switch n.Kind {
		case yaml.DocumentNode, yaml.SequenceNode:
			for _, c := range n.Content {
				if r := walk(c, depth+1); r != nil {
					cp.Content = append(cp.Content, r)
				}
			}
		case yaml.MappingNode:
			explicit := map[string]bool{}
			var merged []*yaml.Node
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], walk(n.Content[i+1], depth+1)
				if v == nil {
					continue
				}
				if k.Value == "<<" {
					srcs := []*yaml.Node{v}
					if v.Kind == yaml.SequenceNode {
						srcs = v.Content
					}
					for _, m := range srcs {
						if m.Kind == yaml.MappingNode {
							merged = append(merged, m.Content...)
						}
					}
					continue
				}
				explicit[k.Value] = true
				cp.Content = append(cp.Content, k, v)
			}
			for i := 0; i+1 < len(merged); i += 2 {
				if !explicit[merged[i].Value] {
					explicit[merged[i].Value] = true
					cp.Content = append(cp.Content, merged[i], merged[i+1])
				}
			}
		}
		return &cp
	}
	r := walk(n, 0)
	return r, budget >= 0 && r != nil
}

// stringsOf: um valor que pode ser texto ou lista de textos (env_file, aliases...).
func stringsOf(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode {
		return []string{n.Value}
	}
	var out []string
	for _, c := range seqOf(n) {
		switch c.Kind {
		case yaml.ScalarNode:
			out = append(out, c.Value)
		case yaml.MappingNode: // env_file: - path: x
			if p := mapGet(c, "path"); p != nil {
				out = append(out, p.Value)
			}
		}
	}
	return out
}

// niceHost escreve a pasta do Runtipi de um jeito que se entende ("${ROOT_FOLDER_HOST}/x" → "do Runtipi /x").
// O Garimpo usa a mesma frase (converter.py, pasta_legivel).
func niceHost(h string) string {
	if r, ok := strings.CutPrefix(h, "${ROOT_FOLDER_HOST}"); ok {
		if r == "" {
			return "do Runtipi (inteira)"
		}
		return "do Runtipi " + r
	}
	return h
}

var shareNames = map[string]string{"pid": "os processos", "ipc": "a memória compartilhada", "userns_mode": "os usuários",
	"uts": "o nome do servidor", "cgroup": "os grupos de controle"}

// Privileges lista, em português, o que um compose pede de poder sobre o servidor (todos os serviços).
// Lista vazia: o app roda como os apps comuns.
func Privileges(root *yaml.Node) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	root, ok := resolveYAML(root)
	if !ok {
		add("Receita com atalhos do YAML demais para o BrunOS conferir")
		return out
	}
	if root != nil && root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	if mapGet(root, "include") != nil {
		add("Inclui outros arquivos (include), que o BrunOS não confere")
	}
	// volumes com nome que na verdade apontam para uma pasta do servidor (driver_opts com bind/device)
	namedHost := map[string]bool{}
	for _, vname := range mapKeys(mapGet(root, "volumes")) {
		v := mapGet(mapGet(root, "volumes"), vname)
		if d := mapGet(v, "driver"); (d != nil && d.Value != "" && d.Value != "local") || mapGet(v, "driver_opts") != nil {
			namedHost[vname] = true
		}
	}
	for _, top := range []string{"secrets", "configs"} {
		sec := mapGet(root, top)
		for _, k := range mapKeys(sec) {
			if f := mapGet(mapGet(sec, k), "file"); f != nil && !fileHostOK(f.Value) {
				add("Lê o arquivo " + niceHost(f.Value) + " do servidor (" + top + ")")
			}
		}
	}
	svcs := mapGet(root, "services")
	for _, name := range mapKeys(svcs) {
		s := mapGet(svcs, name)
		if mapGet(s, "extends") != nil {
			add("Herda de outro arquivo ou serviço (extends), que o BrunOS não confere")
		}
		for _, f := range stringsOf(mapGet(s, "env_file")) {
			if !fileHostOK(f) {
				add("Lê variáveis de um arquivo de fora do app (env_file: " + niceHost(f) + ")")
			}
		}
		// um app que se chama "brunos" na rede se passaria pelo BrunOS (ou pelo proxy do Docker dele)
		names := []string{}
		for _, k := range []string{"hostname", "container_name"} {
			if v := mapGet(s, k); v != nil {
				names = append(names, v.Value)
			}
		}
		if nets := mapGet(s, "networks"); nets != nil && nets.Kind == yaml.MappingNode {
			for _, nn := range mapKeys(nets) {
				names = append(names, stringsOf(mapGet(mapGet(nets, nn), "aliases"))...)
			}
		}
		for _, n := range names {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(n)), "brunos") {
				add("Usa o nome do BrunOS na rede (" + n + ")")
			}
		}
		if v := mapGet(s, "privileged"); v != nil && strings.EqualFold(v.Value, "true") {
			add("Controle total do servidor (privileged)")
		}
		if v := mapGet(s, "network_mode"); v != nil {
			switch {
			case v.Value == "host":
				add("Usa a rede do servidor direto, sem isolamento (network_mode: host)")
			case strings.HasPrefix(v.Value, "container:"):
				add("Usa a rede de outro container, de fora do app (" + v.Value + ")")
			}
		}
		for _, k := range []string{"pid", "ipc", "userns_mode", "uts", "cgroup"} {
			if v := mapGet(s, k); v != nil && v.Value == "host" {
				add(fmt.Sprintf("Compartilha %s com o servidor (%s: host)", shareNames[k], k))
			}
		}
		if mapGet(s, "cgroup_parent") != nil || mapGet(s, "volumes_from") != nil {
			add("Usa cgroup_parent ou volumes_from (recursos de fora do app)")
		}
		caps := []string{}
		for _, c := range seqOf(mapGet(s, "cap_add")) {
			if k := strings.TrimPrefix(strings.ToUpper(c.Value), "CAP_"); capsRecipe[k] {
				caps = append(caps, k)
			}
		}
		if len(caps) > 0 {
			sort.Strings(caps)
			add("Poderes de administrador do Linux (" + strings.Join(caps, ", ") + ")")
		}
		for _, d := range seqOf(mapGet(s, "devices")) {
			src := d.Value
			if d.Kind == yaml.MappingNode {
				if x := mapGet(d, "source"); x != nil {
					src = x.Value
				}
			}
			if src = strings.SplitN(src, ":", 2)[0]; !strings.HasPrefix(src, "/dev/dri") {
				add("Acesso ao aparelho " + src + " do servidor")
			}
		}
		for _, o := range seqOf(mapGet(s, "security_opt")) {
			if strings.Contains(o.Value, "unconfined") || strings.Contains(o.Value, "disable") {
				add("Desliga uma proteção do Docker (" + o.Value + ")")
			}
		}
		if r := mapGet(s, "runtime"); r != nil && r.Value != "" && r.Value != "nvidia" {
			add("Usa o runtime " + r.Value)
		}
		for _, it := range seqOf(mapGet(s, "volumes")) {
			h := volumeHost(it)
			switch {
			case h == "":
			case strings.Contains(h, "docker.sock"):
				add("Controla o Docker do servidor (socket do Docker): equivale a controle total")
			case !recipeHostOK(h):
				add("Mexe na pasta " + niceHost(h) + " do servidor")
			case namedHost[h]:
				add("Volume que aponta para uma pasta do servidor (" + h + ")")
			}
		}
	}
	return out
}

// FilePrivileges lista os privilégios de um arquivo de Ajustes (para o selo do app instalado: um privilégio
// acrescentado nos Ajustes também conta). Arquivo com erro: nenhum.
func FilePrivileges(raw []byte) []string {
	_, root, err := parse(raw)
	if err != nil {
		return nil
	}
	return Privileges(root)
}

// CheckRaw confere um arquivo de Ajustes escrito à mão no editor do Avançado (0.22.0, decisão 62 d): tem
// que ser um docker-compose válido, sem `build` (montar imagem com arquivos do servidor) e com cada serviço
// um mapa. Devolve os privilégios NOVOS em relação ao arquivo de hoje: a tela pede "Entendo os riscos" e a
// Senha de configuração antes de gravar.
func CheckRaw(oldRaw, newRaw []byte) ([]string, error) {
	_, root, err := parse(newRaw)
	if err != nil {
		return nil, err
	}
	svcs := mapGet(root, "services")
	if svcs != nil && svcs.Kind != yaml.MappingNode {
		return nil, errors.New("\"services\" precisa ser uma lista de serviços com nome (services: nome: ...)")
	}
	for _, name := range mapKeys(svcs) {
		s := mapGet(svcs, name)
		if s.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("o serviço %s não tem o formato de um serviço do docker-compose", name)
		}
		if mapGet(s, "build") != nil {
			return nil, fmt.Errorf("o serviço %s usa build (montar uma imagem com arquivos do servidor): isso não é aceito nos Ajustes", name)
		}
	}
	had := map[string]bool{}
	for _, p := range FilePrivileges(oldRaw) {
		had[p] = true
	}
	out := []string{}
	for _, p := range Privileges(root) {
		if !had[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

// volumeHost devolve a origem de um item de volumes ("" = volume anônimo ou tmpfs).
func volumeHost(it *yaml.Node) string {
	switch it.Kind {
	case yaml.ScalarNode:
		if p := splitColon(it.Value); len(p) >= 2 {
			return p[0]
		}
	case yaml.MappingNode:
		if t := mapGet(it, "type"); t != nil && t.Value != "bind" && t.Value != "volume" {
			return ""
		}
		if s := mapGet(it, "source"); s != nil {
			return s.Value
		}
	}
	return ""
}

// ParseRecipe lê a receita de um app. isJSON = docker-compose.json (formato do Runtipi); id = o id do app,
// que é o nome do serviço principal quando a receita não diz qual é.
func ParseRecipe(raw []byte, isJSON bool, id string) (Recipe, error) {
	r := Recipe{Env: []Var{}, Mounts: []Mount{}, Ports: []string{}, Privileges: []string{}}
	if len(raw) > MaxRecipe {
		return r, errors.New("a receita do app é grande demais")
	}
	var root *yaml.Node
	if isJSON {
		n, main, err := jsonRecipe(raw)
		if err != nil {
			return r, err
		}
		root, r.Main = n, main
	} else {
		_, n, err := parse(raw)
		if err != nil {
			return r, errors.New("a receita do app tem um erro")
		}
		root = n
	}
	svcs := mapGet(root, "services")
	names := mapKeys(svcs)
	r.Services = len(names)
	if r.Main == "" {
		for _, n := range names {
			if m := mapGet(mapGet(mapGet(svcs, n), "x-runtipi"), "is_main"); m != nil && m.Value == "true" {
				r.Main = n
			}
		}
	}
	if r.Main == "" && mapGet(svcs, id) != nil {
		r.Main = id
	}
	if r.Main == "" && len(names) > 0 {
		r.Main = names[0]
	}
	r.Privileges = Privileges(root)
	s := mapGet(svcs, r.Main)
	if s == nil {
		return r, nil
	}
	if v := mapGet(s, "image"); v != nil {
		r.Image = v.Value
	}
	if v := mapGet(s, "restart"); v != nil {
		r.Restart = v.Value
	}
	if v := mapGet(s, "shm_size"); v != nil {
		r.ShmMB = ParseMemory(v.Value)
	}
	r.Desktop = isDesktopImage(r.Image)
	r.Env = envOf(mapGet(s, "environment"))
	for _, it := range seqOf(mapGet(s, "volumes")) {
		if m, ok := bindOf(it); ok {
			r.Mounts = append(r.Mounts, m)
		}
	}
	for _, p := range seqOf(mapGet(s, "ports")) {
		r.Ports = append(r.Ports, describe(p))
	}
	return r, nil
}

// jsonRecipe converte o docker-compose.json do Runtipi (schemaVersion 2) para a árvore de um compose
// comum, com as chaves que interessam (privilégios, variáveis, pastas, portas, imagem).
func jsonRecipe(raw []byte) (*yaml.Node, string, error) {
	var c struct {
		Services []map[string]json.RawMessage `json:"services"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, "", errors.New("a receita do app tem um erro")
	}
	root := newMap()
	svcs := newMap()
	mapSet(root, "services", svcs)
	main := ""
	for i, s := range c.Services {
		name := rawScalar(s["name"])
		if name == "" {
			name = "servico" + strconv.Itoa(i+1)
		}
		if rawScalar(s["isMain"]) == "true" {
			main = name
		}
		svc := newMap()
		mapSet(svcs, name, svc)
		for _, kv := range [][2]string{{"image", "image"}, {"privileged", "privileged"}, {"networkMode", "network_mode"}, {"pid", "pid"}, {"restart", "restart"}, {"shmSize", "shm_size"}} {
			if v := rawScalar(s[kv[0]]); v != "" {
				mapSet(svc, kv[1], str(v))
			}
		}
		for _, kv := range [][2]string{{"capAdd", "cap_add"}, {"devices", "devices"}, {"securityOpt", "security_opt"}} {
			var l []string
			if json.Unmarshal(s[kv[0]], &l) == nil && len(l) > 0 {
				seq := newSeq()
				for _, x := range l {
					seq.Content = append(seq.Content, str(x))
				}
				mapSet(svc, kv[1], seq)
			}
		}
		// variáveis: lista de {key, value} (formato atual) ou mapa (formato antigo)
		env := newMap()
		var kv []struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(s["environment"], &kv) == nil {
			for _, e := range kv {
				mapSet(env, e.Key, str(rawScalar(e.Value)))
			}
		} else if json.Unmarshal(s["environment"], &m) == nil {
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				mapSet(env, k, str(rawScalar(m[k])))
			}
		}
		if len(env.Content) > 0 {
			mapSet(svc, "environment", env)
		}
		var vols []struct {
			Host     string `json:"hostPath"`
			Cont     string `json:"containerPath"`
			ReadOnly bool   `json:"readOnly"`
		}
		if json.Unmarshal(s["volumes"], &vols) == nil && len(vols) > 0 {
			seq := newSeq()
			for _, v := range vols {
				x := v.Host + ":" + v.Cont
				if v.ReadOnly {
					x += ":ro"
				}
				seq.Content = append(seq.Content, str(x))
			}
			mapSet(svc, "volumes", seq)
		}
		var ports []struct {
			Cont json.RawMessage `json:"containerPort"`
			Host json.RawMessage `json:"hostPort"`
			UDP  bool            `json:"udp"`
		}
		if json.Unmarshal(s["addPorts"], &ports) == nil && len(ports) > 0 {
			seq := newSeq()
			for _, p := range ports {
				x := rawScalar(p.Host) + ":" + rawScalar(p.Cont)
				if p.UDP {
					x += "/udp"
				}
				seq.Content = append(seq.Content, str(x))
			}
			mapSet(svc, "ports", seq)
		}
	}
	return root, main, nil
}

// desktopImages são imagens do LinuxServer.io que rodam um programa de computador com tela pelo navegador
// (KasmVNC/Selkies). Navegadores baseados no Chromium travam com os 64 MB de /dev/shm do Docker; o
// LinuxServer recomenda shm_size 1gb (e a mesma lista vale para o Garimpo).
var desktopImages = map[string]bool{"brave": true, "chromium": true, "firefox": true, "librewolf": true, "mullvad-browser": true,
	"opera": true, "vivaldi": true, "ungoogled-chromium": true, "webtop": true, "obsidian": true, "libreoffice": true,
	"kicad": true, "freecad": true, "blender": true, "gimp": true, "inkscape": true, "krita": true, "digikam": true,
	"darktable": true, "kdenlive": true, "calibre": true, "rdesktop": true, "remmina": true, "steamos": true, "kasm": true,
	"signal": true, "telegram": true, "thunderbird": true, "zotero": true, "audacity": true, "handbrake": true}

func isDesktopImage(img string) bool {
	repo := imageRepoName(img)
	for _, p := range []string{"linuxserver/", "lscr.io/linuxserver/", "ghcr.io/linuxserver/"} {
		if name, ok := strings.CutPrefix(repo, p); ok {
			return desktopImages[name]
		}
	}
	return false
}

// imageRepoName tira a versão e o digest de uma imagem ("lscr.io/linuxserver/brave:1.2" → "lscr.io/linuxserver/brave").
func imageRepoName(img string) string {
	img, _, _ = strings.Cut(img, "@")
	if i := strings.LastIndex(img, ":"); i > strings.LastIndex(img, "/") {
		img = img[:i]
	}
	return img
}

// rawScalar transforma um valor JSON simples (texto, número, verdadeiro/falso) em texto.
func rawScalar(r json.RawMessage) string {
	if len(r) == 0 || string(r) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(r))
}
