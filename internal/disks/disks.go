// Package disks é a janela Discos (0.15.0). Junta, para cada disco inteiro do servidor:
//
//   - o que o Linux sabe dele ("discos-detalhe" do script do servidor: lsblk com partições, montagens e
//     espaço, as pastas do /etc/fstab e o caminho fixo em /dev/disk/by-id), inclusive discos sem Pasta
//     de montagem;
//   - a saúde e a temperatura (internal/smart) e a temperatura do drivetemp (internal/sysinfo);
//   - os apps que usam o disco (pastas do servidor montadas nos containers, pelo Docker);
//   - a Linha do tempo: conectado, sumiu, montado, desmontado, SMART mudou, teste, pico de temperatura,
//     velocidade e ejetado, até 200 por disco, em /data/disk-events.json.
//
// O "discos-detalhe" não fala com o disco (só udev e montagens) e é pedido a cada 10 min, quando a lista
// de discos do /proc/diskstats muda (ligou ou desligou um disco) e quando a janela abre. As ações só leem
// ("velocidade") ou desligam um disco USB sem nada montado ("ejetar"); nada apaga nem grava no disco.
package disks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"brunos/internal/events"
	"brunos/internal/files"
	"brunos/internal/hostcmd"
	"brunos/internal/smart"
	"brunos/internal/store"
	"brunos/internal/sysinfo"
)

const (
	refreshEvery = 10 * time.Minute
	oldEvery     = 30 * time.Minute // script do servidor antigo: pergunta menos
	staleAfter   = time.Minute      // a janela aberta pede de novo depois disto
	maxEvents    = 200
	maxKnown     = 40
	forgetAfter  = 180 * 24 * time.Hour
	fileVersion  = 1
)

// Limites de cada pedido (variáveis para os testes encurtarem).
var (
	detailWait = 40 * time.Second
	stateWait  = 25 * time.Second
	errorsWait = 50 * time.Second
	speedWait  = 170 * time.Second
	ejectWait  = 40 * time.Second
	quickWait  = 3 * time.Second
)

// Query faz um pedido ao script do servidor.
type Query func(ctx context.Context, cmd string) ([]byte, error)

// AppMount: um app e as pastas do servidor montadas nos containers dele.
type AppMount struct {
	URN     string
	Name    string
	Sources []string
}

// Event é um acontecimento na Linha do tempo de um disco.
type Event struct {
	At    int64  `json:"at"`
	Kind  string `json:"kind"`            // visto, novo, conectado, sumiu, montado, desmontado, smart, teste, temperatura, velocidade, ejetado
	Level string `json:"level,omitempty"` // info (padrão), aviso
	Text  string `json:"text"`
}

// Speed é o último teste de velocidade de leitura de um disco.
type Speed struct {
	MBps  float64 `json:"mbps"`
	Bytes int64   `json:"bytes"`
	Secs  float64 `json:"secs"`
	At    int64   `json:"at"`
}

type known struct {
	Dev     string   `json:"dev"`
	Name    string   `json:"name"`
	First   int64    `json:"first"`
	Last    int64    `json:"last"` // última vez que apareceu
	Present bool     `json:"present"`
	Mounts  []string `json:"mounts,omitempty"`
	Hot     bool     `json:"hot,omitempty"`
	Ejected int64    `json:"ejected,omitempty"`
	Speed   *Speed   `json:"speed,omitempty"`
	Events  []Event  `json:"events,omitempty"`
}

type state struct {
	Version int               `json:"v"`
	Known   map[string]*known `json:"known"`
}

