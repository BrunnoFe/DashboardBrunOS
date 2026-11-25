package integrations

// Assistente de tomadas Tuya (0.31.0): em vez de tirar a chave local com o tinytuya, o BrunOS pede à
// nuvem da Tuya a lista das tomadas (nome, ID e chave local), procura na rede de casa quem atende na
// porta das tomadas e testa a chave de cada uma para achar o IP e a versão. A nuvem só é usada aqui,
// na hora de configurar; as leituras continuam só pela rede de casa.
//
// O Access ID e o Access Secret do projeto da Tuya ficam cifrados em tuya-cloud.json (como as outras
// credenciais). A chave local de cada tomada nunca vai para o navegador: o resultado da busca fica na
// memória por 15 minutos e "Atualizar"/"Adicionar" usam essa cópia.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"brunos/internal/store"
)

// TuyaRegions: data centers da nuvem da Tuya (o do projeto precisa ser o mesmo do app Smart Life/Tuya).
var TuyaRegions = []struct {
	ID, Name, Host string
}{
	{"us", "Américas (Western America)", "openapi.tuyaus.com"},
	{"us-e", "Américas (Eastern America)", "openapi-ueaz.tuyaus.com"},
	{"eu", "Europa Central", "openapi.tuyaeu.com"},
	{"eu-w", "Europa Ocidental", "openapi-weaz.tuyaeu.com"},
	{"in", "Índia", "openapi.tuyain.com"},
	{"cn", "China", "openapi.tuyacn.com"},
}

// tuyaCloudBase devolve o endereço da API de uma região (variável para os testes).
var tuyaCloudBase = func(region string) string {
	for _, r := range TuyaRegions {
		if r.ID == region {
			return "https://" + r.Host
		}
	}
	return ""
}

type tuyaCloudFile struct {
	Version  int    `json:"version"`
	Region   string `json:"region"`
	AccessID string `json:"accessId"`
	Secret   string `json:"secret"` // cifrado
	SavedAt  int64  `json:"savedAt"`
}

// TuyaFound é uma tomada da conta da Tuya, como a interface vê (sem a chave local).
type TuyaFound struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Product  string   `json:"product,omitempty"`
	Online   bool     `json:"online"`             // pela nuvem
	IP       string   `json:"ip,omitempty"`       // achada na rede de casa
	Version  string   `json:"version,omitempty"`  // protocolo que respondeu
	Power    *float64 `json:"power,omitempty"`    // W agora (para conferir)
	Known    string   `json:"known,omitempty"`    // id da integração que já usa esta tomada
	KnownAs  string   `json:"knownAs,omitempty"`  // nome dela
	Same     bool     `json:"same,omitempty"`     // a integração já tem o mesmo IP, versão e chave
	NoKey    bool     `json:"noKey,omitempty"`    // a nuvem não mandou a chave local
	NotFound bool     `json:"notFound,omitempty"` // não respondeu na rede de casa
}

type tuyaCached struct {
	TuyaFound
	key string
}

// TuyaAssist guarda a credencial da nuvem e o último resultado da busca.
type TuyaAssist struct {
	Hub  *Hub
	Path string
	// LAN devolve endereços da rede de casa (ex.: o IP interno do servidor) para achar a faixa a procurar.
	LAN func() []string

	mu     sync.Mutex
	cfg    tuyaCloudFile
	found  map[string]tuyaCached
	foundT time.Time
}

// NewTuyaAssist lê a credencial guardada (se houver).
func NewTuyaAssist(h *Hub, path string) *TuyaAssist {
	a := &TuyaAssist{Hub: h, Path: path}
	if path != "" {
		if _, err := store.ReadJSON(path, &a.cfg); err != nil {
			slog.Warn("assistente tuya: arquivo ilegível", "erro", err)
		}
	}
	return a
}

// ── nuvem da Tuya ──────────────────────────────────────────────────────────

type tuyaCloud struct {
	base, id, secret string
	client           *http.Client
	token            string
}

// tuyaSign assina um pedido como a Tuya pede (HMAC-SHA256 de client_id + token + t + método, hash do
// corpo, cabeçalhos assinados (nenhum) e caminho com a consulta), em hexadecimal maiúsculo.
func tuyaSign(id, token, secret, t, method, path string, body []byte) string {
	sum := sha256.Sum256(body)
	str := method + "\n" + hex.EncodeToString(sum[:]) + "\n\n" + path
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(id + token + t + str))
	return strings.ToUpper(hex.EncodeToString(m.Sum(nil)))
}

