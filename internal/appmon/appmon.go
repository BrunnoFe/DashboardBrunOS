// Package appmon guarda, por app, um minuto a minuto das últimas 24 h: CPU, memória e se o app
// respondia (disponibilidade). Também decide quando avisar na central que um app caiu ou voltou.
//
// Fica só na memória (1440 pontos × poucos bytes por app): reiniciar o BrunOS recomeça o histórico.
package appmon

import (
	"sync"
	"time"

	"brunos/internal/events"
)

const (
	Slots     = 1440 // 24 h, um por minuto
	downAfter = 2    // minutos seguidos sem responder antes de avisar (evita alarme por um soluço)
)

// Estados de disponibilidade de um minuto.
const (
	Unknown int8 = -1 // parado de propósito, sem endereço ou sem medição
	Down    int8 = 0
	Up      int8 = 1
)

type Sample struct {
	CPU   float64 // %
	MemMB float64
	Up    int8
}

type Point struct {
	T   int64   `json:"t"`
	CPU float32 `json:"cpu"`
	Mem float32 `json:"mem"` // MB
	Up  int8    `json:"up"`
}

type ring struct {
	pts  [Slots]Point
	last int64 // minuto (Unix/60) do último ponto
	fail int   // minutos seguidos sem responder
}

type Monitor struct {
	Now func() time.Time

	mu   sync.Mutex
	apps map[string]*ring // chave: projeto do compose (<app>_<loja>)
}

func New() *Monitor { return &Monitor{Now: time.Now, apps: map[string]*ring{}} }

// Add grava a amostra do minuto atual de um app.
func (m *Monitor) Add(project string, s Sample) {
	min := m.Now().Unix() / 60
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.apps[project]
	if r == nil {
		r = &ring{}
		m.apps[project] = r
	}
	r.pts[min%Slots] = Point{T: min * 60, CPU: float32(round2(s.CPU)), Mem: float32(round2(s.MemMB)), Up: s.Up}
	r.last = min
	switch s.Up {
	case Down:
		r.fail++
	case Up:
		r.fail = 0
	}
}

// Forget apaga apps que não existem mais.
func (m *Monitor) Forget(alive map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := range m.apps {
		if !alive[p] {
			delete(m.apps, p)
		}
	}
}

// History devolve os pontos das últimas 24 h em ordem (minutos sem medição ficam de fora).
func (m *Monitor) History(project string) []Point {
	now := m.Now().Unix() / 60
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.apps[project]
	out := []Point{}
	if r == nil {
		return out
	}
	for i := now - Slots + 1; i <= now; i++ {
		p := r.pts[((i%Slots)+Slots)%Slots]
		if p.T == i*60 {
			out = append(out, p)
		}
	}
	return out
}

// Uptime diz a % de minutos em que o app respondia, entre os minutos medidos das últimas 24 h
// (parado de propósito não conta). ok=false se não houve medição.
func (m *Monitor) Uptime(project string) (pct float64, ok bool) {
	up, total := 0, 0
	for _, p := range m.History(project) {
		if p.Up == Unknown {
			continue
		}
		total++
		if p.Up == Up {
			up++
		}
	}
	if total == 0 {
		return 0, false
	}
	return round2(float64(up) / float64(total) * 100), true
}

// Health é o estado atual: Up, Down (já confirmado: downAfter minutos) ou Unknown.
func (m *Monitor) Health(project string) int8 {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.apps[project]
	if r == nil || m.Now().Unix()/60-r.last > 2 {
		return Unknown
	}
	if r.fail >= downAfter {
		return Down
	}
	p := r.pts[r.last%Slots]
	if p.Up == Down { // falhou uma vez só: ainda não conta como fora do ar
		return Up
	}
	return p.Up
}

// Alert avisa na central quando o app fica fora do ar (downAfter minutos seguidos) e quando volta.
func (m *Monitor) Alert(ev *events.Store, project, urn, name string) {
	key := "app-down:" + urn
	switch m.Health(project) {
	case Down:
		ev.Emit(events.Event{Kind: "app-down", Level: events.Aviso, Key: key,
			Title: name + " parou de responder",
			Body:  "O BrunOS não consegue abrir o app há alguns minutos. Veja os logs em Detalhes do app (botão direito no ícone) ou reinicie.",
			Meta:  map[string]any{"urn": urn}})
	case Up:
		ev.Resolve(key, &events.Event{Kind: "app-up", Level: events.Info, Title: name + " voltou", Body: "O app está respondendo de novo.", Meta: map[string]any{"urn": urn}})
	}
}

// Resolve encerra o aviso sem barulho (app parado de propósito, monitor desligado, app removido).
func Resolve(ev *events.Store, urn string) { ev.Resolve("app-down:"+urn, nil) }

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }
