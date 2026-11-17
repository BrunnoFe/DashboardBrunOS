// Package appcfg lê e edita os Ajustes de um app (0.20.0, decisão 61): o arquivo
// user-config/<loja>/<app>/docker-compose.yml que o Runtipi junta ao compose do app em todo início.
//
// O arquivo pode já ter ajustes feitos à mão pelo Bruno no editor do Runtipi. Por isso a edição é feita
// na árvore do YAML (gopkg.in/yaml.v3): só o que o BrunOS entende (pastas, memória, núcleos, placa de
// vídeo, variáveis do serviço principal) muda; o resto do arquivo, comentários e ordem ficam como estavam.
//
// Segurança: o Runtipi não confere nada nesse arquivo. O BrunOS confere: os Ajustes nunca podem ACRESCENTAR
// algo perigoso (privileged, rede do servidor, socket do Docker, pastas do sistema...). O que já estava
// escrito à mão continua lá (é do Bruno), mas não pode crescer.
package appcfg

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxFile é o tamanho máximo do arquivo de ajustes que o BrunOS lê e grava.
const MaxFile = 64 << 10

// Mount é uma pasta do servidor dentro do app.
type Mount struct {
	Host      string `json:"host"`      // no servidor: /mnt/disco1/Filmes
	Container string `json:"container"` // dentro do app: /movies
	ReadOnly  bool   `json:"readOnly"`
}

// Var é uma variável do app.
type Var struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Settings são os Ajustes que a tela mostra e edita.
type Settings struct {
	Mounts   []Mount `json:"mounts"`
	MemoryMB int     `json:"memoryMB"` // 0 = sem limite
	CPUs     float64 `json:"cpus"`     // 0 = sem limite
	GPU      bool    `json:"gpu"`      // /dev/dri (placa de vídeo, para converter vídeo)
	Env      []Var   `json:"env"`
}

// State é o que o arquivo tem hoje.
type State struct {
	Settings
	Exists bool     `json:"exists"` // o arquivo existe e não está vazio
	Manual []string `json:"manual"` // o que o arquivo tem além do que o BrunOS edita (fica como está)
	Risks  []string `json:"risks"`  // coisas perigosas que já estavam escritas à mão
}

var (
	envKeyRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	memRe       = regexp.MustCompile(`^(?i)\s*([0-9]+(?:\.[0-9]+)?)\s*([kmgt]?)(?:i?b|i)?\s*$`)
	containerRe = regexp.MustCompile(`^/[A-Za-z0-9._/@+-]{0,200}$`)
)

// ── árvore do YAML ───────────────────────────────────────────────────────────────────────────────

func mapGet(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func mapSet(n *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content[i+1] = v
			return
		}
	}
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}

func mapDel(n *yaml.Node, key string) {
	if n == nil || n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content = append(n.Content[:i], n.Content[i+2:]...)
			return
		}
	}
}

func mapKeys(n *yaml.Node) []string {
	var out []string
	if n == nil || n.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, n.Content[i].Value)
	}
	return out
}