type tuyaReply struct {
	Success bool            `json:"success"`
	Code    int             `json:"code"`
	Msg     string          `json:"msg"`
	Result  json.RawMessage `json:"result"`
}

// tuyaCloudErr traduz os erros comuns da nuvem da Tuya.
func tuyaCloudErr(code int, msg string) error {
	low := strings.ToLower(msg)
	switch {
	case code == 1004 || strings.Contains(low, "sign invalid"):
		return ferr("o Access ID ou o Access Secret está errado (confira no projeto, em Cloud → Development → Overview)")
	case strings.Contains(low, "data center") || code == 28841002:
		return ferr("a região escolhida não está liberada no projeto da Tuya: escolha a região do seu app (no Brasil, normalmente Western America) e confira em Cloud → Development → Overview → Data Center")
	case code == 1106 || strings.Contains(low, "permission deny"):
		return ferr("a conta do app Smart Life/Tuya não está ligada ao projeto: no projeto, vá em Devices → Link App Account e leia o QR code pelo app")
	case code == 28841101 || code == 28841105 || strings.Contains(low, "not authorized") || strings.Contains(low, "no permission"):
		return ferr("o projeto da Tuya não tem permissão para listar as tomadas: confira se o serviço IoT Core está ativo e se o plano gratuito não venceu (Cloud → Cloud Services)")
	case code == 1010 || code == 1011:
		return ferr("a nuvem da Tuya recusou o acesso (token vencido): tente de novo")
	}
	if msg == "" {
		msg = "sem detalhes"
	}
	return ferr("a nuvem da Tuya respondeu: %s (código %d)", msg, code)
}

func (c *tuyaCloud) do(ctx context.Context, path string, out any) error {
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("client_id", c.id)
	req.Header.Set("t", t)
	req.Header.Set("sign_method", "HMAC-SHA256")
	req.Header.Set("sign", tuyaSign(c.id, c.token, c.secret, t, http.MethodGet, path, nil))
	if c.token != "" {
		req.Header.Set("access_token", c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		slog.Warn("assistente tuya: nuvem", "erro", err)
		return ferr("não consegui falar com a nuvem da Tuya (confira a internet do servidor)")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return ferr("a nuvem da Tuya não terminou de responder")
	}
	var r tuyaReply
	if err := json.Unmarshal(b, &r); err != nil {
		return ferr("resposta estranha da nuvem da Tuya (HTTP %d)", resp.StatusCode)
	}
	if !r.Success {
		return tuyaCloudErr(r.Code, r.Msg)
	}
	if out != nil {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return ferr("resposta estranha da nuvem da Tuya")
		}
	}
	return nil
}

func (c *tuyaCloud) login(ctx context.Context) error {
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := c.do(ctx, "/v1.0/token?grant_type=1", &tok); err != nil {
		return err
	}
	if tok.AccessToken == "" {
		return ferr("a nuvem da Tuya não devolveu o acesso")
	}
	c.token = tok.AccessToken
	return nil
}

type tuyaCloudDevice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LocalKey string `json:"local_key"`
	Product  string `json:"product_name"`
	Category string `json:"category"`
	Online   bool   `json:"online"`
}

// devices lista os aparelhos ligados à conta do app (até 500).
func (c *tuyaCloud) devices(ctx context.Context) ([]tuyaCloudDevice, error) {
	var all []tuyaCloudDevice
	last := ""
	for range 5 {
		q := url.Values{"size": {"100"}}
		if last != "" {
			q.Set("last_row_key", last)
		}
		var page struct {
			Devices []tuyaCloudDevice `json:"devices"`
			HasMore bool              `json:"has_more"`
			LastRow string            `json:"last_row_key"`
		}
		if err := c.do(ctx, "/v1.0/iot-01/associated-users/devices?"+q.Encode(), &page); err != nil {
			return nil, err
		}
		all = append(all, page.Devices...)
		if !page.HasMore || page.LastRow == "" || page.LastRow == last {
			break
		}
		last = page.LastRow
	}
	return all, nil
}

