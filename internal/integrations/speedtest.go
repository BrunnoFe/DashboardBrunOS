package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

// Speedtest Tracker (API v1): GET /api/v1/results/latest e /api/v1/results, token Bearer com a
// permissão "Read results" (criado em /admin/api-tokens). download_bits/upload_bits em bits/s,
// ping em ms. A lista dos últimos 7 dias só é baixada de novo quando aparece um teste novo.
type speedtest struct{}

func init() { register(speedtest{}) }

func (speedtest) Info() KindInfo {
	return KindInfo{
		ID: "speedtest", Name: "Speedtest Tracker", Desc: "Velocidade da internet medida pelos testes agendados",
		Fields: []Field{
			{ID: "url", Label: "Endereço", Type: "url", Placeholder: "http://192.168.0.10:8765", Required: true},
			{ID: "secret", Label: "Token da API", Type: "secret", Required: true,
				Help: "No Speedtest Tracker: menu do usuário → API Tokens → criar com a permissão “Read results”"},
		},
		Interval: 600,
		Metrics: []MetricDef{
			{ID: "download", Name: "Download", Kind: Num, Unit: "Mb/s", Icon: "download", History: "download_series"},
			{ID: "upload", Name: "Upload", Kind: Num, Unit: "Mb/s", Icon: "upload", History: "upload_series"},
			{ID: "ping", Name: "Ping", Kind: Num, Unit: "ms", Icon: "clock", History: "ping_series",
				Ranges: []Range{{fp(40), "ok"}, {fp(100), "aviso"}, {nil, "critico"}}},
			{ID: "jitter", Name: "Variação do ping (jitter)", Kind: Num, Unit: "ms", Icon: "clock"},
			{ID: "last_test", Name: "Último teste", Kind: Num, Unit: "ts", Icon: "clock"},
			{ID: "isp", Name: "Provedor", Kind: Text, Icon: "globe"},
			{ID: "server", Name: "Servidor do teste", Kind: Text, Icon: "server"},
			{ID: "download_series", Name: "Download (7 dias)", Kind: Series, Unit: "Mb/s", Icon: "chart"},
			{ID: "upload_series", Name: "Upload (7 dias)", Kind: Series, Unit: "Mb/s", Icon: "chart"},
			{ID: "ping_series", Name: "Ping (7 dias)", Kind: Series, Unit: "ms", Icon: "chart"},
		},
		Alerts: []AlertDef{
			{ID: "slow_download", Name: "Download baixo", Desc: "Avisa quando o último teste der menos que isto",
				Metric: "download", Op: "<", Default: 100, Unit: "Mb/s", Level: "aviso"},
			{ID: "high_ping", Name: "Ping alto", Desc: "Avisa quando o último teste der mais que isto",
				Metric: "ping", Op: ">", Default: 100, Unit: "ms", Level: "info"},
			{ID: "down", Name: "Speedtest sem responder", Desc: "Avisa quando o Speedtest Tracker para de responder",
				Op: "down", Level: "info", Fixed: true},
		},
		Runtipi: []string{"speedtest-tracker", "speedtest_tracker", "speedtesttracker"},
	}
}

type stResult struct {
	ID           json.Number `json:"id"`
	Ping         float64     `json:"ping"`
	Download     float64     `json:"download"` // bytes/s
	Upload       float64     `json:"upload"`
	DownloadBits float64     `json:"download_bits"`
	UploadBits   float64     `json:"upload_bits"`
	Status       string      `json:"status"`
	CreatedAt    string      `json:"created_at"`
	Data         struct {
		ISP  string `json:"isp"`
		Ping struct {
			Jitter float64 `json:"jitter"`
		} `json:"ping"`
		Server struct {
			Name     string `json:"name"`
			Location string `json:"location"`
		} `json:"server"`
	} `json:"data"`
}

func (r stResult) mbps(bits, bytes float64) float64 {
	if bits <= 0 {
		bits = bytes * 8
	}
	return round1(bits / 1e6)
}

func (r stResult) completed() bool {
	s := strings.ToLower(r.Status)
	return s == "" || s == "completed"
}

var stLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05.000000Z", "2006-01-02 15:04:05", "2006-01-02T15:04:05"}

func (r stResult) when() (time.Time, bool) {
	for _, l := range stLayouts {
		if t, err := time.Parse(l, r.CreatedAt); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// stDecode aceita o resultado "puro" ou dentro de {"data": {...}}.
func stDecode(raw json.RawMessage) (stResult, error) {
	var wrap struct {
		Data json.RawMessage `json:"data"`
	}
	var r stResult
	if err := json.Unmarshal(raw, &wrap); err == nil && len(wrap.Data) > 0 && wrap.Data[0] == '{' {
		var probe struct {
			ID json.Number `json:"id"`
		}
		if json.Unmarshal(wrap.Data, &probe) == nil && probe.ID != "" {
			raw = wrap.Data
		}
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	return r, nil
}

func (speedtest) Fetch(ctx context.Context, c *http.Client, cfg Config) (map[string]Value, error) {
	base := strings.TrimRight(cfg.URL, "/")
	hdr := map[string]string{"Authorization": "Bearer " + cfg.Secret}
	var raw json.RawMessage
	code, err := getJSON(ctx, c, base+"/api/v1/results/latest", hdr, "", "", &raw)
	if code == http.StatusNotFound {
		return map[string]Value{"_id": T("")}, nil // ainda sem nenhum teste
	}
	if err != nil {
		return nil, err
	}
	r, err := stDecode(raw)
	if err != nil {
		return nil, ferr("resposta do Speedtest Tracker em formato inesperado")
	}
	out := map[string]Value{
		"_id":      T(r.ID.String()),
		"download": N(r.mbps(r.DownloadBits, r.Download)),
		"upload":   N(r.mbps(r.UploadBits, r.Upload)),
		"ping":     N(round1(r.Ping)),
		"jitter":   N(round1(r.Data.Ping.Jitter)),
		"isp":      T(r.Data.ISP),
		"server":   T(strings.TrimSpace(strings.Trim(r.Data.Server.Name+" · "+r.Data.Server.Location, " ·"))),
	}
	if t, ok := r.when(); ok {
		out["last_test"] = N(float64(t.Unix()))
	}

	// séries: só baixa a lista quando aparece um teste novo
	if p, ok := cfg.Prev["_id"]; ok && p.Text != nil && *p.Text == r.ID.String() && cfg.Prev["download_series"].Series != nil {
		for _, k := range []string{"download_series", "upload_series", "ping_series"} {
			out[k] = cfg.Prev[k]
		}
		return out, nil
	}
	list, err := stList(ctx, c, base, hdr)
	if err != nil {
		return out, nil // os números do último teste já servem; as séries ficam para a próxima
	}
	down, up, ping := stSeries(list)
	out["download_series"], out["upload_series"], out["ping_series"] = Value{Series: down}, Value{Series: up}, Value{Series: ping}
	return out, nil
}

func stList(ctx context.Context, c *http.Client, base string, hdr map[string]string) ([]stResult, error) {
	since := nowFunc().Add(-7 * 24 * time.Hour).Format("2006-01-02")
	var body struct {
		Data []json.RawMessage `json:"data"`
	}
	url := fmt.Sprintf("%s/api/v1/results?sort=-created_at&per_page=200&filter[start_at]=%s", base, since)
	code, err := getJSON(ctx, c, url, hdr, "", "", &body)
	if code == http.StatusUnprocessableEntity { // versão que não aceita algum parâmetro
		_, err = getJSON(ctx, c, base+"/api/v1/results?sort=-created_at", hdr, "", "", &body)
	}
	if err != nil {
		return nil, err
	}
	out := make([]stResult, 0, len(body.Data))
	for _, raw := range body.Data {
		var r stResult
		if json.Unmarshal(raw, &r) == nil && r.completed() {
			out = append(out, r)
		}
	}
	return out, nil
}

// stSeries agrupa os testes por hora nos últimos 7 dias (168 pontos; hora sem teste = sem dado).
func stSeries(list []stResult) (down, up, ping *SeriesData) {
	const step, n = int64(3600), 168
	now := nowFunc().Unix()
	last := now - now%step
	start := last - (n-1)*step
	mk := func() *SeriesData {
		p := make([]float64, n)
		for i := range p {
			p[i] = math.NaN()
		}
		return &SeriesData{Step: step, Start: start, Points: p}
	}
	down, up, ping = mk(), mk(), mk()
	for _, r := range list {
		t, ok := r.when()
		if !ok {
			continue
		}
		i := (t.Unix() - start) / step
		if i < 0 || i >= n {
			continue
		}
		down.Points[i] = r.mbps(r.DownloadBits, r.Download)
		up.Points[i] = r.mbps(r.UploadBits, r.Upload)
		ping.Points[i] = round1(r.Ping)
	}
	return down, up, ping
}
