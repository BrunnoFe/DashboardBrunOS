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
func recipeHostOK(h string) bool {
	if !strings.HasPrefix(h, "/") && !strings.HasPrefix(h, "$") {
		return true // volume com nome ou caminho relativo à pasta do app
	}
	if strings.Contains(h, "docker.sock") || strings.Contains(h, "..") {
		return false
	}
	for _, p := range []string{"${APP_DATA_DIR}", "$APP_DATA_DIR", "${ROOT_FOLDER_HOST}/media", "${ROOT_FOLDER_HOST}/app-data"} {
		if h == p || strings.HasPrefix(h, p+"/") {
			return true
		}
	}
	if strings.HasPrefix(h, "$") {
		return false // outra variável: não dá para saber onde cai
	}
	c := path.Clean(h)
	return livres[c] || strings.HasPrefix(c, "/mnt/") || strings.HasPrefix(c, "/home/") || strings.HasPrefix(c, "/dev/dri")
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
	svcs := mapGet(root, "services")
	for _, name := range mapKeys(svcs) {
		s := mapGet(svcs, name)
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
			}
		}
	}
	return out
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
		for _, kv := range [][2]string{{"image", "image"}, {"privileged", "privileged"}, {"networkMode", "network_mode"}, {"pid", "pid"}, {"restart", "restart"}} {
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