func (a *TuyaAssist) cloud(cfg tuyaCloudFile, secret string) (*tuyaCloud, error) {
	base := tuyaCloudBase(cfg.Region)
	if base == "" {
		return nil, &ErrInput{"região inválida"}
	}
	client := http.DefaultClient
	if a.Hub != nil && a.Hub.Client != nil {
		client = a.Hub.Client
	}
	return &tuyaCloud{base: base, id: cfg.AccessID, secret: secret, client: client}, nil
}

// ── credencial ─────────────────────────────────────────────────────────────

// TuyaCloudInfo é o que a interface vê da credencial (sem o segredo).
type TuyaCloudInfo struct {
	Configured bool   `json:"configured"`
	Region     string `json:"region,omitempty"`
	AccessID   string `json:"accessId,omitempty"` // só o começo e o fim
	SavedAt    int64  `json:"savedAt,omitempty"`
}

func maskID(s string) string {
	if len(s) <= 8 {
		return strings.Repeat("•", len(s))
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

func (a *TuyaAssist) Info() TuyaCloudInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.AccessID == "" || a.cfg.Secret == "" {
		return TuyaCloudInfo{}
	}
	return TuyaCloudInfo{Configured: true, Region: a.cfg.Region, AccessID: maskID(a.cfg.AccessID), SavedAt: a.cfg.SavedAt}
}

func validCloudID(s string) bool {
	if len(s) < 8 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// SaveCloud confere a credencial na nuvem (pede o acesso) e guarda cifrada.
func (a *TuyaAssist) SaveCloud(ctx context.Context, region, id, secret string) error {
	id, secret = strings.TrimSpace(id), strings.TrimSpace(secret)
	if !validCloudID(id) {
		return &ErrInput{"Access ID inválido (só letras e números, como aparece no projeto da Tuya)"}
	}
	if !validCloudID(secret) {
		return &ErrInput{"Access Secret inválido (só letras e números, como aparece no projeto da Tuya)"}
	}
	cfg := tuyaCloudFile{Version: 1, Region: region, AccessID: id}
	c, err := a.cloud(cfg, secret)
	if err != nil {
		return err
	}
	if err := c.login(ctx); err != nil {
		return err
	}
	sealed, err := a.Hub.Store.box.Seal(secret)
	if err != nil {
		return err
	}
	cfg.Secret, cfg.SavedAt = sealed, time.Now().Unix()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg = cfg
	return a.saveLocked()
}

// ForgetCloud apaga a credencial e a busca na memória.
func (a *TuyaAssist) ForgetCloud() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg, a.found = tuyaCloudFile{}, nil
	return a.saveLocked()
}

func (a *TuyaAssist) saveLocked() error {
	if a.Path == "" {
		return nil
	}
	return store.WriteJSON(a.Path, a.cfg, 0o600)
}

// ── busca na rede de casa ──────────────────────────────────────────────────

// variáveis para os testes
var (
	tuyaPortOpen = func(ctx context.Context, ip string) bool {
		d := net.Dialer{Timeout: 700 * time.Millisecond}
		c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, tuyaPort))
		if err != nil {
			return false
		}
		c.Close()
		return true
	}
	tuyaProbe = func(ctx context.Context, ip, dev, key, ver string) (map[string]any, error) {
		return tuyaStatusOnce(ctx, ip, dev, key, ver)
	}
	tuyaProbeTimeout = 3 * time.Second
)

