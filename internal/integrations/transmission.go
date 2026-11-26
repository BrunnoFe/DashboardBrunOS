package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
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
			// 0.35.0: lista completa para o Formato Torrents (cada item com tamanho, velocidades, razão...)
			{ID: "torrents", Name: "Torrents (todos, com detalhes)", Kind: Torrents, Icon: "download"},
			{ID: "ratio", Name: "Razão geral (enviado ÷ baixado)", Kind: Num, Icon: "activity"},
			{ID: "turtle", Name: "Modo tartaruga (1 = ligado)", Kind: Num, Icon: "clock"},
		},
		Actions: []ActionDef{{ID: "resume_all", Name: "Retomar todos"}, {ID: "pause_all", Name: "Pausar todos"},
			{ID: "turtle_on", Name: "Ligar o modo tartaruga"}, {ID: "turtle_off", Name: "Desligar o modo tartaruga"},
			{ID: "pause", Name: "Pausar um torrent"}, {ID: "resume", Name: "Retomar um torrent"}},
		LiveEvery: 5,
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
			ID      int     `json:"id"`
			Name    string  `json:"name"`
			Percent float64 `json:"percentDone"`
			Rate    float64 `json:"rateDownload"`
			RateUp  float64 `json:"rateUpload"`
			Status  int     `json:"status"`
			ETA     float64 `json:"eta"`
			Error   int     `json:"error"`
			Size    float64 `json:"sizeWhenDone"`
			Ratio   float64 `json:"uploadRatio"`
			Up      float64 `json:"uploadedEver"`
			Down    float64 `json:"downloadedEver"`
			Peers   float64 `json:"peersConnected"`
			Added   float64 `json:"addedDate"`
		} `json:"torrents"`
	}
	fields := []string{"id", "name", "percentDone", "rateDownload", "rateUpload", "status", "eta", "error", "sizeWhenDone", "uploadRatio", "uploadedEver", "downloadedEver", "peersConnected", "addedDate"}
	if err := trCall(ctx, c, cfg, &sid, "torrent-get", map[string]any{"fields": fields}, &tg); err != nil {
		return nil, err
	}
	var sess struct {
		Alt bool `json:"alt-speed-enabled"`
	}
	turtle := Value{} // versão muito antiga sem session-get: a métrica fica vazia
	if err := trCall(ctx, c, cfg, &sid, "session-get", map[string]any{"fields": []string{"alt-speed-enabled"}}, &sess); err == nil {
		turtle = N(map[bool]float64{true: 1, false: 0}[sess.Alt])
	}
	var dl, seed, errs, upAll, downAll float64
	type row struct {
		pct  float64
		item Item
	}
	rows, all := []row{}, []trRow{}
	for _, t := range tg.Torrents {
		if t.Error != 0 {
			errs++
		}
		upAll += t.Up
		downAll += t.Down
		all = append(all, trRow{t.ID, t.Name, t.Status, t.Error != 0, Item{
			Label: t.Name, Num: fp(round1(t.Percent * 100)), Text: trState(t.Status, t.Error != 0), ID: strconv.Itoa(t.ID),
			// 0.38.0: também o total enviado/baixado, pares conectados e quando foi adicionado (Colunas do widget)
			X: map[string]float64{"size": t.Size, "down": t.Rate, "up": t.RateUp, "ratio": round2(t.Ratio), "eta": t.ETA,
				"uploaded": t.Up, "downloaded": t.Down, "peers": t.Peers, "added": t.Added},
		}})
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
	ratio := Value{}
	if downAll > 0 {
		ratio = N(round2(upAll / downAll))
	}
	return map[string]Value{
		"_sid": T(sid), "down": N(st.Down), "up": N(st.Up), "active": N(st.Active), "paused": N(st.Paused),
		"total": N(st.Total), "downloading": N(dl), "seeding": N(seed), "errors": N(errs),
		"downloaded": N(st.Cumulate.Down), "uploaded": N(st.Cumulate.Up), "downloading_list": {List: list},
		"torrents": {List: trOrder(all)}, "ratio": ratio, "turtle": turtle,
	}, nil
}

// trRow guarda o que a ordenação precisa ao lado do item.
type trRow struct {
	id     int
	name   string
	status int
	err    bool
	item   Item
}

// trOrder: com erro e baixando primeiro, depois na fila, semeando e parados; nome em ordem dentro de cada
// grupo. No máximo 100 (o widget mostra os de cima; acima disso o Transmission já é o lugar certo).
func trOrder(all []trRow) []Item {
	rank := func(r trRow) int {
		switch {
		case r.err:
			return 0
		case r.status == 4:
			return 1
		case r.status == 3 || r.status == 1 || r.status == 2:
			return 2
		case r.status == 6 || r.status == 5:
			return 3
		}
		return 4
	}
	sort.SliceStable(all, func(i, j int) bool {
		if a, b := rank(all[i]), rank(all[j]); a != b {
			return a < b
		}
		return strings.ToLower(all[i].name) < strings.ToLower(all[j].name)
	})
	out := make([]Item, 0, min(len(all), 100))
	for i, r := range all {
		if i >= 100 {
			break
		}
		out = append(out, r.item)
	}
	return out
}

// trState: o estado em pt-BR (status do RPC: 0 parado, 1/2 verificando, 3 na fila, 4 baixando, 5 na fila
// para semear, 6 semeando).
func trState(status int, err bool) string {
	if err {
		return "Erro"
	}
	switch status {
	case 0:
		return "Pausado"
	case 1, 2:
		return "Verificando"
	case 3, 5:
		return "Na fila"
	case 4:
		return "Baixando"
	case 6:
		return "Semeando"
	}
	return "—"
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

// Action: pausar/retomar todos (torrent-stop/torrent-start sem ids = todos), um torrent só (0.38.0, pelo
// id em Target) e o modo tartaruga (limite de velocidade alternativo do próprio Transmission). Nada apaga
// torrent (decisão 73).
func (transmission) Action(ctx context.Context, c *http.Client, cfg Config, req ActionReq) (string, error) {
	sid := ""
	var out struct{}
	var err error
	switch req.Action {
	case "pause", "resume":
		id, e := strconv.Atoi(req.Target)
		if e != nil || id <= 0 {
			return "", &ErrInput{"torrent inválido"}
		}
		method := map[string]string{"pause": "torrent-stop", "resume": "torrent-start"}[req.Action]
		if err = trCall(ctx, c, cfg, &sid, method, map[string]any{"ids": []int{id}}, &out); err != nil {
			return "", err
		}
		return map[string]string{"pause": "pausar o torrent ", "resume": "retomar o torrent "}[req.Action] + strconv.Itoa(id), nil
	case "pause_all":
		err = trCall(ctx, c, cfg, &sid, "torrent-stop", map[string]any{}, &out)
	case "resume_all":
		err = trCall(ctx, c, cfg, &sid, "torrent-start", map[string]any{}, &out)
	case "turtle_on", "turtle_off":
		err = trCall(ctx, c, cfg, &sid, "session-set", map[string]any{"alt-speed-enabled": req.Action == "turtle_on"}, &out)
	}
	if err != nil {
		return "", err
	}
	return map[string]string{"pause_all": "pausar todos", "resume_all": "retomar todos", "turtle_on": "ligar a tartaruga", "turtle_off": "desligar a tartaruga"}[req.Action], nil
}