type Monitor struct {
	File     string
	Query    Query // leituras: discos-detalhe, estado, erros
	Act      Query // ações: velocidade, ejetar
	Smart    func() smart.Snapshot
	SysDisks func() []sysinfo.Disk
	Roots    func() []files.DiskInfo
	Apps     func(ctx context.Context) []AppMount
	Present  func() []string // discos inteiros que o Linux vê agora (/proc/diskstats)
	Events   *events.Store
	Now      func() time.Time

	mu        sync.Mutex
	st        state
	fresh     bool // não havia arquivo: a primeira lista não gera "disco novo"
	list      []Disk
	at        time.Time
	err       string
	old       bool // script do servidor sem o "discos-detalhe"
	loading   bool
	speedDev  string // teste de velocidade rodando (um por vez)
	speedErr  map[string]string
	lastNames string
	base      context.Context
}

func New(file string, q, act Query, ev *events.Store) *Monitor {
	m := &Monitor{File: file, Query: q, Act: act, Events: ev, Now: time.Now, st: state{Version: fileVersion, Known: map[string]*known{}},
		speedErr: map[string]string{}}
	if file != "" {
		ok, err := store.ReadJSON(file, &m.st)
		if err != nil {
			slog.Warn("discos: linha do tempo ilegível, começando do zero", "erro", err)
		}
		m.fresh = !ok || err != nil
	} else {
		m.fresh = true
	}
	if m.st.Known == nil {
		m.st.Known = map[string]*known{}
	}
	return m
}

// PresentFrom lê os nomes dos discos inteiros do /proc/diskstats (sem root e sem falar com o disco).
func PresentFrom(file string) func() []string {
	return func() []string {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil
		}
		var out []string
		for _, line := range strings.Split(string(b), "\n") {
			if f := strings.Fields(line); len(f) > 3 && hostcmd.ValidDisk(f[2]) {
				out = append(out, f[2])
			}
		}
		sort.Strings(out)
		return out
	}
}

// Run pede a lista a cada 10 min e logo que um disco aparece ou some do /proc/diskstats.
func (m *Monitor) Run(ctx context.Context) {
	m.mu.Lock()
	m.base = ctx
	m.mu.Unlock()
	select {
	case <-ctx.Done():
		return
	case <-time.After(25 * time.Second): // deixa o servidor subir
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		names := ""
		if m.Present != nil {
			names = strings.Join(m.Present(), ",")
		}
		m.mu.Lock()
		changed := m.lastNames != "" && names != m.lastNames
		m.lastNames = names
		every := refreshEvery
		if m.old {
			every = oldEvery
		}
		due := m.Now().Sub(m.at) >= every
		m.mu.Unlock()
		if changed || due {
			_ = m.Refresh(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ErrBusy: já há uma leitura (ou um teste de velocidade) em andamento.
var ErrBusy = errors.New("já estou lendo os discos, espere um pouco")

// ErrNoDisk: o disco pedido não está na lista.
var ErrNoDisk = errors.New("esse disco não está na lista (talvez tenha sido desconectado): atualize a janela")

// Refresh pede a lista detalhada agora (uma por vez).
func (m *Monitor) Refresh(ctx context.Context) error {
	m.mu.Lock()
	if m.loading {
		m.mu.Unlock()
		return ErrBusy
	}
	m.loading = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.loading = false; m.mu.Unlock() }()

	qctx, cancel := context.WithTimeout(ctx, detailWait)
	out, err := m.Query(qctx, hostcmd.CmdDiskDetail)
	cancel()
	var list []Disk
	if err == nil {
		list, err = parseDetail(out)
	}
	now := m.Now()
	if err != nil {
		m.mu.Lock()
		m.err, m.old = friendly(err), errors.Is(err, hostcmd.ErrOldScript)
		m.at = now // não insiste até a próxima vez
		msg := m.err
		m.mu.Unlock()
		return errors.New(msg)
	}
	evs := m.observe(list, now)
	m.mu.Lock()
	m.list, m.at, m.err, m.old = list, now, "", false
	m.mu.Unlock()
	evs = append(evs, m.temps(now)...)
	m.emit(evs)
	m.save()
	return nil
}

// start pede a lista em segundo plano (a janela aberta); devolve um canal que fecha quando terminar.
func (m *Monitor) start() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = m.Refresh(m.bg())
	}()
	return done
}

func (m *Monitor) bg() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.base != nil {
		return m.base
	}
	return context.Background()
}