// lanPrefixes devolve as faixas /24 da rede de casa (só endereços privados IPv4) a partir dos endereços
// conhecidos: IP interno do servidor e IPs das tomadas já cadastradas.
func lanPrefixes(addrs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range addrs {
		ip := net.ParseIP(strings.TrimSpace(s)).To4()
		if ip == nil || !ip.IsPrivate() {
			continue
		}
		p := fmt.Sprintf("%d.%d.%d.", ip[0], ip[1], ip[2])
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// openPorts procura, em cada faixa, os endereços que atendem na porta das tomadas Tuya.
func openPorts(ctx context.Context, prefixes []string) []string {
	var (
		mu  sync.Mutex
		out []string
		wg  sync.WaitGroup
	)
	sem := make(chan struct{}, 64)
	for _, p := range prefixes {
		for i := 1; i <= 254; i++ {
			ip := p + strconv.Itoa(i)
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if tuyaPortOpen(ctx, ip) {
					mu.Lock()
					out = append(out, ip)
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()
	sort.Strings(out)
	return out
}

func dpsPower(dps map[string]any) *float64 {
	switch v := dps["19"].(type) {
	case float64:
		w := round1(v / 10)
		return &w
	case json.Number:
		if f, err := v.Float64(); err == nil {
			w := round1(f / 10)
			return &w
		}
	}
	return nil
}

// match testa as chaves nas tomadas achadas: uma tomada por endereço, um pedido por vez em cada
// endereço (elas atendem uma conexão por vez); endereços diferentes ao mesmo tempo.
func match(ctx context.Context, ips []string, devs []tuyaCloudDevice) map[string]tuyaCached {
	type hit struct {
		id, ip, ver string
		power       *float64
	}
	var (
		mu    sync.Mutex
		taken = map[string]bool{}
		hits  []hit
		wg    sync.WaitGroup
	)
	for _, ip := range ips {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, d := range devs {
				if len(d.LocalKey) != 16 {
					continue
				}
				mu.Lock()
				done := taken[d.ID]
				mu.Unlock()
				if done {
					continue
				}
				for _, ver := range []string{"3.5", "3.4", "3.3"} {
					pctx, cancel := context.WithTimeout(ctx, tuyaProbeTimeout)
					dps, err := tuyaProbe(pctx, ip, d.ID, d.LocalKey, ver)
					cancel()
					if err == nil && dps != nil {
						mu.Lock()
						if !taken[d.ID] {
							taken[d.ID] = true
							hits = append(hits, hit{d.ID, ip, ver, dpsPower(dps)})
						}
						mu.Unlock()
						return // este endereço já tem dono
					}
					if ctx.Err() != nil {
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	out := map[string]tuyaCached{}
	for _, h := range hits {
		out[h.id] = tuyaCached{TuyaFound: TuyaFound{IP: h.ip, Version: h.ver, Power: h.power}}
	}
	return out
}

// Scan pede a lista à nuvem, procura na rede e devolve as tomadas (com o que já está cadastrado).
func (a *TuyaAssist) Scan(ctx context.Context) ([]TuyaFound, error) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if cfg.AccessID == "" || cfg.Secret == "" {
		return nil, &ErrInput{"informe primeiro o Access ID e o Access Secret do projeto da Tuya"}
	}
	secret, err := a.Hub.Store.box.Open(cfg.Secret)
	if err != nil {
		return nil, ferr("não deu para ler a credencial da Tuya guardada: informe de novo")
	}
	c, err := a.cloud(cfg, secret)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.login(cctx); err != nil {
		return nil, err
	}
	devs, err := c.devices(cctx)
	if err != nil {
		return nil, err
	}
	// tomadas: categoria "cz" (tomada) e "pc" (filtro de linha), ou qualquer aparelho com chave local
	var plugs []tuyaCloudDevice
	for _, d := range devs {
		if d.ID != "" {
			plugs = append(plugs, d)
		}
	}
	if len(plugs) == 0 {
		return nil, ferr("a conta do app ligada ao projeto não tem nenhum aparelho")
	}

	// faixas da rede: IP interno do servidor e IPs das tomadas já cadastradas
	var addrs []string
	if a.LAN != nil {
		addrs = a.LAN()
	}
	known := map[string]Integration{}
	for _, it := range a.Hub.Store.List() {
		if it.Kind == "tuya" {
			addrs = append(addrs, it.Options["host"])
			known[it.Options["devid"]] = it
		}
	}
	prefixes := lanPrefixes(addrs)
	if len(prefixes) == 0 {
		return nil, ferr("não sei a faixa da rede de casa (o Runtipi não informou o IP interno do servidor)")
	}
	sctx, cancel2 := context.WithTimeout(ctx, 90*time.Second)
	defer cancel2()
	hits := match(sctx, openPorts(sctx, prefixes), plugs)

	found := map[string]tuyaCached{}
	list := make([]TuyaFound, 0, len(plugs))
	for _, d := range plugs {
		f := TuyaFound{ID: d.ID, Name: strings.TrimSpace(d.Name), Product: d.Product, Online: d.Online, NoKey: len(d.LocalKey) != 16}
		if f.Name == "" {
			f.Name = "Tomada " + d.ID[:min(6, len(d.ID))]
		}
		if h, ok := hits[d.ID]; ok {
			f.IP, f.Version, f.Power = h.IP, h.Version, h.Power
		} else if !f.NoKey {
			f.NotFound = true
		}
		if it, ok := known[d.ID]; ok {
			f.Known, f.KnownAs = it.ID, it.Name
			if f.IP != "" && it.Options["host"] == f.IP && normVer(it.Options["version"]) == f.Version {
				if cfg, err := a.Hub.Store.Config(it); err == nil && cfg.Secret == d.LocalKey {
					f.Same = true
				}
			}
		}
		if f.IP != "" {
			found[d.ID] = tuyaCached{TuyaFound: f, key: d.LocalKey}
		}
		list = append(list, f)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].IP != "" && list[j].IP == "" })
	a.mu.Lock()
	a.found, a.foundT = found, time.Now()
	a.mu.Unlock()
	return list, nil
}

func normVer(v string) string {
	if v = strings.TrimSpace(v); v == "" {
		return "3.5"
	}
	return v
}

const tuyaFoundTTL = 15 * time.Minute

// Apply grava uma tomada achada na busca: "update" no cadastro que já usa a tomada (mesmo ID; widgets,
// Medidor e histórico continuam ligados nele) ou "add" num cadastro novo.
func (a *TuyaAssist) Apply(id, mode, name string) (Integration, error) {
	a.mu.Lock()
	f, ok := a.found[id]
	fresh := time.Since(a.foundT) < tuyaFoundTTL
	a.mu.Unlock()
	if !ok || !fresh {
		return Integration{}, &ErrInput{"procure as tomadas de novo (a busca vale por 15 minutos)"}
	}
	key := f.key
	switch mode {
	case "update":
		var prev Integration
		found := false
		for _, it := range a.Hub.Store.List() {
			if it.Kind == "tuya" && it.Options["devid"] == id {
				prev, found = it, true
				break
			}
		}
		if !found {
			return Integration{}, &ErrInput{"essa tomada ainda não está cadastrada: use Adicionar"}
		}
		opts := map[string]string{}
		for k, v := range prev.Options {
			opts[k] = v
		}
		opts["host"], opts["devid"], opts["version"] = f.IP, id, f.Version
		it, err := a.Hub.Store.Update(prev.ID, Input{Name: prev.Name, Interval: prev.Interval, Alerts: prev.Alerts, Options: opts, Secret: &key})
		if err == nil {
			a.Hub.Changed(it.ID, false)
		}
		return it, err
	case "add":
		name = strings.TrimSpace(name)
		if name == "" {
			name = f.Name
		}
		it, err := a.Hub.Store.Add(Input{Kind: "tuya", Name: name, Secret: &key,
			Options: map[string]string{"host": f.IP, "devid": id, "version": f.Version},
			Alerts:  map[string]AlertSetting{"down": {On: true}}})
		if err == nil {
			a.Hub.Changed(it.ID, false)
		}
		return it, err
	}
	return Integration{}, &ErrInput{"escolha Atualizar ou Adicionar"}
}

// ── rotas ──────────────────────────────────────────────────────────────────

// Register liga /api/tuya/*. As que usam ou mudam a credencial pedem a senha do BrunOS de novo (o
// main põe essas rotas no modo sudo).
func (a *TuyaAssist) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/tuya/cloud", wrap(func(w http.ResponseWriter, r *http.Request) {
		regions := make([]map[string]string, len(TuyaRegions))
		for i, rg := range TuyaRegions {
			regions[i] = map[string]string{"id": rg.ID, "name": rg.Name}
		}
		writeJSON(w, 200, map[string]any{"cloud": a.Info(), "regions": regions})
	}))
	mux.HandleFunc("PUT /api/tuya/cloud", wrap(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Region, AccessID, Secret string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeErr(w, &ErrInput{"pedido inválido"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if err := a.SaveCloud(ctx, body.Region, body.AccessID, body.Secret); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"cloud": a.Info()})
	}))
	mux.HandleFunc("DELETE /api/tuya/cloud", wrap(func(w http.ResponseWriter, r *http.Request) {
		if err := a.ForgetCloud(); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/tuya/scan", wrap(func(w http.ResponseWriter, r *http.Request) {
		list, err := a.Scan(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"plugs": list})
	}))
	mux.HandleFunc("POST /api/tuya/apply", wrap(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ ID, Mode, Name string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeErr(w, &ErrInput{"pedido inválido"})
			return
		}
		it, err := a.Apply(body.ID, body.Mode, body.Name)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				err = &ErrInput{"o cadastro da tomada não existe mais"}
			}
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, it.Public())
	}))
}
