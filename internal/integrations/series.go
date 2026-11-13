package integrations

import (
	"log/slog"
	"math"
	"sync"
	"time"

	"brunos/internal/store"
)

// Histórico próprio de métricas sem histórico no app (coleta em segundo plano, decisão 32):
// 24 h a cada 5 min (288 pontos) e 30 dias por hora (720 pontos, média). Alguns KB por métrica.
const (
	fineStep   = 300
	fineSlots  = 288
	hourStep   = 3600
	hourSlots  = 720 // 30 dias por hora (0.14.0; era 7 dias)
	staleAfter = 30 * 24 * time.Hour
)

type ring struct {
	fine     [fineSlots]float64
	fineAt   [fineSlots]int64 // epoch (t/fineStep) de cada casa; 0 = vazia
	hour     [hourSlots]float64
	hourAt   [hourSlots]int64
	hourN    [hourSlots]uint16
	lastSeen int64
}

type seriesFile struct {
	Version int                    `json:"version"`
	Metrics map[string]seriesSaved `json:"metrics"`
}

type seriesSaved struct {
	Fine  SeriesData `json:"fine"`
	Hours SeriesData `json:"hours"`
}

// SeriesStore guarda as séries em /data/metric-history.json.
type SeriesStore struct {
	path string
	Now  func() time.Time
	mu   sync.Mutex
	m    map[string]*ring
}

func ridx(n int64, size int) int { return int(((n % int64(size)) + int64(size)) % int64(size)) }

func NewSeriesStore(path string) *SeriesStore {
	s := &SeriesStore{path: path, Now: time.Now, m: map[string]*ring{}}
	if path == "" {
		return s
	}
	var f seriesFile
	if _, err := store.ReadJSON(path, &f); err != nil {
		slog.Warn("histórico das integrações: arquivo ilegível, começando do zero", "erro", err)
	}
	for id, sv := range f.Metrics {
		r := &ring{}
		load := func(sd SeriesData, step int64, size int, vals []float64, ats []int64, n []uint16) {
			for i, v := range sd.Points {
				if math.IsNaN(v) {
					continue
				}
				ep := (sd.Start + int64(i)*sd.Step) / step
				k := ridx(ep, size)
				vals[k], ats[k] = v, ep
				if n != nil {
					n[k] = 1
				}
				if t := ep * step; t > r.lastSeen {
					r.lastSeen = t
				}
			}
		}
		load(sv.Fine, fineStep, fineSlots, r.fine[:], r.fineAt[:], nil)
		load(sv.Hours, hourStep, hourSlots, r.hour[:], r.hourAt[:], r.hourN[:])
		s.m[id] = r
	}
	return s
}

// Add grava uma amostra da métrica id (ex.: "a1b2c3.download").
func (s *SeriesStore) Add(id string, v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	now := s.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.m[id]
	if r == nil {
		r = &ring{}
		s.m[id] = r
	}
	fe, he := now/fineStep, now/hourStep
	k := ridx(fe, fineSlots)
	r.fine[k], r.fineAt[k] = v, fe
	h := ridx(he, hourSlots)
	if r.hourAt[h] != he {
		r.hour[h], r.hourAt[h], r.hourN[h] = v, he, 1
	} else {
		n := float64(r.hourN[h])
		r.hour[h] = (r.hour[h]*n + v) / (n + 1)
		if r.hourN[h] < math.MaxUint16 {
			r.hourN[h]++
		}
	}
	r.lastSeen = now
}

// Series devolve "1h", "24h" (5 min) ou "48h", "7d", "30d" (hora). ok = false se a métrica não tem histórico.
func (s *SeriesStore) Series(id, rng string) (SeriesData, bool) {
	now := s.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.m[id]
	if r == nil {
		return SeriesData{}, false
	}
	return r.series(now, rng), true
}

func (r *ring) series(now int64, rng string) SeriesData {
	step, size, n := int64(fineStep), fineSlots, int64(fineSlots)
	vals, ats := r.fine[:], r.fineAt[:]
	switch rng {
	case "48h", "7d", "30d":
		step, size, vals, ats = hourStep, hourSlots, r.hour[:], r.hourAt[:]
		n = map[string]int64{"48h": 48, "7d": 168, "30d": hourSlots}[rng]
	case "1h":
		n = 12
	}
	cur := now / step
	pts := make([]float64, n)
	for i := int64(0); i < n; i++ {
		ep := cur - n + 1 + i
		k := ridx(ep, size)
		if ats[k] == ep {
			pts[i] = vals[k]
		} else {
			pts[i] = math.NaN()
		}
	}
	return SeriesData{Step: step, Start: (cur - n + 1) * step, Points: pts}
}

// Forget apaga as métricas cujo id começa com prefix (integração apagada).
func (s *SeriesStore) Forget(prefix string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.m {
		if len(id) > len(prefix) && id[:len(prefix)] == prefix {
			delete(s.m, id)
		}
	}
}

// Save grava tudo e descarta métricas sem amostra há 7 dias.
func (s *SeriesStore) Save() {
	if s.path == "" {
		return
	}
	now := s.Now()
	s.mu.Lock()
	f := seriesFile{Version: 1, Metrics: map[string]seriesSaved{}}
	for id, r := range s.m {
		if now.Sub(time.Unix(r.lastSeen, 0)) > staleAfter {
			delete(s.m, id)
			continue
		}
		f.Metrics[id] = seriesSaved{Fine: r.series(now.Unix(), "24h"), Hours: r.series(now.Unix(), "7d")}
	}
	s.mu.Unlock()
	if err := store.WriteJSON(s.path, f, 0o600); err != nil {
		slog.Error("histórico das integrações: não foi possível gravar", "erro", err)
	}
}