func title(d Disk) string {
	name := d.Model
	if name == "" {
		name = d.Dev
	}
	for _, p := range d.Parts {
		if p.Label != "" {
			return fmt.Sprintf("%s (%s)", p.Label, name)
		}
	}
	return name
}

func (k *known) add(e Event) {
	k.Events = append(k.Events, e)
	if len(k.Events) > maxEvents {
		k.Events = k.Events[len(k.Events)-maxEvents:]
	}
}

func diff(a, b []string) []string {
	in := map[string]bool{}
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if !in[s] {
			out = append(out, s)
		}
	}
	return out
}

func mountName(p string) string {
	if p == "[SWAP]" {
		return "swap"
	}
	return p
}

// observe compara a lista nova com a anterior: discos novos, que voltaram, que sumiram, montagens.
func (m *Monitor) observe(list []Disk, now time.Time) []events.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	var evs []events.Event
	ts := now.Unix()
	first := m.fresh && len(m.st.Known) == 0
	seen := map[string]bool{}
	for _, d := range list {
		seen[d.Key] = true
		mounts := d.Mounts()
		name := title(d)
		k := m.st.Known[d.Key]
		switch {
		case k == nil:
			k = &known{First: ts, Mounts: mounts}
			m.st.Known[d.Key] = k
			if first {
				k.add(Event{At: ts, Kind: "visto", Text: "Visto pela primeira vez pelo BrunOS como /dev/" + d.Dev})
			} else {
				k.add(Event{At: ts, Kind: "novo", Text: "Conectado pela primeira vez como /dev/" + d.Dev})
				evs = append(evs, events.Event{Kind: "disk-new", Level: events.Info, Key: "disk-new:" + d.Key,
					Title: "Disco novo conectado: " + name,
					Body: fmt.Sprintf("Apareceu um disco que o BrunOS nunca tinha visto: %s, %s, %s. Abra a janela Discos para ver os detalhes, a saúde e as partições.",
						name, human(d.Size), tranName(d.Tran))})
			}
		case !k.Present:
			k.add(Event{At: ts, Kind: "conectado", Text: "Conectado de novo como /dev/" + d.Dev})
			if m.Events != nil {
				m.Events.Resolve("disk-gone:"+d.Key, &events.Event{Kind: "disk-back", Level: events.Info,
					Title: "O disco voltou: " + name, Body: "Ele apareceu de novo como /dev/" + d.Dev + "."})
			}
			fallthrough
		default:
			for _, p := range diff(mounts, k.Mounts) {
				k.add(Event{At: ts, Kind: "montado", Text: "Montado em " + mountName(p)})
			}
			for _, p := range diff(k.Mounts, mounts) {
				k.add(Event{At: ts, Kind: "desmontado", Text: "Desmontado de " + mountName(p)})
			}
		}
		k.Dev, k.Name, k.Present, k.Last, k.Mounts = d.Dev, name, true, ts, mounts
	}
	for key, k := range m.st.Known {
		if seen[key] || !k.Present {
			continue
		}
		k.Present = false
		if k.Ejected > 0 && ts-k.Ejected < 600 {
			continue // ejetado pelo BrunOS: já está na Linha do tempo
		}
		k.add(Event{At: ts, Kind: "sumiu", Level: "aviso", Text: "Sumiu da lista de discos (desligado ou desconectado)"})
		evs = append(evs, events.Event{Kind: "disk-gone", Level: events.Aviso, Key: "disk-gone:" + key,
			Title: "Disco sumiu: " + k.Name,
			Body: "O disco que estava em /dev/" + k.Dev + " não aparece mais para o Linux. Se não foi você que desligou, confira o cabo e a fonte do case. " +
				"Se ele estava montado e gravando, o que estava sendo gravado pode ter se perdido."})
	}
	m.prune(now)
	m.fresh = false
	return evs
}