func newMap() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }
func newSeq() *yaml.Node { return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"} }
func str(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// child devolve (criando, se precisar) o mapa em n[key].
func child(n *yaml.Node, key string) *yaml.Node {
	c := mapGet(n, key)
	if c == nil || c.Kind != yaml.MappingNode {
		c = newMap()
		mapSet(n, key, c)
	}
	return c
}

func parse(raw []byte) (*yaml.Node, *yaml.Node, error) {
	if len(raw) > MaxFile {
		return nil, nil, errors.New("o arquivo de ajustes é grande demais")
	}
	var doc yaml.Node
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, nil, fmt.Errorf("o arquivo de ajustes tem um erro de YAML: %v", err)
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{newMap()}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, errors.New("o arquivo de ajustes não tem o formato de um docker-compose")
	}
	return &doc, doc.Content[0], nil
}

// ── leitura ──────────────────────────────────────────────────────────────────────────────────────

// splitVolume separa "origem:destino[:modo]".
func splitVolume(s string) (host, cont string, ro bool, ok bool) {
	p := strings.Split(s, ":")
	if len(p) < 2 || len(p) > 3 {
		return "", "", false, false
	}
	if len(p) == 3 {
		for _, m := range strings.Split(p[2], ",") {
			if m == "ro" {
				ro = true
			}
		}
	}
	return p[0], p[1], ro, true
}

// bindOf entende um item de volumes como pasta do servidor (só as que começam com / ou com variável).
func bindOf(it *yaml.Node) (Mount, bool) {
	isHost := func(h string) bool { return strings.HasPrefix(h, "/") || strings.HasPrefix(h, "${") }
	switch it.Kind {
	case yaml.ScalarNode:
		h, c, ro, ok := splitVolume(it.Value)
		if ok && isHost(h) {
			return Mount{Host: h, Container: c, ReadOnly: ro}, true
		}
	case yaml.MappingNode:
		t, src, dst := mapGet(it, "type"), mapGet(it, "source"), mapGet(it, "target")
		if (t == nil || t.Value == "bind") && src != nil && dst != nil && isHost(src.Value) {
			ro := mapGet(it, "read_only")
			return Mount{Host: src.Value, Container: dst.Value, ReadOnly: ro != nil && ro.Value == "true"}, true
		}
	}
	return Mount{}, false
}

// ParseMemory entende "2G", "512m", "1.5GB", "1073741824" (bytes) e devolve MB.
func ParseMemory(s string) int {
	m := memRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	switch strings.ToLower(m[2]) {
	case "":
		v /= 1 << 20
	case "k":
		v /= 1 << 10
	case "g":
		v *= 1 << 10
	case "t":
		v *= 1 << 20
	}
	return int(v + 0.5)
}

func memoryString(mb int) string {
	if mb%1024 == 0 {
		return strconv.Itoa(mb/1024) + "G"
	}
	return strconv.Itoa(mb) + "M"
}

func envOf(n *yaml.Node) []Var {
	out := []Var{}
	switch {
	case n == nil:
	case n.Kind == yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			out = append(out, Var{Key: n.Content[i].Value, Value: n.Content[i+1].Value})
		}
	case n.Kind == yaml.SequenceNode:
		for _, it := range n.Content {
			k, v, _ := strings.Cut(it.Value, "=")
			out = append(out, Var{Key: k, Value: v})
		}
	}
	return out
}

// managed são as chaves do serviço que a tela edita (o resto aparece como "feito à mão").
var managed = map[string]bool{"volumes": true, "environment": true, "deploy": true, "mem_limit": true, "cpus": true, "devices": true}

// Parse lê o arquivo de ajustes do serviço principal do app.
func Parse(raw []byte, service string) (State, error) {
	st := State{Settings: Settings{Mounts: []Mount{}, Env: []Var{}}, Manual: []string{}, Risks: []string{}}
	_, root, err := parse(raw)
	if err != nil {
		return st, err
	}
	st.Exists = len(root.Content) > 0
	st.Risks = Risks(root)
	for _, k := range mapKeys(root) {
		if k != "services" {
			st.Manual = append(st.Manual, "chave "+k+" no arquivo")
		}
	}
	svcs := mapGet(root, "services")
	for _, name := range mapKeys(svcs) {
		if name != service {
			st.Manual = append(st.Manual, "ajustes do serviço "+name)
		}
	}
	s := mapGet(svcs, service)
	if s == nil {
		return st, nil
	}
	for _, k := range mapKeys(s) {
		if !managed[k] {
			st.Manual = append(st.Manual, k)
		}
	}
	if v := mapGet(s, "volumes"); v != nil && v.Kind == yaml.SequenceNode {
		for _, it := range v.Content {
			if m, ok := bindOf(it); ok {
				st.Mounts = append(st.Mounts, m)
			} else {
				st.Manual = append(st.Manual, "volume "+describe(it))
			}
		}
	}
	st.Env = envOf(mapGet(s, "environment"))
	limits := mapGet(mapGet(mapGet(s, "deploy"), "resources"), "limits")
	if m := mapGet(limits, "memory"); m != nil {
		st.MemoryMB = ParseMemory(m.Value)
	} else if m := mapGet(s, "mem_limit"); m != nil {
		st.MemoryMB = ParseMemory(m.Value)
	}
	if c := mapGet(limits, "cpus"); c != nil {
		st.CPUs, _ = strconv.ParseFloat(c.Value, 64)
	} else if c := mapGet(s, "cpus"); c != nil {
		st.CPUs, _ = strconv.ParseFloat(c.Value, 64)
	}
	if d := mapGet(s, "deploy"); d != nil {
		for _, k := range mapKeys(d) {
			if k != "resources" {
				st.Manual = append(st.Manual, "deploy."+k)
			}
		}
		for _, k := range mapKeys(mapGet(d, "resources")) {
			if k != "limits" {
				st.Manual = append(st.Manual, "deploy.resources."+k)
			}
		}
	}
	for _, it := range seqOf(mapGet(s, "devices")) {
		if strings.HasPrefix(it.Value, "/dev/dri") {
			st.GPU = true
		} else {
			st.Manual = append(st.Manual, "aparelho "+describe(it))
		}
	}
	return st, nil
}

