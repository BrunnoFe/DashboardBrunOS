// Package smart acompanha a saúde dos discos pelo SMART (D3, decisão 43).
//
// O BrunOS não tem root: pede ao script do servidor (brunos-host 4) o "smart" (smartctl -j -a) a cada
// 6 h e a "temperatura" (smartctl -j -A) a cada 20 min, sempre com -n standby: HD parado não é
// acordado e fica para a próxima. Adaptadores USB (que o drivetemp não atravessa) são lidos com
// -d sat; a temperatura deles vai para a lista de discos do Uso ao vivo.
//
// Selo: OK, Atenção (setores realocados/pendentes/incorrigíveis, erros de mídia no NVMe) ou Falhando
// (o próprio disco diz que falhou). Avisos no sino uma vez por mudança. Estado em /data/smart.json.
package smart

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"brunos/internal/events"
	"brunos/internal/store"
)

const (
	fullEvery = 6 * time.Hour
	tempEvery = 20 * time.Minute
	maxHist   = 30
)

// Selos.
const (
	HealthOK   = "ok"
	HealthWarn = "atencao"
	HealthFail = "falhando"
)

// Change é um momento em que os números importantes mudaram.
type Change struct {
	At          int64 `json:"at"`
	Reallocated int64 `json:"reallocated"`
	Pending     int64 `json:"pending"`
	Passed      bool  `json:"passed"`
}

// Disk é o que se sabe de um disco.
type Disk struct {
	Dev           string   `json:"dev"` // sda, nvme0n1
	Model         string   `json:"model,omitempty"`
	Serial        string   `json:"serial,omitempty"`
	Health        string   `json:"health"`
	Passed        *bool    `json:"passed,omitempty"`
	Temp          *float64 `json:"temp,omitempty"`
	TempAt        int64    `json:"tempAt,omitempty"`
	PowerOnHours  *int64   `json:"powerOnHours,omitempty"`
	PowerCycles   *int64   `json:"powerCycles,omitempty"`
	Reallocated   int64    `json:"reallocated"`
	Pending       int64    `json:"pending"`
	Uncorrectable int64    `json:"uncorrectable"`
	CRCErrors     int64    `json:"crcErrors"`
	MediaErrors   int64    `json:"mediaErrors"`           // NVMe
	PercentUsed   *int64   `json:"percentUsed,omitempty"` // NVMe: desgaste
	CheckedAt     int64    `json:"checkedAt"`             // última leitura completa
	Standby       bool     `json:"standby,omitempty"`     // estava parado na última tentativa
	History       []Change `json:"history,omitempty"`
}

type state struct {
	Version   int              `json:"v"`
	Disks     map[string]*Disk `json:"disks"`
	CheckedAt int64            `json:"checkedAt"`
	Err       string           `json:"err,omitempty"`
}

// Query faz um pedido ao script do servidor (main: power.HostOutput).
type Query func(ctx context.Context, cmd string) ([]byte, error)

type Monitor struct {
	File    string
	Query   Query
	Events  *events.Store
	SetTemp func(dev string, celsius float64, at time.Time) // temperatura para a lista de discos
	Now     func() time.Time

	mu       sync.Mutex
	st       state
	lastFull time.Time
	lastTemp time.Time
	running  bool
}

func New(file string, q Query, ev *events.Store) *Monitor {
	m := &Monitor{File: file, Query: q, Events: ev, Now: time.Now, st: state{Version: 1, Disks: map[string]*Disk{}}}
	if file != "" {
		if _, err := store.ReadJSON(file, &m.st); err != nil {
			slog.Warn("smart: estado ilegível, começando do zero", "erro", err)
		}
	}
	if m.st.Disks == nil {
		m.st.Disks = map[string]*Disk{}
	}
	if m.st.CheckedAt > 0 {
		m.lastFull = time.Unix(m.st.CheckedAt, 0)
	}
	return m
}

