package integrations

import (
	"context"
	"net/http"
	"sort"
	"strings"
)

// WatchYourLAN (v2): GET /api/all devolve os aparelhos vistos na rede. Os campos não têm tag JSON no
// código dele, então vêm com os nomes do Go: ID, Name, DNS, Iface, IP, Mac, Hw, Date, Known, Now.
// O aviso de aparelho novo usa o "conhecido" do próprio WatchYourLAN (escolha do Bruno): um aviso por
// MAC desconhecido; marcar como conhecido lá fecha o aviso. O BrunOS não guarda lista nenhuma.
type watchyourlan struct{}

func init() { register(watchyourlan{}) }

func (watchyourlan) Info() KindInfo {
	return KindInfo{
		ID: "watchyourlan", Name: "WatchYourLAN", Desc: "Quem está na rede de casa e aviso de aparelho desconhecido",
		Fields: []Field{
			{ID: "url", Label: "Endereço", Type: "url", Placeholder: "http://192.168.0.10:8840", Required: true},
		},
		Interval: 120,
		Metrics: []MetricDef{
			{ID: "online", Name: "Aparelhos online", Kind: Num, Icon: "wifi"},
			{ID: "offline", Name: "Aparelhos desligados", Kind: Num, Icon: "wifi"},
			{ID: "total", Name: "Aparelhos no total", Kind: Num, Icon: "network"},
			{ID: "known", Name: "Aparelhos conhecidos", Kind: Num, Icon: "check"},
			{ID: "unknown", Name: "Aparelhos desconhecidos", Kind: Num, Icon: "alert"},
			{ID: "online_list", Name: "Lista: online agora", Kind: List, Icon: "wifi"},
			{ID: "unknown_list", Name: "Lista: desconhecidos", Kind: List, Icon: "alert"},
		},
		Alerts: []AlertDef{
			{ID: "new_device", Name: "Aparelho novo na rede", Desc: "Avisa quando aparece um aparelho que o WatchYourLAN marca como desconhecido",
				Metric: "unknown_list", Op: "new", Level: "aviso", Fixed: true,
				Hint: "Se for seu, marque como conhecido no WatchYourLAN e o aviso não volta."},
			{ID: "down", Name: "WatchYourLAN sem responder", Desc: "Avisa quando o WatchYourLAN para de responder por 5 minutos", Op: "down", Level: "info", Fixed: true},
		},
		Runtipi: []string{"watchyourlan", "watch-your-lan"},
	}
}

type wylHost struct {
	ID    int    `json:"ID"`
	Name  string `json:"Name"`
	DNS   string `json:"DNS"`
	IP    string `json:"IP"`
	Mac   string `json:"Mac"`
	Hw    string `json:"Hw"`
	Known int    `json:"Known"`
	Now   int    `json:"Now"`
}

func (h wylHost) label() string {
	for _, s := range []string{h.Name, strings.TrimSuffix(h.DNS, "."), h.Hw, h.IP} {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return h.Mac
}

func (h wylHost) detail() string {
	parts := []string{}
	for _, s := range []string{h.IP, h.Mac, h.Hw} {
		if s = strings.TrimSpace(s); s != "" && s != h.label() {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

func (watchyourlan) Fetch(ctx context.Context, c *http.Client, cfg Config) (map[string]Value, error) {
	var hosts []wylHost
	if _, err := getJSON(ctx, c, strings.TrimRight(cfg.URL, "/")+"/api/all", nil, "", "", &hosts); err != nil {
		return nil, err
	}
	sort.SliceStable(hosts, func(i, j int) bool { return strings.ToLower(hosts[i].label()) < strings.ToLower(hosts[j].label()) })
	online, unknown := []Item{}, []Item{}
	var on, known float64
	for _, h := range hosts {
		if h.Now == 1 {
			on++
			online = append(online, Item{Label: h.label(), Text: h.detail(), ID: h.Mac})
		}
		if h.Known == 1 {
			known++
		} else {
			unknown = append(unknown, Item{Label: h.label(), Text: h.detail(), ID: strings.ToLower(h.Mac)})
		}
	}
	total := float64(len(hosts))
	return map[string]Value{
		"online": N(on), "offline": N(total - on), "total": N(total), "known": N(known), "unknown": N(total - known),
		"online_list": {List: online}, "unknown_list": {List: unknown},
	}, nil
}
