// Package history é o Histórico do servidor (CPU, memória, temperatura, rede, ventoinha, bateria e a
// leitura/escrita de cada disco). Desde a 0.14.0 os pontos ficam no tsdb (1 min por 48 h, 5 min por 7
// dias, 20 min por 30 dias, com média e pico); este pacote recebe cada amostra do coletor, dá nome às
// séries e monta a resposta de /api/stats/history no formato de sempre. O /data/history.json antigo
// (24 h por minuto e 7 dias por hora) é trazido uma vez e apagado.
package history

import (
	"errors"
	"log/slog"
	"math"
	"os"
	"regexp"
	"strings"
	"time"

	"brunos/internal/store"
	"brunos/internal/sysinfo"
	"brunos/internal/tsdb"
)

// Nomes das séries do servidor.
const (
	CPU  = "sys.cpu"
	Mem  = "sys.mem"
	Temp = "sys.temp"
	Rx   = "sys.rx"
	Tx   = "sys.tx"
	Fan  = "sys.fan"
	Bat  = "sys.bat"
)

// Point é um ponto de /api/stats/history. Os *Max (pico do período) vêm só em 7 e 30 dias.
type Point struct {
	T       int64   `json:"t"`
	CPU     float32 `json:"cpu"`
	Mem     float32 `json:"mem"`
	Temp    float32 `json:"temp,omitempty"` // °C (0 = sem sensor)
	Rx      float32 `json:"rx"`             // bytes/s
	Tx      float32 `json:"tx"`
	CPUMax  float32 `json:"cpuMax,omitempty"`
	MemMax  float32 `json:"memMax,omitempty"`
	TempMax float32 `json:"tempMax,omitempty"`
	RxMax   float32 `json:"rxMax,omitempty"`
	TxMax   float32 `json:"txMax,omitempty"`
}

type Store struct{ DB *tsdb.DB }

// New usa o tsdb e traz o histórico antigo (oldPath), se existir.
func New(db *tsdb.DB, oldPath string) *Store {
	s := &Store{DB: db}
	if oldPath != "" {
		s.importOld(oldPath)
	}
	return s
}

// Add recebe uma amostra do coletor.
func (s *Store) Add(snap sysinfo.Snapshot, memPct float64) {
	db := s.DB
	db.Add(CPU, snap.CPU.Percent)
	db.Add(Mem, memPct)
	if snap.Temp != nil {
		db.Add(Temp, *snap.Temp)
	}
	db.Add(Rx, snap.Net.Rx)
	db.Add(Tx, snap.Net.Tx)
	if snap.FanRPM != nil {
		db.Add(Fan, float64(*snap.FanRPM))
	}
	if snap.Battery != nil {
		db.Add(Bat, float64(snap.Battery.Percent))
	}
	for _, d := range snap.DiskIO {
		db.Add("disk."+d.Name+".rd", d.Read)
		db.Add("disk."+d.Name+".wr", d.Write)
		db.Add("disk."+d.Name+".busy", d.Busy)
	}
	// temperatura dos discos com drivetemp/NVMe (0.15.0); a do SMART chega pelo monitors.go. Medida
	// velha (HD parado) não entra, para não desenhar uma linha reta falsa.
	now := time.Now().Unix()
	for _, d := range snap.Disks {
		if d.Temp != nil && now-d.TempAt < 120 {
			if dev := baseDev.FindString(strings.TrimPrefix(d.Device, "/dev/")); dev != "" {
				db.Add("disk."+dev+".temp", *d.Temp)
			}
		}
	}
}

// baseDev: sda1 → sda, nvme0n1p2 → nvme0n1.
var baseDev = regexp.MustCompile(`^(nvme\d+n\d+|sd[a-z]+)`)

// Ranges aceitos por /api/stats/history.
var Ranges = map[string]bool{"1h": true, "24h": true, "48h": true, "7d": true, "30d": true}

