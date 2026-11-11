// Package energy mede o consumo do servidor e calcula o custo (B3, termos em CONTEXT.md).
//
// O consumo vem do Medidor escolhido em Configurações → Energia:
//   - "auto" (padrão): tomada inteligente → bateria descarregando → estimativa calibrada.
//   - "plug": a tomada (uma Integração do tipo Tomada Tuya); "battery": a bateria descarregando;
//     "rapl": só o processador (RAPL, pelo script do servidor); "estimate": a estimativa calibrada.
//
// A estimativa calibrada soma o processador (RAPL) a um "resto da máquina" aprendido nas quedas de
// energia, quando a bateria mostra o consumo total. O RAPL só é lido quando o Medidor precisa dele.
//
// A cada minuto, mesmo sem ninguém olhando, o consumo vira kWh do dia. Guarda 24 h minuto a minuto e
// os totais por dia de 13 meses (/data/energy.json, poucos KB). Nada mais roda o tempo todo.
package energy

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"brunos/internal/store"
)

const (
	step       = time.Minute
	points     = 24 * 60 // 24 h por minuto
	keepDays   = 400     // ~13 meses
	saveEvery  = 10 * time.Minute
	minPlugW   = 1.0 // abaixo disso a tomada está sem energia (queda) ou desligada
	defTariff  = 0.90
	calibAlpha = 0.1 // peso de cada amostra nova na calibração
)

// Medidores aceitos.
const (
	MeterAuto     = "auto"
	MeterPlug     = "plug"
	MeterBattery  = "battery"
	MeterRAPL     = "rapl"
	MeterEstimate = "estimate"
)

var meters = map[string]bool{MeterAuto: true, MeterPlug: true, MeterBattery: true, MeterRAPL: true, MeterEstimate: true}

// Config é o que o Bruno escolhe.
type Config struct {
	Meter  string  `json:"meter"`
	PlugID string  `json:"plugId"` // integração Tomada Tuya ("" = a primeira cadastrada)
	Tariff float64 `json:"tariff"` // R$ por kWh
}

// Sources são as leituras que o main fornece (todas opcionais).
type Sources struct {
	// Plug lê a potência da tomada (W) da integração dada.
	Plug func(ctx context.Context, id string) (float64, error)
	// Battery devolve a potência da bateria (W) e se ela está descarregando (sem energia da tomada).
	Battery func() (w float64, discharging bool)
	// RAPL lê o contador de energia do processador (µJ) e o valor em que ele dá a volta.
	RAPL func(ctx context.Context) (uj, max uint64, err error)
	// Plugs lista as integrações Tomada Tuya (o Automático usa a primeira, se nenhuma foi escolhida).
	Plugs func() []string
}

type state struct {
	Version   int                `json:"v"`
	Config    Config             `json:"config"`
	Start     int64              `json:"start"`  // unix do ponto 0 da série de minutos
	Minute    []*float64         `json:"minute"` // W por minuto (nil = sem leitura)
	Days      map[string]float64 `json:"days"`   // "2026-11-10" → kWh
	Offset    *float64           `json:"offset"` // "resto da máquina" aprendido (W)
	OffsetN   int                `json:"offsetN"`
	LastMeter string             `json:"lastMeter"`
}

// Now é o retrato atual para a interface.
type Now struct {
	W          *float64 `json:"w"`          // consumo agora (nil = sem leitura)
	Meter      string   `json:"meter"`      // Medidor que valeu (plug, battery, rapl, estimate)
	TodayKWh   float64  `json:"todayKWh"`   // hoje
	MonthKWh   float64  `json:"monthKWh"`   // mês até agora
	Tariff     float64  `json:"tariff"`     // R$/kWh
	TodayCost  float64  `json:"todayCost"`  // R$
	MonthCost  float64  `json:"monthCost"`  // R$
	Projection float64  `json:"projection"` // R$ no mês inteiro, pelo ritmo até agora
	Offset     *float64 `json:"offset"`     // calibração (W), se já aprendida
	Err        string   `json:"err,omitempty"`
	At         int64    `json:"at"`
}

type raplReading struct {
	uj, max uint64
	at      time.Time
}

type Monitor struct {
	File string
	Src  Sources
	Now  func() time.Time

	mu    sync.Mutex
	st    state
	last  Now
	rapl  raplReading
	saved time.Time
	dirty bool
}

func New(file string, src Sources) *Monitor {
	m := &Monitor{File: file, Src: src, Now: time.Now}
	m.st = state{Version: 1, Config: Config{Meter: MeterAuto, Tariff: defTariff}, Days: map[string]float64{}}
	if file != "" {
		if _, err := store.ReadJSON(file, &m.st); err != nil {
			slog.Warn("energia: histórico de consumo ilegível, começando do zero", "erro", err)
		}
	}
	if m.st.Days == nil {
		m.st.Days = map[string]float64{}
	}
	if !meters[m.st.Config.Meter] {
		m.st.Config.Meter = MeterAuto
	}
	if m.st.Config.Tariff <= 0 {
		m.st.Config.Tariff = defTariff
	}
	return m
}