func seqOf(n *yaml.Node) []*yaml.Node {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	return n.Content
}

func describe(n *yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	b, _ := yaml.Marshal(n)
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\n", " "))
}

// ── conferência ──────────────────────────────────────────────────────────────────────────────────

var capsRuins = map[string]bool{"ALL": true, "SYS_ADMIN": true, "NET_ADMIN": true, "SYS_MODULE": true, "SYS_RAWIO": true,
	"SYS_PTRACE": true, "SYS_BOOT": true, "SYS_TIME": true, "DAC_READ_SEARCH": true, "MAC_ADMIN": true, "BPF": true}

// HostOK diz se uma pasta do servidor pode entrar nos Ajustes pelo BrunOS: só dentro de /mnt e /home
// (discos e pastas de usuário), da pasta de mídia do Runtipi ou da pasta do próprio app, sem "..".
func HostOK(h string) bool {
	if h == "" || len(h) > 300 || strings.ContainsAny(h, "\n\r\x00:") || strings.Contains(h, "..") {
		return false
	}
	for _, p := range []string{"${ROOT_FOLDER_HOST}/media", "${APP_DATA_DIR}/"} {
		if strings.HasPrefix(h, p) {
			return !strings.Contains(h[len(p):], "$")
		}
	}
	c := path.Clean(h)
	if c != h {
		return false
	}
	return strings.HasPrefix(c, "/mnt/") || strings.HasPrefix(c, "/home/")
}

