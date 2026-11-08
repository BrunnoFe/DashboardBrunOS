// Package netmon acompanha a internet do servidor: a cada minuto abre uma conexão TCP com alguns
// servidores públicos (Cloudflare, Google, Quad9, porta 443) e mede quanto demora (latência). Se
// nenhum responder por 2 minutos seguidos, a internet caiu: avisa na central, e avisa de novo quando
// volta, com quanto tempo ficou fora. Não usa ICMP (ping), que exigiria privilégio no container.
//
// As últimas 24 h ficam na memória; as quedas (até 100) ficam em /data/internet.json.
package netmon

import (
	"context"
	"log/slog"
	"net"
	"sort"
	"sync"
	"time"

	"brunos/internal/events"
	"brunos/internal/store"
)

const (
	slots      = 1440
	downAfter  = 2
	maxOutages = 100
)

var DefaultTargets = []string{"1.1.1.1:443", "8.8.8.8:443", "9.9.9.9:443"}

type Point struct {
	T  int64   `json:"t"`
	Ms float32 `json:"ms"` // -1 = sem internet
}

type Outage struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"` // 0 = ainda fora
}

type Status struct {
	Up      bool     `json:"up"`
	Checked bool     `json:"checked"`
	Ms      float32  `json:"ms"`
	Uptime  *float64 `json:"uptime,omitempty"` // % dos minutos medidos nas últimas 24 h
	Avg     float32  `json:"avg"`              // latência média (ms) das últimas 24 h
	Points  []Point  `json:"points"`
	Outages []Outage `json:"outages"`
	Enabled bool     `json:"enabled"`
}

type file struct {
	Version int      `json:"version"`
	Outages []Outage `json:"outages"`
}

type Monitor struct {
	File    string
	Now     func() time.Time
	Targets []string
	// Dial mede o tempo para abrir uma conexão (trocável nos testes).
	Dial    func(ctx context.Context, addr string) (time.Duration, error)
	Enabled func() bool

	ev        *events.Store
	mu        sync.Mutex
	pts       [slots]Point
	last      Point
	fails     int
	downSince int64
	outages   []Outage
}

func New(path string, ev *events.Store) *Monitor {
	m := &Monitor{File: path, Now: time.Now, Targets: DefaultTargets, Dial: dialTCP, ev: ev, Enabled: func() bool { return true }}
	if path != "" {
		var f file
		if _, err := store.ReadJSON(path, &f); err != nil {
			slog.Warn("internet: arquivo ilegível", "erro", err)
		}
		m.outages = f.Outages
	}
	return m
}

func dialTCP(ctx context.Context, addr string) (time.Duration, error) {
	d := net.Dialer{Timeout: 4 * time.Second}
	start := time.Now()
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return 0, err
	}
	el := time.Since(start)
	c.Close()
	return el, nil
}

// Check mede uma vez (todos os alvos ao mesmo tempo; vale o mais rápido) e trata quedas.
func (m *Monitor) Check(ctx context.Context) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	best := time.Duration(-1)
	for _, t := range m.Targets {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			if d, err := m.Dial(ctx, addr); err == nil {
				mu.Lock()
				if best < 0 || d < best {
					best = d
				}
				mu.Unlock()
			}
		}(t)
	}
	wg.Wait()
	now := m.Now()
	min := now.Unix() / 60
	p := Point{T: min * 60, Ms: -1}
	if best >= 0 {
		p.Ms = float32(int64(best.Seconds()*10000+0.5)) / 10 // ms com uma casa
	}
	m.mu.Lock()
	m.pts[min%slots] = p
	m.last = p
	var emitDown, emitUp bool
	var outage Outage
	if p.Ms < 0 {
		if m.fails == 0 {
			m.downSince = p.T
		}
		m.fails++
		if m.fails == downAfter {
			emitDown = true
			m.outages = append(m.outages, Outage{Start: m.downSince})
			m.trimSave()
		}
	} else {
		if m.fails >= downAfter {
			emitUp = true
			if n := len(m.outages); n > 0 && m.outages[n-1].End == 0 {
				m.outages[n-1].End = p.T
				outage = m.outages[n-1]
			}
			m.trimSave()
		}
		m.fails = 0
	}
	m.mu.Unlock()
	if m.ev == nil {
		return
	}
	if emitDown {
		m.ev.Emit(events.Event{Kind: "internet-down", Level: events.Aviso, Key: "internet-down",
			Title: "A internet caiu", Body: "O servidor não consegue alcançar a internet há alguns minutos. Os apps da rede de casa continuam funcionando."})
	}
	if emitUp {
		mins := (outage.End - outage.Start) / 60
		body := "A conexão voltou."
		if outage.End > 0 {
			body = "Ficou cerca de " + plural(int(mins), "minuto", "minutos") + " sem internet."
		}
		m.ev.Resolve("internet-down", &events.Event{Kind: "internet-up", Level: events.Info, Title: "A internet voltou", Body: body})
	}
}

func plural(n int, one, many string) string {
	if n <= 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// trimSave guarda as quedas (chamar com m.mu travado).
func (m *Monitor) trimSave() {
	if len(m.outages) > maxOutages {
		m.outages = m.outages[len(m.outages)-maxOutages:]
	}
	if m.File == "" {
		return
	}
	if err := store.WriteJSON(m.File, file{Version: 1, Outages: m.outages}, 0o600); err != nil {
		slog.Error("internet: não foi possível gravar", "erro", err)
	}
}

// Run mede a cada minuto enquanto o monitor estiver ligado nas preferências.
func (m *Monitor) Run(ctx context.Context) {
	time.Sleep(15 * time.Second)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if m.Enabled() {
			cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			m.Check(cctx)
			cancel()
		} else {
			m.ev.Resolve("internet-down", nil)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (m *Monitor) Status() Status {
	now := m.Now().Unix() / 60
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Points: []Point{}, Enabled: m.Enabled()}
	up, total, sum, n := 0, 0, 0.0, 0
	for i := now - slots + 1; i <= now; i++ {
		p := m.pts[((i%slots)+slots)%slots]
		if p.T != i*60 {
			continue
		}
		st.Points = append(st.Points, p)
		total++
		if p.Ms >= 0 {
			up++
			sum += float64(p.Ms)
			n++
		}
	}
	if total > 0 {
		pct := float64(int64(float64(up)/float64(total)*10000+0.5)) / 100
		st.Uptime = &pct
	}
	if n > 0 {
		st.Avg = float32(int64(sum/float64(n)*10+0.5)) / 10
	}
	st.Checked = m.last.T > 0 && now*60-m.last.T <= 180
	st.Up = st.Checked && m.fails < downAfter
	if m.last.Ms >= 0 {
		st.Ms = m.last.Ms
	}
	st.Outages = append([]Outage(nil), m.outages...)
	sort.Slice(st.Outages, func(i, j int) bool { return st.Outages[i].Start > st.Outages[j].Start })
	if len(st.Outages) > 20 {
		st.Outages = st.Outages[:20]
	}
	return st
}
