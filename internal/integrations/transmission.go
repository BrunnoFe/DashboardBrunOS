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
)

// Transmission (RPC): POST /transmission/rpc com JSON. A primeira chamada recebe 409 e o cabeçalho
// X-Transmission-Session-Id, que vai em todas as seguintes (proteção contra CSRF do próprio app).
// Usuário e senha são opcionais (só se o Transmission pedir). Só lê: session-stats e torrent-get.
type transmission struct{}

func init() { register(transmission{}) }

func (transmission) Info() KindInfo {
	return KindInfo{
		ID: "transmission", Name: "Transmission", Desc: "Velocidade, torrents ativos e o que está baixando agora",
		Fields: []Field{
			{ID: "url", Label: "Endereço", Type: "url", Placeholder: "http://192.168.0.10:9091", Required: true},
			{ID: "user", Label: "Usuário (se tiver)", Type: "text"},
			{ID: "secret", Label: "Senha (se tiver)", Type: "secret"},
		},
		Interval: 30,
		Metrics: []MetricDef{
			{ID: "down", Name: "Download agora", Kind: Num, Unit: "B/s", Icon: "download"},
			{ID: "up", Name: "Upload agora", Kind: Num, Unit: "B/s", Icon: "upload"},
			{ID: "downloading", Name: "Baixando", Kind: Num, Icon: "download"},
			{ID: "seeding", Name: "Semeando", Kind: Num, Icon: "upload"},
			{ID: "active", Name: "Ativos", Kind: Num, Icon: "activity"},
			{ID: "paused", Name: "Pausados", Kind: Num, Icon: "clock"},
			{ID: "total", Name: "Torrents no total", Kind: Num, Icon: "box"},
			{ID: "errors", Name: "Com erro", Kind: Num, Icon: "alert"},
			{ID: "downloaded", Name: "Baixado no total", Kind: Num, Unit: "B", Icon: "download"},
			{ID: "uploaded", Name: "Enviado no total", Kind: Num, Unit: "B", Icon: "upload"},
			{ID: "downloading_list", Name: "Lista: baixando agora", Kind: List, Icon: "download"},
		},
		Alerts: []AlertDef{
			{ID: "errors", Name: "Torrent com erro", Desc: "Avisa quando algum torrent fica com erro", Metric: "errors", Op: ">", Default: 0, Level: "info", Fixed: true},
			{ID: "down", Name: "Transmission sem responder", Desc: "Avisa quando o Transmission para de responder por 5 minutos", Op: "down", Level: "info", Fixed: true},
		},
		Runtipi: []string{"transmission"},
	}
}

// trRPC aceita o endereço com ou sem o caminho do RPC.
func trRPC(base string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/rpc") {
		return base
	}
	return base + "/transmission/rpc"
}