// prune esquece discos que não aparecem há 180 dias e mantém no máximo 40 lembrados.
func (m *Monitor) prune(now time.Time) {
	var gone []string
	for key, k := range m.st.Known {
		if k.Present {
			continue
		}
		if now.Sub(time.Unix(k.Last, 0)) > forgetAfter {
			delete(m.st.Known, key)
			continue
		}
		gone = append(gone, key)
	}
	if extra := len(m.st.Known) - maxKnown; extra > 0 {
		sort.Slice(gone, func(i, j int) bool { return m.st.Known[gone[i]].Last < m.st.Known[gone[j]].Last })
		for _, key := range gone[:min(extra, len(gone))] {
			delete(m.st.Known, key)
		}
	}
}

// hotLimit: HDs sofrem acima de ~55 °C; SSD/NVMe aguentam mais (os mesmos limites do watch.go).
func hotLimit(rotational bool) float64 {
	if rotational {
		return 55
	}
	return 70
}

// temps marca os picos de temperatura na Linha do tempo. Discos sem pasta no Uso ao vivo (o watch.go
// avisa os outros) ganham também o aviso no sino.
func (m *Monitor) temps(now time.Time) []events.Event {
	snap := m.merge(context.Background(), false)
	m.mu.Lock()
	defer m.mu.Unlock()
	var evs []events.Event
	for _, d := range snap.Disks {
		k := m.st.Known[d.Key]
		if k == nil || d.Temp == nil {
			continue
		}
		t, lim := *d.Temp, hotLimit(d.Rotational)
		hasHist := false
		for _, p := range d.Parts {
			hasHist = hasHist || p.Hist != ""
		}
		switch {
		case t >= lim && !k.Hot:
			k.Hot = true
			k.add(Event{At: now.Unix(), Kind: "temperatura", Level: "aviso", Text: fmt.Sprintf("Chegou a %.0f °C (alerta a partir de %.0f °C)", t, lim)})
			if !hasHist {
				evs = append(evs, events.Event{Kind: "disk-hot", Level: events.Aviso, Key: "disk-hot:" + d.Key,
					Title: fmt.Sprintf("O disco %s está quente (%.0f °C)", title(d.Disk), t),
					Body:  fmt.Sprintf("Acima de %.0f °C ele desgasta mais rápido. Veja se a ventilação está livre e se não há poeira.", lim)})
			}
		case t <= lim-5 && k.Hot:
			k.Hot = false
			k.add(Event{At: now.Unix(), Kind: "temperatura", Text: fmt.Sprintf("Esfriou: %.0f °C", t)})
			if !hasHist && m.Events != nil {
				m.Events.Resolve("disk-hot:"+d.Key, nil)
			}
		}
	}
	return evs
}

// Note registra um acontecimento vindo de fora (o SMART mudou, teste do disco).
func (m *Monitor) Note(key, kind, level, text string) {
	m.mu.Lock()
	k := m.st.Known[key]
	if k == nil {
		k = &known{First: m.Now().Unix()}
		m.st.Known[key] = k
	}
	if level == "info" {
		level = ""
	}
	k.add(Event{At: m.Now().Unix(), Kind: kind, Level: level, Text: text})
	m.mu.Unlock()
	m.save()
}

func (m *Monitor) emit(list []events.Event) {
	if m.Events == nil {
		return
	}
	for _, e := range list {
		m.Events.Emit(e)
	}
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
		slog.Warn("discos: não deu para gravar a linha do tempo", "erro", err)
	}
}

// ── resposta para a interface ─────────────────────────────────────

