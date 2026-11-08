// Package diskhist guarda o uso diário de cada disco e prevê quando ele vai encher.
//
// Um ponto por disco por dia (o último valor do dia), até 180 dias, em /data/disk-history.json.
// A previsão é uma reta (regressão linear) sobre os últimos 30 dias: quantos bytes por dia o
// disco ganha e, no ritmo atual, em quantos dias o espaço livre acaba.
package diskhist

import (
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"brunos/internal/events"
	"brunos/internal/store"
)

const (
	keepDays   = 180
	windowDays = 30
	minPoints  = 5 // pelo menos 5 dias medidos…
	minSpan    = 6 // …espalhados por uma semana, antes de arriscar uma previsão
	maxDays    = 3650
)

// Limites dos avisos (com folga para não ficar avisando e desavisando).
const (
	SoonDays   = 30 // avisa quando a previsão cai para 30 dias ou menos
	SoonClear  = 45 // e encerra quando passa de 45 dias (ou o disco para de crescer)
	FullPct    = 90 // avisa com 90% ocupado
	FullClear  = 85 // e encerra abaixo de 85%
	dayLayout  = "2006-01-02"
	fileFormat = 1
)

// Usage é a leitura de um disco agora.
type Usage struct {
	Name string
	Used uint64 // bytes ocupados
	Free uint64 // bytes livres para gravar
}

type Point struct {
	Day  string `json:"d"`
	Used uint64 `json:"u"`
	Free uint64 `json:"f"`
}

type Forecast struct {
	Name     string  `json:"name"`
	PerDay   float64 `json:"perDay"`             // bytes por dia (negativo = liberando espaço)
	DaysLeft *int    `json:"daysLeft,omitempty"` // nil = sem dados suficientes ou não está enchendo
	Enough   bool    `json:"enough"`             // já há dados para prever
	Points   []Point `json:"points"`
}

type file struct {
	Version int                `json:"version"`
	Disks   map[string][]Point `json:"disks"`
}

type Store struct {
	File string
	Now  func() time.Time

	mu    sync.Mutex
	disks map[string][]Point
}

// New lê o arquivo (se existir). path vazio = só em memória (testes).
func New(path string) *Store {
	s := &Store{File: path, Now: time.Now, disks: map[string][]Point{}}
	if path != "" {
		var f file
		if _, err := store.ReadJSON(path, &f); err != nil {
			slog.Warn("histórico de discos ilegível, começando do zero", "erro", err)
		}
		if f.Disks != nil {
			s.disks = f.Disks
		}
	}
	return s
}

// Record guarda o uso de hoje (substitui o valor anterior do mesmo dia).
func (s *Store) Record(list []Usage) {
	now := s.Now()
	today := now.Format(dayLayout)
	cut := now.AddDate(0, 0, -keepDays).Format(dayLayout)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range list {
		if u.Name == "" || u.Used+u.Free == 0 {
			continue
		}
		pts := s.disks[u.Name]
		p := Point{Day: today, Used: u.Used, Free: u.Free}
		if n := len(pts); n > 0 && pts[n-1].Day == today {
			pts[n-1] = p
		} else {
			pts = append(pts, p)
		}
		s.disks[u.Name] = pts
	}
	for name, pts := range s.disks {
		i := sort.Search(len(pts), func(i int) bool { return pts[i].Day >= cut })
		if i == len(pts) {
			delete(s.disks, name) // disco sumiu há meses
		} else if i > 0 {
			s.disks[name] = append([]Point(nil), pts[i:]...)
		}
	}
	if s.File != "" {
		if err := store.WriteJSON(s.File, file{Version: fileFormat, Disks: s.disks}, 0o600); err != nil {
			slog.Error("histórico de discos: não foi possível gravar", "erro", err)
		}
	}
}