func (m *Monitor) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.Config
}

// ErrBadConfig: valores fora do aceitável.
var ErrBadConfig = errors.New("configuração de consumo inválida (tarifa entre 0,01 e 10 R$/kWh)")

func (m *Monitor) SetConfig(c Config) error {
	if !meters[c.Meter] || c.Tariff < 0.01 || c.Tariff > 10 || len(c.PlugID) > 64 {
		return ErrBadConfig
	}
	m.mu.Lock()
	m.st.Config = c
	m.dirty = true
	m.mu.Unlock()
	m.Save()
	return nil
}

// Run amostra a cada minuto até o contexto acabar.
func (m *Monitor) Run(ctx context.Context) {
	t := time.NewTicker(step)
	defer t.Stop()
	m.Sample(ctx)
	for {
		select {
		case <-ctx.Done():
			m.Save()
			return
		case <-t.C:
			m.Sample(ctx)
		}
	}
}

var (
	errNoRAPL  = errors.New("sem leitura do processador (RAPL): atualize o script do servidor em Configurações → Energia")
	errNoCalib = errors.New("a estimativa ainda não foi calibrada (ela aprende na próxima vez que o servidor rodar na bateria)")
	errNoPlug  = errors.New("nenhuma tomada inteligente cadastrada (Widgets → Integrações → Tomada Tuya)")
	errNoBat   = errors.New("a bateria só mostra o consumo quando o servidor está sem energia da tomada")
	errNoMeter = errors.New("nenhum medidor disponível: cadastre a tomada inteligente (ou o consumo aparece na bateria nas quedas de energia)")
)

// read decide o Medidor e lê a potência. Também calibra a estimativa durante as quedas.
func (m *Monitor) read(ctx context.Context, c Config) (float64, string, error) {
	var batW float64
	var onBat bool
	if m.Src.Battery != nil {
		batW, onBat = m.Src.Battery()
	}
	onBat = onBat && batW > 0
	plugID := c.PlugID
	if plugID == "" && m.Src.Plugs != nil {
		if ids := m.Src.Plugs(); len(ids) > 0 {
			plugID = ids[0]
		}
	}
	readPlug := func() (float64, error) {
		if plugID == "" || m.Src.Plug == nil {
			return 0, errNoPlug
		}
		return m.Src.Plug(ctx, plugID)
	}
	// o processador só é lido quando serve para algo: Medidor que depende dele, calibração na queda,
	// ou o Automático já calibrado (para a estimativa estar pronta se a tomada falhar)
	wantCPU := c.Meter == MeterRAPL || c.Meter == MeterEstimate || onBat || (c.Meter == MeterAuto && m.offset() != nil)
	cpuW, cpuOK := 0.0, false
	if wantCPU {
		cpuW, cpuOK = m.readRAPL(ctx)
	}
	if onBat && cpuOK {
		m.calibrate(batW - cpuW)
	}
	estimate := func() (float64, error) {
		if !cpuOK {
			return 0, errNoRAPL
		}
		off := m.offset()
		if off == nil {
			return 0, errNoCalib
		}
		return cpuW + *off, nil
	}
	switch c.Meter {
	case MeterPlug:
		w, err := readPlug()
		return w, MeterPlug, err
	case MeterBattery:
		if !onBat {
			return 0, MeterBattery, errNoBat
		}
		return batW, MeterBattery, nil
	case MeterRAPL:
		if !cpuOK {
			return 0, MeterRAPL, errNoRAPL
		}
		return cpuW, MeterRAPL, nil
	case MeterEstimate:
		w, err := estimate()
		return w, MeterEstimate, err
	}
	// Automático: tomada → bateria descarregando → estimativa
	if pw, err := readPlug(); err == nil && pw >= minPlugW {
		return pw, MeterPlug, nil
	}
	if onBat {
		return batW, MeterBattery, nil
	}
	if ew, err := estimate(); err == nil {
		return ew, MeterEstimate, nil
	}
	return 0, MeterAuto, errNoMeter
}

func (m *Monitor) offset() *float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.Offset
}

func (m *Monitor) calibrate(rest float64) {
	if rest < 0 || rest > 500 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v := rest
	if m.st.Offset != nil {
		v = *m.st.Offset*(1-calibAlpha) + rest*calibAlpha
	}
	m.st.Offset = &v
	m.st.OffsetN++
	m.dirty = true
}

// readRAPL lê o contador e devolve a potência média desde a última leitura.
func (m *Monitor) readRAPL(ctx context.Context) (float64, bool) {
	if m.Src.RAPL == nil {
		return 0, false
	}
	uj, max, err := m.Src.RAPL(ctx)
	if err != nil {
		return 0, false
	}
	now := m.Now()
	m.mu.Lock()
	prev := m.rapl
	m.rapl = raplReading{uj, max, now}
	m.mu.Unlock()
	dt := now.Sub(prev.at)
	if prev.at.IsZero() || dt < 5*time.Second || dt > 10*time.Minute {
		return 0, false // precisa de duas leituras próximas
	}
	d := float64(uj) - float64(prev.uj)
	if d < 0 && max > 0 {
		d += float64(max) // o contador deu a volta
	}
	if d < 0 {
		return 0, false
	}
	return d / 1e6 / dt.Seconds(), true
}