// Risks lista o que é perigoso no arquivo inteiro (todos os serviços).
func Risks(root *yaml.Node) []string {
	out := []string{}
	add := func(s string) {
		for _, x := range out {
			if x == s {
				return
			}
		}
		out = append(out, s)
	}
	svcs := mapGet(root, "services")
	for _, name := range mapKeys(svcs) {
		s := mapGet(svcs, name)
		if v := mapGet(s, "privileged"); v != nil && v.Value == "true" {
			add(name + ": privileged")
		}
		if v := mapGet(s, "network_mode"); v != nil && (v.Value == "host" || strings.HasPrefix(v.Value, "container:")) {
			add(name + ": rede " + v.Value)
		}
		for _, k := range []string{"pid", "ipc", "userns_mode", "uts", "cgroup"} {
			if v := mapGet(s, k); v != nil && v.Value == "host" {
				add(name + ": " + k + " do servidor")
			}
		}
		for _, c := range seqOf(mapGet(s, "cap_add")) {
			if capsRuins[strings.TrimPrefix(strings.ToUpper(c.Value), "CAP_")] {
				add(name + ": poder " + c.Value)
			}
		}
		for _, d := range seqOf(mapGet(s, "devices")) {
			src := d.Value
			if d.Kind == yaml.MappingNode {
				if x := mapGet(d, "source"); x != nil {
					src = x.Value
				}
			}
			if !strings.HasPrefix(strings.SplitN(src, ":", 2)[0], "/dev/dri") {
				add(name + ": aparelho " + src)
			}
		}
		for _, o := range seqOf(mapGet(s, "security_opt")) {
			if strings.Contains(o.Value, "unconfined") {
				add(name + ": " + o.Value)
			}
		}
		for _, it := range seqOf(mapGet(s, "volumes")) {
			m, ok := bindOf(it)
			if !ok {
				continue
			}
			if strings.Contains(m.Host, "docker.sock") || !HostOK(m.Host) {
				add(name + ": pasta " + m.Host)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ── edição ───────────────────────────────────────────────────────────────────────────────────────

// Validate confere os Ajustes que vieram da tela.
func Validate(s Settings) error {
	if len(s.Mounts) > 30 || len(s.Env) > 60 {
		return errors.New("ajustes demais")
	}
	seen := map[string]bool{}
	for _, m := range s.Mounts {
		if !HostOK(m.Host) {
			return fmt.Errorf("a pasta %q não pode ser usada: escolha um disco de /mnt, uma pasta de /home ou a pasta de mídia do Runtipi", m.Host)
		}
		if !containerRe.MatchString(m.Container) || m.Container == "/" || strings.Contains(m.Container, "..") || path.Clean(m.Container) != m.Container {
			return fmt.Errorf("o destino %q dentro do app é inválido (use algo como /media)", m.Container)
		}
		if seen[m.Container] {
			return fmt.Errorf("o destino %s aparece duas vezes", m.Container)
		}
		seen[m.Container] = true
	}
	if s.MemoryMB != 0 && (s.MemoryMB < 64 || s.MemoryMB > 1<<20) {
		return errors.New("limite de memória inválido")
	}
	if s.CPUs != 0 && (s.CPUs < 0.1 || s.CPUs > 256) {
		return errors.New("limite de núcleos inválido")
	}
	keys := map[string]bool{}
	for _, v := range s.Env {
		if !envKeyRe.MatchString(v.Key) {
			return fmt.Errorf("nome de variável inválido: %q", v.Key)
		}
		if keys[v.Key] {
			return fmt.Errorf("a variável %s aparece duas vezes", v.Key)
		}
		keys[v.Key] = true
		if len(v.Value) > 2000 || strings.ContainsAny(v.Value, "\n\r\x00") {
			return fmt.Errorf("o valor de %s é inválido", v.Key)
		}
	}
	return nil
}

// Apply devolve o arquivo com os Ajustes novos no serviço principal, mexendo só no que a tela edita.
// Pastas que já estavam no arquivo (mesmo fora das regras, se foram escritas à mão) podem continuar;
// pastas novas passam por HostOK.
func Apply(raw []byte, service string, s Settings) ([]byte, error) {
	doc, root, err := parse(raw)
	if err != nil {
		return nil, err
	}
	before := Risks(root)
	svcs := child(root, "services")
	svc := child(svcs, service)

	old := map[Mount]*yaml.Node{}
	vols := mapGet(svc, "volumes")
	var keep []*yaml.Node
	for _, it := range seqOf(vols) {
		if m, ok := bindOf(it); ok {
			old[m] = it
		} else {
			keep = append(keep, it)
		}
	}
	// só as pastas novas precisam seguir as regras (as que já estavam são do Bruno)
	check := s
	check.Mounts = nil
	for _, m := range s.Mounts {
		if _, ok := old[m]; !ok {
			check.Mounts = append(check.Mounts, m)
		}
	}
	if err := Validate(check); err != nil {
		return nil, err
	}
	seenDst := map[string]bool{}
	for _, m := range s.Mounts {
		if seenDst[m.Container] {
			return nil, fmt.Errorf("o destino %s aparece duas vezes", m.Container)
		}
		seenDst[m.Container] = true
	}

	// pastas: as que não mudaram reaproveitam o item original (com o comentário dele)
	if len(s.Mounts) == 0 && len(keep) == 0 {
		mapDel(svc, "volumes")
	} else {
		seq := newSeq()
		if vols != nil && vols.Kind == yaml.SequenceNode {
			seq.Style, seq.HeadComment, seq.LineComment = vols.Style, vols.HeadComment, vols.LineComment
		}
		seq.Content = append(seq.Content, keep...)
		for _, m := range s.Mounts {
			if it, ok := old[m]; ok {
				seq.Content = append(seq.Content, it)
				continue
			}
			v := m.Host + ":" + m.Container
			if m.ReadOnly {
				v += ":ro"
			}
			seq.Content = append(seq.Content, str(v))
		}
		mapSet(svc, "volumes", seq)
	}

	// variáveis: mantém o formato que já estava (mapa ou lista) e os itens que não mudaram
	env := mapGet(svc, "environment")
	switch {
	case len(s.Env) == 0:
		mapDel(svc, "environment")
	case env != nil && env.Kind == yaml.SequenceNode:
		seq := newSeq()
		for _, v := range s.Env {
			seq.Content = append(seq.Content, str(v.Key+"="+v.Value))
		}
		mapSet(svc, "environment", seq)
	default:
		m := newMap()
		for _, v := range s.Env {
			var n *yaml.Node
			if o := mapGet(env, v.Key); o != nil && o.Value == v.Value {
				n = o
			}
			if n == nil {
				n = str(v.Value)
			}
			mapSet(m, v.Key, n)
		}
		mapSet(svc, "environment", m)
	}

	// memória e núcleos: em deploy.resources.limits (ou mem_limit/cpus, se o arquivo já usava assim)
	setLimit := func(short, long, val string) {
		if mapGet(svc, short) != nil {
			if val == "" {
				mapDel(svc, short)
			} else {
				mapSet(svc, short, str(val))
			}
			return
		}
		if val == "" {
			d := mapGet(svc, "deploy")
			r := mapGet(d, "resources")
			mapDel(mapGet(r, "limits"), long)
			prune(r, "limits")
			prune(d, "resources")
			prune(svc, "deploy")
			return
		}
		mapSet(child(child(child(svc, "deploy"), "resources"), "limits"), long, str(val))
	}
	mem, cpu := "", ""
	if s.MemoryMB > 0 {
		mem = memoryString(s.MemoryMB)
	}
	if s.CPUs > 0 {
		cpu = strconv.FormatFloat(s.CPUs, 'f', -1, 64)
	}
	setLimit("mem_limit", "memory", mem)
	setLimit("cpus", "cpus", cpu)

	// placa de vídeo
	seq := newSeq()
	has := false
	for _, it := range seqOf(mapGet(svc, "devices")) {
		if strings.HasPrefix(it.Value, "/dev/dri") {
			if s.GPU && !has {
				seq.Content = append(seq.Content, it)
				has = true
			}
			continue
		}
		seq.Content = append(seq.Content, it)
	}
	if s.GPU && !has {
		seq.Content = append(seq.Content, str("/dev/dri:/dev/dri"))
	}
	if len(seq.Content) == 0 {
		mapDel(svc, "devices")
	} else {
		mapSet(svc, "devices", seq)
	}

	prune(svcs, service)
	prune(root, "services")

	// nada perigoso pode aparecer por causa dos Ajustes (o que já estava escrito à mão continua)
	had := map[string]bool{}
	for _, r := range before {
		had[r] = true
	}
	for _, r := range Risks(root) {
		if !had[r] {
			return nil, fmt.Errorf("ajuste recusado por segurança (%s)", r)
		}
	}
	if len(root.Content) == 0 {
		return []byte{}, nil
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	_ = enc.Close()
	if b.Len() > MaxFile {
		return nil, errors.New("o arquivo de ajustes ficaria grande demais")
	}
	return b.Bytes(), nil
}

// prune tira n[key] se ele ficou um mapa vazio.
func prune(n *yaml.Node, key string) {
	if c := mapGet(n, key); c != nil && c.Kind == yaml.MappingNode && len(c.Content) == 0 {
		mapDel(n, key)
	}
}
