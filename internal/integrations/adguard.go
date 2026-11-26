package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// AdGuard Home: GET /control/stats e /control/status, com autenticação básica.
// As séries por hora vêm do próprio AdGuard (período configurado nele, 24 h por padrão).
type adguard struct{}

func init() { register(adguard{}) }

func (adguard) Info() KindInfo {
	return KindInfo{
		ID: "adguard", Name: "AdGuard Home", Desc: "Consultas DNS, bloqueios e quem mais usa a rede",
		Fields: []Field{
			{ID: "url", Label: "Endereço", Type: "url", Placeholder: "http://192.168.0.10:8104", Required: true},
			{ID: "user", Label: "Usuário", Type: "text", Help: "O mesmo usuário do painel do AdGuard"},
			{ID: "secret", Label: "Senha", Type: "secret"},
		},
		Interval: 60,
		Metrics: []MetricDef{
			{ID: "queries", Name: "Consultas DNS", Kind: Num, Icon: "globe", History: "queries_series"},
			{ID: "blocked", Name: "Bloqueadas", Kind: Num, Icon: "shield", History: "blocked_series"},
			{ID: "blocked_pct", Name: "% bloqueado", Kind: Num, Unit: "%", Icon: "shield"},
			{ID: "avg_time", Name: "Tempo médio de resposta", Kind: Num, Unit: "ms", Icon: "clock",
				Ranges: []Range{{fp(50), "ok"}, {fp(200), "aviso"}, {nil, "critico"}}},
			{ID: "protection", Name: "Proteção", Kind: Text, Icon: "shield"},
			{ID: "protection_on", Name: "Proteção ligada (1 = sim)", Kind: Num, Icon: "shield"},
			{ID: "pause_left", Name: "Pausa: segundos até voltar", Kind: Num, Unit: "s", Icon: "clock"},
			{ID: "version", Name: "Versão", Kind: Text, Icon: "info"},
			{ID: "top_domains", Name: "Domínios mais consultados", Kind: List, Icon: "globe"},
			{ID: "top_blocked", Name: "Domínios mais bloqueados", Kind: List, Icon: "shield"},
			{ID: "top_clients", Name: "Aparelhos que mais consultam", Kind: List, Icon: "monitor"},
			{ID: "queries_series", Name: "Consultas por hora", Kind: Series, Icon: "chart"},
			{ID: "blocked_series", Name: "Bloqueios por hora", Kind: Series, Icon: "chart"},
		},
		Alerts: []AlertDef{
			// "_guard" (interna): 0 só quando desligada sem prazo para voltar; pausar por um tempo não avisa
			{ID: "protection_off", Name: "Proteção desligada", Desc: "Avisa quando a proteção do AdGuard fica desligada sem prazo para voltar (pausas com tempo não avisam)",
				Metric: "_guard", Op: "<", Default: 1, Level: "aviso", Fixed: true},
			{ID: "down", Name: "AdGuard sem responder", Desc: "Avisa quando o AdGuard para de responder por 5 minutos",
				Op: "down", Level: "aviso", Fixed: true},
		},
		Runtipi: []string{"adguard", "adguard-home", "adguardhome"},
		Actions: []ActionDef{{ID: "pause", Name: "Pausar a proteção"}, {ID: "resume", Name: "Ligar a proteção"}},
		Live:    true,
	}
}

// Action pausa (por Minutes; 0 = até ligar de novo) ou liga a proteção. POST /control/protection existe
// desde a v0.107.27 (com "duration" em ms); nas mais antigas, só liga/desliga por /control/dns_config.
func (adguard) Action(ctx context.Context, c *http.Client, cfg Config, req ActionReq) (string, error) {
	base := strings.TrimRight(cfg.URL, "/")
	body := map[string]any{"enabled": req.Action == "resume"}
	what := "ligar a proteção"
	if req.Action == "pause" {
		what = "pausar até ligar de novo"
		if req.Minutes > 0 {
			body["duration"] = int64(req.Minutes) * 60_000
			what = fmt.Sprintf("pausar %d min", req.Minutes)
		}
	}
	code, err := adgPost(ctx, c, cfg, base+"/control/protection", body)
	if code == http.StatusNotFound || code == http.StatusMethodNotAllowed {
		if req.Action == "pause" && req.Minutes > 0 {
			return "", ferr("este AdGuard é antigo e não pausa por tempo; atualize o AdGuard ou use “Até eu ligar”")
		}
		_, err = adgPost(ctx, c, cfg, base+"/control/dns_config", map[string]any{"protection_enabled": body["enabled"]})
	}
	if err != nil {
		return "", err
	}
	return what, nil
}