// Forecasts devolve a previsão de cada disco, em ordem de nome.
func (s *Store) Forecasts() []Forecast {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Forecast, 0, len(s.disks))
	for name, pts := range s.disks {
		f := predict(pts)
		f.Name = name
		f.Points = append([]Point(nil), pts...)
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func dayNum(d string) (float64, bool) {
	t, err := time.Parse(dayLayout, d)
	if err != nil {
		return 0, false
	}
	return float64(t.Unix()) / 86400, true
}

// predict ajusta uma reta (mínimos quadrados) ao uso dos últimos 30 dias.
func predict(pts []Point) Forecast {
	var f Forecast
	if len(pts) == 0 {
		return f
	}
	last, ok := dayNum(pts[len(pts)-1].Day)
	if !ok {
		return f
	}
	var xs, ys []float64
	for _, p := range pts {
		x, ok := dayNum(p.Day)
		if !ok || x < last-windowDays {
			continue
		}
		xs, ys = append(xs, x), append(ys, float64(p.Used))
	}
	if len(xs) < minPoints || last-xs[0] < minSpan {
		return f
	}
	f.Enough = true
	n := float64(len(xs))
	var sx, sy float64
	for i := range xs {
		sx, sy = sx+xs[i], sy+ys[i]
	}
	mx, my := sx/n, sy/n
	var num, den float64
	for i := range xs {
		num += (xs[i] - mx) * (ys[i] - my)
		den += (xs[i] - mx) * (xs[i] - mx)
	}
	if den == 0 {
		return f
	}
	f.PerDay = math.Round(num / den)
	free := float64(pts[len(pts)-1].Free)
	if f.PerDay > 0 {
		if d := free / f.PerDay; d <= maxDays {
			days := int(d)
			f.DaysLeft = &days
		}
	}
	return f
}

// Check avisa na central quando um disco está quase cheio e, se forecast estiver ligado
// (Configurações → Previsão de disco cheio), quando ele vai encher em breve.
func (s *Store) Check(ev *events.Store, now []Usage, forecast bool) {
	byName := map[string]Forecast{}
	for _, f := range s.Forecasts() {
		byName[f.Name] = f
	}
	for _, u := range now {
		total := u.Used + u.Free
		if total == 0 {
			continue
		}
		pct := float64(u.Used) / float64(total) * 100
		key := "disk-full:" + u.Name
		switch {
		case pct >= FullPct:
			ev.Emit(events.Event{Kind: "disk-full", Level: events.Aviso, Key: key,
				Title: "O disco " + u.Name + " está quase cheio",
				Body:  "Ele já está " + itoa(int(pct)) + "% ocupado. Apague ou mova arquivos (a lixeira do BrunOS também ocupa espaço)."})
		case pct < FullClear:
			ev.Resolve(key, &events.Event{Kind: "disk-full-ok", Level: events.Info,
				Title: "O disco " + u.Name + " tem espaço de novo", Body: "Agora ele está " + itoa(int(pct)) + "% ocupado."})
		}

		f := byName[u.Name]
		key = "disk-full-soon:" + u.Name
		switch {
		case forecast && f.DaysLeft != nil && *f.DaysLeft <= SoonDays && pct < FullPct: // quase cheio já tem aviso próprio
			ev.Emit(events.Event{Kind: "disk-full-soon", Level: events.Aviso, Key: key,
				Title: "O disco " + u.Name + " deve encher em ~" + plural(*f.DaysLeft, "dia", "dias"),
				Body:  "No ritmo das últimas semanas ele ganha " + human(f.PerDay) + " por dia. Vale liberar espaço ou mover arquivos para outro disco.",
				Meta:  map[string]any{"daysLeft": *f.DaysLeft, "perDay": f.PerDay}})
		case !forecast || f.DaysLeft == nil || *f.DaysLeft > SoonClear:
			ev.Resolve(key, nil) // desligar a previsão encerra o aviso sem barulho
		}
	}
}

var itoa = strconv.Itoa

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

// human escreve bytes como "1,5 GB" (base 1000, igual ao fmtBytes da interface).
func human(b float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for math.Abs(b) >= 1000 && i < len(units)-1 {
		b /= 1000
		i++
	}
	s := strconv.FormatFloat(b, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return strings.Replace(s, ".", ",", 1) + " " + units[i]
}