// Run confere a cada minuto se é hora de ler (6 h o SMART completo, 20 min a temperatura).
func (m *Monitor) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	select { // espera o servidor subir antes da primeira leitura
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	for {
		now := m.Now()
		if now.Sub(m.lastFull) >= fullEvery {
			_ = m.Refresh(ctx)
		} else if now.Sub(m.lastTemp) >= tempEvery {
			m.readTemps(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ErrBusy: já há uma leitura em andamento.
var ErrBusy = errors.New("já estou lendo os discos, espere um pouco")

// Refresh lê o SMART completo agora.
func (m *Monitor) Refresh(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return ErrBusy
	}
	m.running = true
	now := m.Now()
	m.lastFull, m.lastTemp = now, now
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.running = false; m.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := m.Query(ctx, "smart")
	m.mu.Lock()
	if err != nil {
		m.st.Err = friendly(err)
		msg := m.st.Err
		m.mu.Unlock()
		m.save()
		return errors.New(msg)
	}
	m.st.Err = ""
	m.st.CheckedAt = now.Unix()
	var notify []events.Event
	for _, r := range ParseBlocks(out) {
		d := m.st.Disks[r.dev]
		if d == nil {
			d = &Disk{Dev: r.dev}
			m.st.Disks[r.dev] = d
		}
		if r.standby {
			d.Standby = true
			continue
		}
		if r.data == nil {
			continue
		}
		prev := *d
		apply(d, r.data, now, true)
		notify = append(notify, changes(prev, d)...)
		if d.Temp != nil && m.SetTemp != nil {
			m.SetTemp(d.Dev, *d.Temp, now)
		}
	}
	m.mu.Unlock()
	if m.Events != nil {
		for _, e := range notify {
			m.Events.Emit(e)
		}
	}
	m.save()
	return nil
}

// readTemps lê só a temperatura (discos parados ficam com a última medida).
func (m *Monitor) readTemps(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	now := m.Now()
	m.mu.Lock()
	m.lastTemp = now
	m.mu.Unlock()
	out, err := m.Query(ctx, "temperatura")
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range ParseBlocks(out) {
		if r.standby || r.data == nil {
			continue
		}
		d := m.st.Disks[r.dev]
		if d == nil {
			d = &Disk{Dev: r.dev}
			m.st.Disks[r.dev] = d
		}
		apply(d, r.data, now, false)
		if d.Temp != nil && m.SetTemp != nil {
			m.SetTemp(d.Dev, *d.Temp, now)
		}
	}
}

// changes compara antes e depois e monta os avisos (uma vez por mudança).
func changes(prev Disk, d *Disk) []events.Event {
	var out []events.Event
	name := d.Dev
	if d.Model != "" {
		name = fmt.Sprintf("%s (%s)", d.Dev, d.Model)
	}
	id := d.Serial
	if id == "" {
		id = d.Dev
	}
	if d.Passed != nil && !*d.Passed {
		out = append(out, events.Event{Kind: "disk-smart-fail", Level: "critico", Key: "smart-fail:" + id,
			Title: "Disco falhando: " + name, Body: "O próprio disco avisou que está falhando (SMART). Faça backup do que está nele o quanto antes."})
	}
	if d.Reallocated > prev.Reallocated || d.Pending > prev.Pending || d.Uncorrectable > prev.Uncorrectable || d.MediaErrors > prev.MediaErrors {
		out = append(out, events.Event{Kind: "disk-smart-sectors", Level: "aviso",
			Key:   fmt.Sprintf("smart-sectors:%s:%d:%d:%d:%d", id, d.Reallocated, d.Pending, d.Uncorrectable, d.MediaErrors),
			Title: "Setores com problema: " + name,
			Body: fmt.Sprintf("Realocados: %d · pendentes: %d · incorrigíveis: %d. Pode ser o começo de uma falha: confira o backup.",
				d.Reallocated, d.Pending, d.Uncorrectable)})
	}
	return out
}

// Snapshot é o que a interface recebe.
type Snapshot struct {
	Disks     []Disk `json:"disks"`
	CheckedAt int64  `json:"checkedAt"`
	Err       string `json:"err,omitempty"`
}

func (m *Monitor) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := Snapshot{CheckedAt: m.st.CheckedAt, Err: m.st.Err, Disks: []Disk{}}
	for _, d := range m.st.Disks {
		cp := *d
		cp.History = append([]Change(nil), d.History...)
		out.Disks = append(out.Disks, cp)
	}
	sort.Slice(out.Disks, func(i, j int) bool { return out.Disks[i].Dev < out.Disks[j].Dev })
	return out
}

func (m *Monitor) save() {
	if m.File == "" {
		return
	}
	m.mu.Lock()
	b, err := json.Marshal(m.st)
	m.mu.Unlock()
	if err != nil {
		return
	}
	if err := store.WriteFile(m.File, b, 0o600); err != nil {
		slog.Warn("smart: não deu para gravar", "erro", err)
	}
}

func friendly(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "smartmontools"):
		return "o smartmontools não está instalado no servidor: rode sudo apt install smartmontools"
	case strings.Contains(msg, "script do servidor é antigo") || strings.Contains(msg, "pedido inválido"):
		return "o script do servidor é antigo: em Configurações → Energia, copie os comandos de atualização e cole no servidor"
	}
	return msg
}

// ── leitura da saída do smartctl ─────────────────────────────────

type block struct {
	dev     string
	standby bool
	data    *smartJSON
}

type attr struct {
	ID  int `json:"id"`
	Raw struct {
		Value int64 `json:"value"`
	} `json:"raw"`
}