// Series monta os pontos do período juntando as séries pelo horário.
func (s *Store) Series(rng string) (step int64, pts []Point) {
	if !Ranges[rng] {
		rng = "24h"
	}
	by := map[int64]*Point{}
	order := []int64{}
	long := rng == "7d" || rng == "30d"
	for _, f := range []struct {
		name  string
		v, mx func(*Point) *float32
	}{
		{CPU, func(p *Point) *float32 { return &p.CPU }, func(p *Point) *float32 { return &p.CPUMax }},
		{Mem, func(p *Point) *float32 { return &p.Mem }, func(p *Point) *float32 { return &p.MemMax }},
		{Temp, func(p *Point) *float32 { return &p.Temp }, func(p *Point) *float32 { return &p.TempMax }},
		{Rx, func(p *Point) *float32 { return &p.Rx }, func(p *Point) *float32 { return &p.RxMax }},
		{Tx, func(p *Point) *float32 { return &p.Tx }, func(p *Point) *float32 { return &p.TxMax }},
	} {
		st, list := s.DB.Query(f.name, rng)
		step = st
		for _, q := range list {
			p := by[q.T]
			if p == nil {
				p = &Point{T: q.T}
				by[q.T] = p
				order = append(order, q.T)
			}
			*f.v(p) = r1(q.V)
			if long {
				*f.mx(p) = r1(q.Max)
			}
		}
	}
	sortInts(order)
	pts = make([]Point, 0, len(order))
	for _, t := range order {
		pts = append(pts, *by[t])
	}
	return step, pts
}

func sortInts(a []int64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

func r1(v float32) float32 { return float32(math.Round(float64(v)*10) / 10) }

// ── Histórico antigo (até a 0.13.x) ──

type oldPoint struct {
	T    int64   `json:"t"`
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
	Temp float64 `json:"temp,omitempty"`
	Rx   float64 `json:"rx"`
	Tx   float64 `json:"tx"`
}

type oldFile struct {
	Minutes []oldPoint `json:"minutes"`
	Hours   []oldPoint `json:"hours"`
}

func (s *Store) importOld(path string) {
	var f oldFile
	ok, err := store.ReadJSON(path, &f)
	if err != nil {
		slog.Warn("histórico antigo ilegível; começando do zero", "erro", err)
	}
	if !ok && err == nil {
		return
	}
	put := func(tier byte, p oldPoint) {
		s.DB.Import(CPU, tier, p.T, p.CPU, p.CPU)
		s.DB.Import(Mem, tier, p.T, p.Mem, p.Mem)
		if p.Temp > 0 {
			s.DB.Import(Temp, tier, p.T, p.Temp, p.Temp)
		}
		s.DB.Import(Rx, tier, p.T, p.Rx, p.Rx)
		s.DB.Import(Tx, tier, p.T, p.Tx, p.Tx)
	}
	// as horas preenchem as camadas de 5 e 20 min (cada hora vira 12 e 3 pontos iguais); os minutos,
	// a fina e por cima as outras duas nas últimas 24 h
	for _, p := range f.Hours {
		for k := int64(0); k < 3600; k += tsdb.MidStep {
			put(1, oldPoint{T: p.T + k, CPU: p.CPU, Mem: p.Mem, Temp: p.Temp, Rx: p.Rx, Tx: p.Tx})
		}
		for k := int64(0); k < 3600; k += tsdb.CoarseStep {
			put(2, oldPoint{T: p.T + k, CPU: p.CPU, Mem: p.Mem, Temp: p.Temp, Rx: p.Rx, Tx: p.Tx})
		}
	}
	for _, p := range f.Minutes {
		put(0, p)
	}
	s.DB.Save()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("não foi possível apagar o histórico antigo", "erro", err)
	}
	slog.Info("histórico antigo trazido para o tsdb", "minutos", len(f.Minutes), "horas", len(f.Hours))
}