// adgPost manda um POST com JSON e ignora a resposta (o AdGuard responde só "OK" ou nada).
func adgPost(ctx context.Context, c *http.Client, cfg Config, u string, body any) (int, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return 0, ferr("endereço inválido")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "BrunOS")
	req.SetBasicAuth(cfg.User, cfg.Secret)
	resp, err := c.Do(req)
	if err != nil {
		return 0, ferr("não consegui falar com o AdGuard (confira o endereço e se ele está ligado)")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return resp.StatusCode, ferr("o AdGuard recusou o acesso (confira usuário e senha)")
	case resp.StatusCode >= 300:
		return resp.StatusCode, ferr("o AdGuard respondeu com erro %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

type adgStats struct {
	TimeUnits     string               `json:"time_units"`
	Queries       float64              `json:"num_dns_queries"`
	Blocked       float64              `json:"num_blocked_filtering"`
	SafeBrowsing  float64              `json:"num_replaced_safebrowsing"`
	Parental      float64              `json:"num_replaced_parental"`
	AvgTime       float64              `json:"avg_processing_time"` // segundos
	TopDomains    []map[string]float64 `json:"top_queried_domains"`
	TopBlocked    []map[string]float64 `json:"top_blocked_domains"`
	TopClients    []map[string]float64 `json:"top_clients"`
	DNSQueries    []float64            `json:"dns_queries"`
	BlockedSeries []float64            `json:"blocked_filtering"`
}

type adgStatus struct {
	Protection bool    `json:"protection_enabled"`
	Version    string  `json:"version"`
	Running    bool    `json:"running"`
	PauseLeft  float64 `json:"protection_disabled_duration"` // ms até voltar sozinha (0 = sem prazo)
}

func (adguard) Fetch(ctx context.Context, c *http.Client, cfg Config) (map[string]Value, error) {
	base := strings.TrimRight(cfg.URL, "/")
	var st adgStats
	if _, err := getJSON(ctx, c, base+"/control/stats", nil, cfg.User, cfg.Secret, &st); err != nil {
		return nil, err
	}
	var status adgStatus
	if _, err := getJSON(ctx, c, base+"/control/status", nil, cfg.User, cfg.Secret, &status); err != nil {
		return nil, err
	}
	blocked := st.Blocked + st.SafeBrowsing + st.Parental
	out := map[string]Value{
		"queries":     N(st.Queries),
		"blocked":     N(blocked),
		"avg_time":    N(round1(st.AvgTime * 1000)),
		"version":     T(status.Version),
		"top_domains": adgTop(st.TopDomains),
		"top_blocked": adgTop(st.TopBlocked),
		"top_clients": adgTop(st.TopClients),
	}
	if st.Queries > 0 {
		out["blocked_pct"] = N(round1(blocked / st.Queries * 100))
	} else {
		out["blocked_pct"] = N(0)
	}
	if status.Protection {
		out["protection"], out["protection_on"], out["_guard"] = T("Ligada"), N(1), N(1)
	} else {
		out["protection"], out["protection_on"], out["_guard"] = T("Desligada"), N(0), N(0)
		if status.PauseLeft > 0 {
			out["protection"], out["_guard"] = T("Pausada"), N(1)
			out["pause_left"] = N(float64(int64(status.PauseLeft / 1000)))
		}
	}
	step := int64(3600)
	if st.TimeUnits == "days" {
		step = 86400
	}
	out["queries_series"] = Value{Series: adgSeries(st.DNSQueries, step)}
	out["blocked_series"] = Value{Series: adgSeries(st.BlockedSeries, step)}
	return out, nil
}

// adgSeries: o último ponto é a hora (ou dia) atual.
func adgSeries(v []float64, step int64) *SeriesData {
	now := nowFunc().Unix()
	last := now - now%step
	if step == 86400 {
		t := nowFunc()
		last = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).Unix()
	}
	pts := append([]float64(nil), v...)
	return &SeriesData{Step: step, Start: last - int64(len(pts)-1)*step, Points: pts}
}

// adgTop transforma [{"dominio": 12}, ...] em itens (no máximo 10).
func adgTop(in []map[string]float64) Value {
	items := []Item{}
	for _, m := range in {
		for k, v := range m {
			items = append(items, Item{Label: k, Num: fp(v)})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return *items[i].Num > *items[j].Num })
	if len(items) > 10 {
		items = items[:10]
	}
	return Value{List: items}
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
