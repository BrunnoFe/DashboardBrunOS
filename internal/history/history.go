// Package history guarda o uso do servidor por longos períodos: média por minuto das últimas 24 h e
// média por hora dos últimos 7 dias (CPU, memória, temperatura, rede). Recebe cada amostra do
// coletor (a cada 2 ou 15 s) e faz a média ponderada pelo tempo. Fica em /data/history.json
// (gravado a cada 10 minutos e ao desligar), uns 100 KB.
package history

import (
	"log/slog"
	"sync"
	"time"

	"brunos/internal/store"
)

const (
	minuteSlots = 1440 // 24 h
	hourSlots   = 168  // 7 dias
	saveEvery   = 10 * time.Minute
)

type Point struct {
	T    int64   `json:"t"`
	CPU  float32 `json:"cpu"`
	Mem  float32 `json:"mem"`            // % da memória
	Temp float32 `json:"temp,omitempty"` // °C (0 = sem sensor)
	Rx   float32 `json:"rx"`             // bytes/s
	Tx   float32 `json:"tx"`
}

// acc soma as amostras de um período (minuto ou hora), ponderadas pelo tempo.
type acc struct {
	start               int64
	w, cpu, mem, rx, tx float64
	tempW, temp         float64
}

func (a *acc) add(cpu, mem float64, temp *float64, rx, tx, dt float64) {
	a.w += dt
	a.cpu += cpu * dt
	a.mem += mem * dt
	a.rx += rx * dt
	a.tx += tx * dt
	if temp != nil {
		a.tempW += dt
		a.temp += *temp * dt
	}
}

func (a *acc) point() (Point, bool) {
	if a.w <= 0 {
		return Point{}, false
	}
	p := Point{T: a.start, CPU: r1(a.cpu / a.w), Mem: r1(a.mem / a.w), Rx: r1(a.rx / a.w), Tx: r1(a.tx / a.w)}
	if a.tempW > 0 {
		p.Temp = r1(a.temp / a.tempW)
	}
	return p, true
}

type file struct {
	Version int     `json:"version"`
	Minutes []Point `json:"minutes"`
	Hours   []Point `json:"hours"`
}

type Store struct {
	File string
	Now  func() time.Time

	mu      sync.Mutex
	minutes [minuteSlots]Point
	hours   [hourSlots]Point
	curMin  acc
	curHour acc
	savedAt time.Time
}

// New lê o arquivo (se existir). path vazio = só em memória (testes).
func New(path string) *Store {
	s := &Store{File: path, Now: time.Now}
	if path == "" {
		return s
	}
	var f file
	if _, err := store.ReadJSON(path, &f); err != nil {
		slog.Warn("histórico: arquivo ilegível, começando do zero", "erro", err)
	}
	for _, p := range f.Minutes {
		s.minutes[idx(p.T/60, minuteSlots)] = p
	}
	for _, p := range f.Hours {
		s.hours[idx(p.T/3600, hourSlots)] = p
	}
	s.savedAt = s.Now()
	return s
}

func idx(n int64, size int) int { return int(((n % int64(size)) + int64(size)) % int64(size)) }

// Add recebe uma amostra do coletor. dt = segundos desde a amostra anterior.
func (s *Store) Add(cpu, mem float64, temp *float64, rx, tx, dt float64) {
	if dt <= 0 || dt > 120 { // primeira amostra ou o servidor dormiu: não pondera um buraco
		dt = 2
	}
	now := s.Now()
	min, hour := now.Unix()/60*60, now.Unix()/3600*3600
	s.mu.Lock()
	if s.curMin.start != min {
		if p, ok := s.curMin.point(); ok {
			s.minutes[idx(p.T/60, minuteSlots)] = p
		}
		s.curMin = acc{start: min}
	}
	if s.curHour.start != hour {
		if p, ok := s.curHour.point(); ok {
			s.hours[idx(p.T/3600, hourSlots)] = p
		}
		s.curHour = acc{start: hour}
	}
	s.curMin.add(cpu, mem, temp, rx, tx, dt)
	s.curHour.add(cpu, mem, temp, rx, tx, dt)
	due := s.File != "" && now.Sub(s.savedAt) >= saveEvery
	s.mu.Unlock()
	if due {
		s.Save()
	}
}

// Series devolve os pontos do período: "1h" e "24h" por minuto, "7d" por hora. O período em
// andamento entra como último ponto (parcial).
func (s *Store) Series(rng string) (step int, pts []Point) {
	now := s.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	pts = []Point{}
	switch rng {
	case "7d":
		cur := now / 3600
		for h := cur - hourSlots + 1; h < cur; h++ {
			if p := s.hours[idx(h, hourSlots)]; p.T == h*3600 {
				pts = append(pts, p)
			}
		}
		// a hora em andamento (ou a última, se ainda não chegou amostra da hora nova) ainda está somando
		if p, ok := s.curHour.point(); ok && p.T > (cur-hourSlots)*3600 && (len(pts) == 0 || p.T > pts[len(pts)-1].T) {
			pts = append(pts, p)
		}
		return 3600, pts
	default:
		n := int64(minuteSlots)
		if rng == "1h" {
			n = 60
		}
		cur := now / 60
		for m := cur - n + 1; m < cur; m++ {
			if p := s.minutes[idx(m, minuteSlots)]; p.T == m*60 {
				pts = append(pts, p)
			}
		}
		if p, ok := s.curMin.point(); ok && p.T > (cur-n)*60 && (len(pts) == 0 || p.T > pts[len(pts)-1].T) {
			pts = append(pts, p)
		}
		return 60, pts
	}
}

// Save grava o histórico (chamado a cada 10 min e ao desligar o BrunOS).
func (s *Store) Save() {
	if s.File == "" {
		return
	}
	_, mins := s.Series("24h")
	_, hours := s.Series("7d")
	s.mu.Lock()
	s.savedAt = s.Now()
	s.mu.Unlock()
	if err := store.WriteJSON(s.File, file{Version: 1, Minutes: mins, Hours: hours}, 0o600); err != nil {
		slog.Error("histórico: não foi possível gravar", "erro", err)
	}
}

func r1(v float64) float32 { return float32(float64(int64(v*10+0.5)) / 10) }