// DiskOut é um disco com tudo o que a janela mostra.
type DiskOut struct {
	Disk
	Smart     *smart.Disk `json:"smart,omitempty"`
	Temp      *float64    `json:"temp,omitempty"`
	TempAt    int64       `json:"tempAt,omitempty"`
	Apps      []AppRef    `json:"apps"`
	Speed     *Speed      `json:"speed,omitempty"`
	SpeedBusy bool        `json:"speedBusy,omitempty"`
	SpeedErr  string      `json:"speedErr,omitempty"`
	First     int64       `json:"first,omitempty"` // quando o BrunOS viu o disco pela primeira vez
}

// AppRef é um app que usa o disco (com a pasta do servidor montada nele).
type AppRef struct {
	URN     string   `json:"urn,omitempty"`
	Name    string   `json:"name"`
	Folders []string `json:"folders"`
}

type Snapshot struct {
	Disks     []DiskOut `json:"disks"`
	CheckedAt int64     `json:"checkedAt"`
	Err       string    `json:"err,omitempty"`
	OldScript bool      `json:"oldScript,omitempty"`
	Loading   bool      `json:"loading,omitempty"`
}

// Snapshot junta a lista com o SMART, as temperaturas, o espaço e os apps.
func (m *Monitor) Snapshot(ctx context.Context) Snapshot { return m.merge(ctx, true) }

func (m *Monitor) merge(ctx context.Context, withApps bool) Snapshot {
	m.mu.Lock()
	list := append([]Disk(nil), m.list...)
	out := Snapshot{CheckedAt: m.at.Unix(), Err: m.err, OldScript: m.old, Loading: m.loading, Disks: []DiskOut{}}
	if m.at.IsZero() {
		out.CheckedAt = 0
	}
	extra := map[string]known{}
	for k, v := range m.st.Known {
		extra[k] = *v
	}
	speedDev, speedErr := m.speedDev, map[string]string{}
	for k, v := range m.speedErr {
		speedErr[k] = v
	}
	m.mu.Unlock()

	var sm smart.Snapshot
	if m.Smart != nil {
		sm = m.Smart()
	}
	byKey, byDev := map[string]*smart.Disk{}, map[string]*smart.Disk{}
	for i := range sm.Disks {
		byKey[sm.Disks[i].Key], byDev[sm.Disks[i].Dev] = &sm.Disks[i], &sm.Disks[i]
	}
	var sys []sysinfo.Disk
	if m.SysDisks != nil {
		sys = m.SysDisks()
	}
	var roots []files.DiskInfo
	if m.Roots != nil {
		roots = m.Roots()
	}
	var apps []AppMount
	if withApps && m.Apps != nil {
		apps = m.Apps(ctx)
	}
	for _, d := range list {
		o := DiskOut{Disk: d, Apps: []AppRef{}}
		o.Parts = append([]Part(nil), d.Parts...)
		if s := byKey[d.Key]; s != nil {
			o.Smart = s
		} else if s := byDev[d.Dev]; s != nil && strings.HasPrefix(d.Key, "dev:") {
			o.Smart = s
		}
		if o.Smart != nil && o.Smart.Temp != nil {
			o.Temp, o.TempAt = o.Smart.Temp, o.Smart.TempAt
		}
		for i := range o.Parts {
			p := &o.Parts[i]
			for _, s := range sys {
				if s.Device == p.Path {
					p.Hist = s.Name
					if s.Temp != nil && (o.Temp == nil || s.TempAt > o.TempAt) {
						o.Temp, o.TempAt = s.Temp, s.TempAt
					}
				}
			}
			for _, mp := range p.Mounts {
				if !strings.HasPrefix(mp, "/mnt/") && !strings.HasPrefix(mp, "/media/") {
					continue
				}
				for _, r := range roots {
					if r.Mounted && r.Name == path.Base(mp) {
						p.Root = r.ID
					}
				}
			}
		}
		o.Apps = appsOn(d, apps)
		if k, ok := extra[d.Key]; ok {
			o.Speed, o.First = k.Speed, k.First
		}
		o.SpeedBusy, o.SpeedErr = speedDev == d.Dev, speedErr[d.Key]
		out.Disks = append(out.Disks, o)
	}
	return out
}