type smartJSON struct {
	Smartctl struct {
		Messages []struct {
			String string `json:"string"`
		} `json:"messages"`
	} `json:"smartctl"`
	Model       string `json:"model_name"`
	Serial      string `json:"serial_number"`
	SmartStatus *struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	Temperature *struct {
		Current *float64 `json:"current"`
	} `json:"temperature"`
	PowerOnTime *struct {
		Hours int64 `json:"hours"`
	} `json:"power_on_time"`
	PowerCycles *int64 `json:"power_cycle_count"`
	ATA         *struct {
		Table []attr `json:"table"`
	} `json:"ata_smart_attributes"`
	NVMe *struct {
		CriticalWarning int64    `json:"critical_warning"`
		Temperature     *float64 `json:"temperature"`
		PercentUsed     *int64   `json:"percentage_used"`
		MediaErrors     int64    `json:"media_errors"`
	} `json:"nvme_smart_health_information_log"`
}

// ParseBlocks separa a saída do script ("=== /dev/sda" + JSON do smartctl) por disco.
func ParseBlocks(out []byte) []block {
	var res []block
	parts := bytes.Split(out, []byte("=== /dev/"))
	for _, p := range parts[1:] {
		nl := bytes.IndexByte(p, '\n')
		if nl < 0 {
			continue
		}
		dev := strings.TrimSpace(string(p[:nl]))
		if dev == "" || strings.ContainsAny(dev, "/ ") || len(dev) > 16 {
			continue
		}
		b := block{dev: dev}
		body := bytes.TrimSpace(p[nl+1:])
		var s smartJSON
		if len(body) > 0 && json.Unmarshal(body, &s) == nil {
			for _, msg := range s.Smartctl.Messages {
				u := strings.ToUpper(msg.String)
				if strings.Contains(u, "STANDBY") || strings.Contains(u, "SLEEP") {
					b.standby = true
				}
			}
			if !b.standby && (s.SmartStatus != nil || s.ATA != nil || s.NVMe != nil || s.Temperature != nil) {
				b.data = &s
			}
		}
		res = append(res, b)
	}
	return res
}

// apply copia os números para o disco. full = leitura completa (-a), senão só temperatura (-A).
func apply(d *Disk, s *smartJSON, now time.Time, full bool) {
	d.Standby = false
	if s.Model != "" {
		d.Model = s.Model
	}
	if s.Serial != "" {
		d.Serial = s.Serial
	}
	var temp *float64
	if s.Temperature != nil && s.Temperature.Current != nil {
		temp = s.Temperature.Current
	}
	if s.NVMe != nil && temp == nil {
		temp = s.NVMe.Temperature
	}
	if s.ATA != nil {
		for _, a := range s.ATA.Table {
			v := a.Raw.Value
			switch a.ID {
			case 5:
				d.Reallocated = v
			case 197:
				d.Pending = v
			case 198:
				d.Uncorrectable = v
			case 199:
				d.CRCErrors = v
			case 9:
				if s.PowerOnTime == nil {
					h := v
					d.PowerOnHours = &h
				}
			case 194, 190:
				if temp == nil {
					t := float64(v & 0xff) // o byte baixo é a temperatura atual
					if t > 0 && t < 100 {
						temp = &t
					}
				}
			}
		}
	}
	if temp != nil && *temp > 0 && *temp < 120 {
		v := *temp
		d.Temp, d.TempAt = &v, now.Unix()
	}
	if s.PowerOnTime != nil {
		h := s.PowerOnTime.Hours
		d.PowerOnHours = &h
	}
	if s.PowerCycles != nil {
		c := *s.PowerCycles
		d.PowerCycles = &c
	}
	if s.NVMe != nil {
		d.MediaErrors = s.NVMe.MediaErrors
		d.PercentUsed = s.NVMe.PercentUsed
	}
	if !full {
		return
	}
	if s.SmartStatus != nil {
		p := s.SmartStatus.Passed
		d.Passed = &p
	}
	if s.NVMe != nil && s.NVMe.CriticalWarning > 0 {
		f := false
		d.Passed = &f
	}
	d.CheckedAt = now.Unix()
	d.Health = HealthOK
	if d.Reallocated > 0 || d.Pending > 0 || d.Uncorrectable > 0 || d.MediaErrors > 0 {
		d.Health = HealthWarn
	}
	if d.Passed != nil && !*d.Passed {
		d.Health = HealthFail
	}
	passed := d.Passed == nil || *d.Passed
	last := len(d.History) - 1
	if last < 0 || d.History[last].Reallocated != d.Reallocated || d.History[last].Pending != d.Pending || d.History[last].Passed != passed {
		d.History = append(d.History, Change{At: now.Unix(), Reallocated: d.Reallocated, Pending: d.Pending, Passed: passed})
		if len(d.History) > maxHist {
			d.History = d.History[len(d.History)-maxHist:]
		}
	}
}
