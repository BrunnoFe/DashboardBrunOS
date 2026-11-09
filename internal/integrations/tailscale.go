package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Tailscale: sem token da nuvem nem socket do tailscaled no container (decisão 34). O BrunOS pede
// "tailscale" ao script brunos-host do servidor, pela mesma conexão SSH restrita do desligamento
// seguro, e o script roda "tailscale status --json" (só leitura).
type tailscale struct{}

func init() { register(tailscale{}) }

func (tailscale) Info() KindInfo {
	return KindInfo{
		ID: "tailscale", Name: "Tailscale", Desc: "Aparelhos da sua rede Tailscale: quem está online e o IP deste servidor",
		NoURL: true, Interval: 60,
		Note: "Usa a mesma conexão SSH do desligamento seguro (Configurações → Energia) e precisa do script do servidor na versão 3: copie os comandos de atualização abaixo e cole no servidor uma vez.",
		Metrics: []MetricDef{
			{ID: "state", Name: "Situação", Kind: Text, Icon: "shield"},
			{ID: "state_ok", Name: "Conectado (1 = sim)", Kind: Num, Icon: "shield"},
			{ID: "online", Name: "Aparelhos online", Kind: Num, Icon: "network"},
			{ID: "total", Name: "Aparelhos na rede", Kind: Num, Icon: "network"},
			{ID: "peers", Name: "Lista de aparelhos", Kind: List, Icon: "network"},
			{ID: "self_ip", Name: "IP deste servidor", Kind: Text, Icon: "server"},
			{ID: "exit_node", Name: "Saída para a internet", Kind: Text, Icon: "globe"},
			{ID: "health", Name: "Avisos do Tailscale", Kind: Text, Icon: "alert"},
		},
		Alerts: []AlertDef{
			{ID: "disconnected", Name: "Tailscale desconectado", Desc: "Avisa quando o Tailscale deste servidor sai do ar",
				Metric: "state_ok", Op: "<", Default: 1, Level: "aviso", Fixed: true},
			{ID: "down", Name: "Tailscale sem leitura", Desc: "Avisa quando o BrunOS não consegue ler o Tailscale por 5 minutos", Op: "down", Level: "info", Fixed: true},
		},
		Runtipi: []string{"tailscale"},
	}
}

type tsPeer struct {
	HostName     string    `json:"HostName"`
	DNSName      string    `json:"DNSName"`
	OS           string    `json:"OS"`
	TailscaleIPs []string  `json:"TailscaleIPs"`
	Online       bool      `json:"Online"`
	LastSeen     time.Time `json:"LastSeen"`
	ExitNode     bool      `json:"ExitNode"`
	PublicKey    string    `json:"PublicKey"`
}

func (p tsPeer) name() string {
	if n := strings.SplitN(strings.TrimSuffix(p.DNSName, "."), ".", 2)[0]; n != "" {
		return n
	}
	return p.HostName
}

var tsStates = map[string]string{"Running": "Conectado", "Stopped": "Desligado", "NeedsLogin": "Precisa entrar na conta",
	"NeedsMachineAuth": "Esperando aprovação", "Starting": "Conectando", "NoState": "Sem estado"}

// agoBR: "há 5 min", "há 3 h", "há 2 dias".
func agoBR(t, now time.Time) string {
	if t.IsZero() || t.Year() < 2000 {
		return "nunca visto"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "agora"
	case d < time.Hour:
		return fmt.Sprintf("há %d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("há %d h", int(d.Hours()))
	default:
		return fmt.Sprintf("há %d dias", int(d.Hours()/24))
	}
}

func (tailscale) Fetch(ctx context.Context, _ *http.Client, _ Config) (map[string]Value, error) {
	if HostQuery == nil {
		return nil, ferr("leitura do servidor indisponível")
	}
	raw, err := HostQuery(ctx, "tailscale")
	if err != nil {
		return nil, ferr("%s", err.Error())
	}
	var st struct {
		BackendState string             `json:"BackendState"`
		Self         *tsPeer            `json:"Self"`
		Peer         map[string]*tsPeer `json:"Peer"`
		Health       []string           `json:"Health"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, ferr("resposta do Tailscale em formato inesperado")
	}
	now := nowFunc()
	peers := make([]*tsPeer, 0, len(st.Peer))
	for _, p := range st.Peer {
		if p != nil {
			peers = append(peers, p)
		}
	}
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Online != peers[j].Online {
			return peers[i].Online
		}
		return strings.ToLower(peers[i].name()) < strings.ToLower(peers[j].name())
	})
	items := []Item{}
	var on float64
	exit := "direto (sem saída por outro aparelho)"
	for _, p := range peers {
		ip := ""
		if len(p.TailscaleIPs) > 0 {
			ip = p.TailscaleIPs[0]
		}
		status := "online"
		if p.Online {
			on++
		} else {
			status = agoBR(p.LastSeen, now)
		}
		if p.ExitNode {
			exit = p.name()
		}
		items = append(items, Item{Label: p.name(), Text: strings.Trim(strings.Join([]string{status, ip, p.OS}, " · "), " ·"), ID: p.PublicKey})
	}
	state := tsStates[st.BackendState]
	if state == "" {
		state = st.BackendState
	}
	ok := 0.0
	if st.BackendState == "Running" {
		ok = 1
	}
	out := map[string]Value{
		"state": T(state), "state_ok": N(ok), "online": N(on), "total": N(float64(len(peers))), "peers": {List: items},
		"exit_node": T(exit), "health": T("nenhum"),
	}
	if len(st.Health) > 0 {
		out["health"] = T(strings.Join(st.Health, " · "))
	}
	if st.Self != nil && len(st.Self.TailscaleIPs) > 0 {
		out["self_ip"] = T(st.Self.TailscaleIPs[0])
	}
	return out, nil
}