// appsOn: apps com alguma pasta montada dentro das pastas deste disco (a raiz "/" não conta: nela fica
// tudo, inclusive o Runtipi).
func appsOn(d Disk, apps []AppMount) []AppRef {
	out := []AppRef{}
	mounts := []string{}
	for _, mp := range d.Mounts() {
		if strings.HasPrefix(mp, "/") && mp != "/" && !strings.HasPrefix(mp, "/boot") {
			mounts = append(mounts, filepath.Clean(mp))
		}
	}
	for _, a := range apps {
		var folders []string
		for _, src := range a.Sources {
			src = filepath.Clean(src)
			for _, mp := range mounts {
				if src == mp || strings.HasPrefix(src, mp+"/") {
					folders = append(folders, src)
				}
			}
		}
		if len(folders) > 0 {
			out = append(out, AppRef{URN: a.URN, Name: a.Name, Folders: uniq(folders)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Timeline devolve a Linha do tempo de um disco (o mais novo primeiro).
func (m *Monitor) Timeline(key string) []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.st.Known[key]
	if k == nil {
		return []Event{}
	}
	out := make([]Event, len(k.Events))
	for i, e := range k.Events {
		out[len(k.Events)-1-i] = e
	}
	return out
}

func (m *Monitor) find(dev string) (Disk, bool) {
	if !hostcmd.ValidDisk(dev) {
		return Disk{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.list {
		if d.Dev == dev {
			return d, true
		}
	}
	return Disk{}, false
}

// stuckSmart: o adaptador deste disco travou com os pedidos do SMART; o BrunOS não manda outros
// pedidos especiais a ele (o hdparm -C também passa pelo adaptador).
func (m *Monitor) stuckSmart(d Disk) bool {
	if m.Smart == nil {
		return false
	}
	for _, s := range m.Smart().Disks {
		if (s.Key == d.Key || s.Dev == d.Dev) && s.NoSmart && s.NoSmartWhy == smart.WhyStuck {
			return true
		}
	}
	return false
}

// State diz se o disco está dormindo (parado) ou acordado, sem acordá-lo.
func (m *Monitor) State(ctx context.Context, dev string) (string, string, error) {
	d, ok := m.find(dev)
	if !ok {
		return "", "", ErrNoDisk
	}
	if m.stuckSmart(d) {
		return "desconhecido", "o adaptador deste disco travou com pedidos especiais, então o BrunOS não pergunta", nil
	}
	qctx, cancel := context.WithTimeout(ctx, stateWait)
	out, err := m.Query(qctx, hostcmd.CmdDiskState+" "+dev)
	cancel()
	if err != nil {
		return "", "", errors.New(friendly(err))
	}
	f := strings.Fields(string(out))
	if len(f) < 2 || f[0] != "estado" {
		return "desconhecido", "", nil
	}
	why := ""
	if len(f) > 2 && f[2] == "sem-hdparm" {
		why = "instale o hdparm no servidor para ver (sudo apt install hdparm)"
	}
	switch f[1] {
	case "dormindo", "acordado":
		return f[1], why, nil
	}
	return "desconhecido", why, nil
}

// Errors devolve as linhas do registro do Linux sobre o disco nos últimos 30 dias.
func (m *Monitor) Errors(ctx context.Context, dev string) ([]LogLine, error) {
	if _, ok := m.find(dev); !ok {
		return nil, ErrNoDisk
	}
	qctx, cancel := context.WithTimeout(ctx, errorsWait)
	out, err := m.Query(qctx, hostcmd.CmdDiskErrors+" "+dev)
	cancel()
	if err != nil {
		return nil, errors.New(friendly(err))
	}
	return parseErrors(out), nil
}

// StartSpeed começa o teste de velocidade de leitura (1 GiB) em segundo plano; um por vez.
func (m *Monitor) StartSpeed(dev string) error {
	d, ok := m.find(dev)
	if !ok {
		return ErrNoDisk
	}
	if m.Act == nil {
		return errors.New("teste de velocidade indisponível")
	}
	m.mu.Lock()
	if m.speedDev != "" {
		m.mu.Unlock()
		return errors.New("já tem um teste de velocidade rodando: espere terminar")
	}
	m.speedDev = dev
	delete(m.speedErr, d.Key)
	m.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(m.bg(), speedWait)
		out, err := m.Act(ctx, hostcmd.CmdSpeed+" "+dev)
		cancel()
		now := m.Now()
		m.mu.Lock()
		m.speedDev = ""
		k := m.st.Known[d.Key]
		if err == nil {
			if b, secs, ok := parseSpeed(out); ok && k != nil {
				sp := &Speed{MBps: float64(int(float64(b)/secs/1e6*10+0.5)) / 10, Bytes: b, Secs: secs, At: now.Unix()}
				k.Speed = sp
				k.add(Event{At: now.Unix(), Kind: "velocidade", Text: fmt.Sprintf("Teste de velocidade: leitura de %.0f MB/s", sp.MBps)})
			} else if !ok {
				err = errors.New("o servidor respondeu o teste num formato inesperado")
			}
		}
		if err != nil {
			m.speedErr[d.Key] = friendly(err)
		}
		m.mu.Unlock()
		m.save()
	}()
	return nil
}

// Eject desliga com segurança um disco USB sem nada montado (o script confere tudo de novo).
func (m *Monitor) Eject(ctx context.Context, dev string) error {
	d, ok := m.find(dev)
	switch {
	case !ok:
		return ErrNoDisk
	case m.Act == nil:
		return errors.New("ejetar indisponível")
	case d.Tran != "usb":
		return errors.New("só dá para ejetar disco USB")
	case d.System:
		return errors.New("este é o disco do sistema")
	case len(d.Mounts()) > 0:
		return errors.New("o disco ainda está montado em " + strings.Join(d.Mounts(), ", ") + ": desmonte antes de ejetar")
	}
	qctx, cancel := context.WithTimeout(ctx, ejectWait)
	_, err := m.Act(qctx, hostcmd.CmdEject+" "+dev)
	cancel()
	if err != nil {
		return errors.New(friendly(err))
	}
	now := m.Now()
	m.mu.Lock()
	if k := m.st.Known[d.Key]; k != nil {
		k.Ejected = now.Unix()
		k.add(Event{At: now.Unix(), Kind: "ejetado", Text: "Ejetado com segurança pelo BrunOS"})
	}
	m.mu.Unlock()
	m.save()
	go func() { // a lista nova já sai sem ele
		time.Sleep(3 * time.Second)
		_ = m.Refresh(m.bg())
	}()
	return nil
}

func friendly(err error) string {
	msg := err.Error()
	switch {
	case errors.Is(err, hostcmd.ErrOldScript) || strings.Contains(msg, "pedido inválido"):
		return "o script do servidor é antigo: ele se atualiza sozinho em até 1 hora, ou em Configurações → Energia, copie os comandos de atualização e cole no servidor"
	case errors.Is(err, hostcmd.ErrNotConfigured):
		return "a conexão com o servidor ainda não foi configurada: em Configurações → Energia, siga os passos do desligamento seguro"
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "deadline exceeded"):
		return "o servidor demorou demais para responder: tente de novo daqui a pouco"
	case errors.Is(err, context.Canceled):
		return "o pedido foi interrompido"
	}
	return msg
}

// human escreve bytes como "1 TB" (base 1000, igual ao fmtBytes da interface).
func human(b int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	v, i := float64(b), 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	s := strings.Replace(strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0"), ".", ",", 1)
	return s + " " + units[i]
}

func tranName(t string) string {
	switch t {
	case "usb":
		return "ligado por USB"
	case "sata":
		return "SATA (interno)"
	case "nvme":
		return "NVMe (interno)"
	case "":
		return "conexão desconhecida"
	}
	return t
}