// trCall faz uma chamada RPC; sid é o cabeçalho de sessão (atualizado se o app pedir outro).
func trCall(ctx context.Context, c *http.Client, cfg Config, sid *string, method string, args any, v any) error {
	body, _ := json.Marshal(map[string]any{"method": method, "arguments": args})
	for try := 0; try < 2; try++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, trRPC(cfg.URL), bytes.NewReader(body))
		if err != nil {
			return ferr("endereço inválido")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "BrunOS")
		req.Header.Set("X-Transmission-Session-Id", *sid)
		if cfg.User != "" || cfg.Secret != "" {
			req.SetBasicAuth(cfg.User, cfg.Secret)
		}
		resp, err := c.Do(req)
		if err != nil {
			return ferr("não consegui falar com o app (confira o endereço e se ele está ligado)")
		}
		if resp.StatusCode == http.StatusConflict { // sessão nova: pega o id e tenta de novo
			*sid = resp.Header.Get("X-Transmission-Session-Id")
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
			resp.Body.Close()
			continue
		}
		defer resp.Body.Close()
		switch {
		case resp.StatusCode == 401 || resp.StatusCode == 403:
			return ferr("o Transmission recusou o acesso (confira usuário e senha)")
		case resp.StatusCode >= 300:
			return ferr("o Transmission respondeu com erro %d", resp.StatusCode)
		}
		var out struct {
			Result    string          `json:"result"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out); err != nil {
			return ferr("resposta do Transmission em formato inesperado")
		}
		if out.Result != "success" {
			return ferr("o Transmission respondeu: %s", out.Result)
		}
		if err := json.Unmarshal(out.Arguments, v); err != nil {
			return ferr("resposta do Transmission em formato inesperado")
		}
		return nil
	}
	return ferr("o Transmission não aceitou a sessão")
}

// fmtETA: "falta 12 min", "falta 3 h".
func fmtETA(sec float64) string {
	switch {
	case sec < 0:
		return ""
	case sec < 3600:
		return fmt.Sprintf("falta %d min", int(sec/60)+1)
	case sec < 48*3600:
		return fmt.Sprintf("falta %d h", int(sec/3600))
	default:
		return fmt.Sprintf("falta %d dias", int(sec/86400))
	}
}

// fmtRateBR: "2,1 MB/s".
func fmtRateBR(b float64) string {
	units := []string{"B/s", "KB/s", "MB/s", "GB/s", "TB/s"}
	i := 0
	for b >= 1000 && i < len(units)-1 {
		b /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", b, units[i])
	}
	return strings.Replace(fmt.Sprintf("%.1f %s", b, units[i]), ".", ",", 1)
}

func (transmission) Fetch(ctx context.Context, c *http.Client, cfg Config) (map[string]Value, error) {
	sid := ""
	if p, ok := cfg.Prev["_sid"]; ok && p.Text != nil {
		sid = *p.Text
	}
	var st struct {
		Active   float64 `json:"activeTorrentCount"`
		Paused   float64 `json:"pausedTorrentCount"`
		Total    float64 `json:"torrentCount"`
		Down     float64 `json:"downloadSpeed"`
		Up       float64 `json:"uploadSpeed"`
		Cumulate struct {
			Down float64 `json:"downloadedBytes"`
			Up   float64 `json:"uploadedBytes"`
		} `json:"cumulative-stats"`
	}
	if err := trCall(ctx, c, cfg, &sid, "session-stats", map[string]any{}, &st); err != nil {
		return nil, err
	}
	var tg struct {
		Torrents []struct {
			Name    string  `json:"name"`
			Percent float64 `json:"percentDone"`
			Rate    float64 `json:"rateDownload"`
			Status  int     `json:"status"`
			ETA     float64 `json:"eta"`
			Error   int     `json:"error"`
		} `json:"torrents"`
	}
	fields := []string{"name", "percentDone", "rateDownload", "status", "eta", "error"}
	if err := trCall(ctx, c, cfg, &sid, "torrent-get", map[string]any{"fields": fields}, &tg); err != nil {
		return nil, err
	}
	var dl, seed, errs float64
	type row struct {
		pct  float64
		item Item
	}
	rows := []row{}
	for _, t := range tg.Torrents {
		if t.Error != 0 {
			errs++
		}
		switch t.Status {
		case 4: // baixando
			dl++
			pct := round1(t.Percent * 100)
			rows = append(rows, row{pct, Item{Label: t.Name,
				Text: strings.Trim(strings.Replace(fmt.Sprintf("%.0f%%", pct), ".", ",", 1)+" · "+fmtRateBR(t.Rate)+" · "+fmtETA(t.ETA), " ·")}})
		case 6: // semeando
			seed++
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].pct > rows[j].pct })
	list := []Item{}
	for i, r := range rows {
		if i >= 20 {
			break
		}
		list = append(list, r.item)
	}
	return map[string]Value{
		"_sid": T(sid), "down": N(st.Down), "up": N(st.Up), "active": N(st.Active), "paused": N(st.Paused),
		"total": N(st.Total), "downloading": N(dl), "seeding": N(seed), "errors": N(errs),
		"downloaded": N(st.Cumulate.Down), "uploaded": N(st.Cumulate.Up), "downloading_list": {List: list},
	}, nil
}