// Sample faz uma leitura e soma ao dia.
func (m *Monitor) Sample(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c := m.Config()
	w, meter, err := m.read(ctx, c)
	now := m.Now()
	m.mu.Lock()
	m.advance(now)
	if err == nil {
		v := math.Round(w*10) / 10
		if i := int(now.Unix()-m.st.Start) / int(step.Seconds()); i >= 0 && i < len(m.st.Minute) {
			m.st.Minute[i] = &v
		}
		m.st.Days[now.Format("2006-01-02")] += v * step.Hours() / 1000
		m.st.LastMeter = meter
		m.dirty = true
	}
	m.last = m.snapshotLocked(now, w, meter, err)
	save := m.dirty && now.Sub(m.saved) >= saveEvery
	m.mu.Unlock()
	if save {
		m.Save()
	}
}

// advance desliza a série de minutos até agora e apaga dias velhos.
func (m *Monitor) advance(now time.Time) {
	start := now.Truncate(step).Add(-time.Duration(points-1) * step).Unix()
	if len(m.st.Minute) != points || m.st.Start == 0 {
		m.st.Minute, m.st.Start = make([]*float64, points), start
		return
	}
	shift := int(start-m.st.Start) / int(step.Seconds())
	if shift <= 0 {
		return
	}
	if shift >= points {
		m.st.Minute = make([]*float64, points)
	} else {
		m.st.Minute = append(m.st.Minute[shift:], make([]*float64, shift)...)
	}
	m.st.Start = start
	cut := now.AddDate(0, 0, -keepDays).Format("2006-01-02")
	for d := range m.st.Days {
		if d < cut {
			delete(m.st.Days, d)
		}
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func (m *Monitor) snapshotLocked(now time.Time, w float64, meter string, err error) Now {
	c := m.st.Config
	today := m.st.Days[now.Format("2006-01-02")]
	month := 0.0
	prefix := now.Format("2006-01-")
	for d, k := range m.st.Days {
		if len(d) == 10 && d[:8] == prefix {
			month += k
		}
	}
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	days := first.AddDate(0, 1, 0).Sub(first).Hours() / 24
	elapsed := now.Sub(first).Hours() / 24
	proj := 0.0
	if elapsed > 0.25 { // menos de 6 horas de mês: projeção ainda sem sentido
		proj = month / elapsed * days * c.Tariff
	}
	n := Now{Meter: meter, TodayKWh: round3(today), MonthKWh: round3(month), Tariff: c.Tariff,
		TodayCost: round2(today * c.Tariff), MonthCost: round2(month * c.Tariff), Projection: round2(proj), At: now.Unix()}
	if m.st.Offset != nil {
		v := math.Round(*m.st.Offset*10) / 10
		n.Offset = &v
	}
	if err != nil {
		n.Err = err.Error()
	} else {
		v := math.Round(w*10) / 10
		n.W = &v
	}
	return n
}

// Current devolve o último retrato (com a tarifa atual, que pode ter mudado depois da amostra).
func (m *Monitor) Current() Now {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.last.At == 0 {
		return m.snapshotLocked(m.Now(), 0, "", errors.New("medindo…"))
	}
	n := m.last
	var err error
	w := 0.0
	if n.W != nil {
		w = *n.W
	} else {
		err = errors.New(n.Err)
	}
	return m.snapshotLocked(time.Unix(n.At, 0), w, n.Meter, err)
}

// Point é um ponto do gráfico.
type Point struct {
	T int64    `json:"t"`
	V *float64 `json:"v"`
}

// Series devolve as últimas 24 h (minuto a minuto).
func (m *Monitor) Series() []Point {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Point, 0, len(m.st.Minute))
	for i, v := range m.st.Minute {
		out = append(out, Point{T: m.st.Start + int64(i)*int64(step.Seconds()), V: v})
	}
	return out
}

// Day é o total de um dia.
type Day struct {
	Date string  `json:"date"`
	KWh  float64 `json:"kWh"`
	Cost float64 `json:"cost"`
}

// Days devolve os dias guardados (do mais antigo ao mais novo).
func (m *Monitor) Days() []Day {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Day, 0, len(m.st.Days))
	for d, k := range m.st.Days {
		out = append(out, Day{Date: d, KWh: round3(k), Cost: round2(k * m.st.Config.Tariff)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// Save grava o estado (atômico, 0600).
func (m *Monitor) Save() {
	if m.File == "" {
		return
	}
	m.mu.Lock()
	st := m.st
	st.Minute = append([]*float64(nil), m.st.Minute...)
	st.Days = make(map[string]float64, len(m.st.Days))
	for k, v := range m.st.Days {
		st.Days[k] = v
	}
	m.saved, m.dirty = m.Now(), false
	m.mu.Unlock()
	if err := store.WriteJSON(m.File, st, 0o600); err != nil {
		slog.Warn("energia: não deu para gravar o consumo", "erro", err)
	}
}
