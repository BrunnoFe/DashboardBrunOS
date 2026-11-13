// Package smart acompanha a saúde dos discos pelo SMART (D3, decisões 43 e 46).
//
// O BrunOS não tem root: pede ao script do servidor (brunos-host 5) a lista de discos ("discos", pelo
// lsblk, que não fala com o disco) e depois UM disco por pedido: "smart <disco> <modo>" a cada 6 h e
// "temperatura <disco> <modo>" a cada 20 min, sempre com -n standby (HD parado não é acordado).
//
// Modo: disco USB tenta primeiro "sat" (-d sat, disco SATA atrás do adaptador) e, se o adaptador
// recusar na hora, "auto" (o smartctl decide, e conhece as pontes USB de NVMe). O chip do case não diz
// o que tem dentro: um chip feito para NVMe pode ter um HD SATA dentro, e aí o "auto" trava. O modo que
// funcionou fica guardado pelo número de série (as letras sda, sdb mudam ao reiniciar).
//
// "Basta uma vez": um disco que travou (o adaptador não responde e o kernel reinicia o USB do disco
// montado) ou que não devolveu dados vira "sem SMART" e nunca mais é lido sozinho. Um limite de tempo
// não interrompe o smartctl preso esperando o kernel, então a regra é não insistir. "Tentar de novo"
// (Retry) testa o outro modo.
//
// Selo: OK, Atenção (setores realocados/pendentes/incorrigíveis, erros de mídia no NVMe) ou Falhando
// (o próprio disco diz que falhou). Avisos no sino uma vez por mudança. Estado em /data/smart.json.
package smart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"brunos/internal/events"
	"brunos/internal/hostcmd"
	"brunos/internal/store"
)

const (
	fullEvery = 6 * time.Hour
	tempEvery = 20 * time.Minute
	maxHist   = 30
	maxGone   = 16 // discos que sumiram e continuam lembrados (o "sem SMART" não se perde)
)

// Limites de cada pedido (variáveis só para os testes encurtarem).
var (
	listWait = 30 * time.Second // pedido "discos"
	fullWait = 50 * time.Second // leitura completa de um disco
	tempWait = 30 * time.Second // temperatura de um disco
)

// Selos.
const (
	HealthOK   = "ok"
	HealthWarn = "atencao"
	HealthFail = "falhando"
)

// Por que um disco ficou sem SMART.
const (
	WhyStuck  = "travou"    // o adaptador não respondeu dentro do limite
	WhyNoData = "sem-dados" // respondeu, mas sem nenhum dado do SMART
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
	Key           string   `json:"key"`            // número de série do lsblk (ou "dev:sda" sem número)
	Dev           string   `json:"dev"`            // sda, nvme0n1 (pode mudar ao reiniciar)
	Tran          string   `json:"tran,omitempty"` // usb, sata, nvme
	Mode          string   `json:"mode,omitempty"` // modo que funcionou (sat ou auto)
	NoSmart       bool     `json:"noSmart,omitempty"`
	NoSmartWhy    string   `json:"noSmartWhy,omitempty"`
	NoSmartAt     int64    `json:"noSmartAt,omitempty"`
	FailMode      string   `json:"failMode,omitempty"` // modo que travou (o Retry tenta o outro)
	Force         string   `json:"force,omitempty"`    // modo pedido pelo Retry (uma vez)
	Gone          bool     `json:"gone,omitempty"`     // não apareceu na última lista de discos
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
	// 0.15.0 (janela Discos): todos os atributos da última leitura completa (a interface explica cada
	// um), o teste do próprio disco em andamento e os últimos resultados do registro do disco.
	Attrs     []Attr       `json:"attrs,omitempty"`
	Test      *TestRun     `json:"test,omitempty"`
	Tests     []TestResult `json:"tests,omitempty"`
	ShortMin  int          `json:"shortMin,omitempty"`  // quanto o teste curto leva (o disco diz)
	LongMin   int          `json:"longMin,omitempty"`   // e o longo
	TestAsked int64        `json:"testAsked,omitempty"` // quando o BrunOS pediu o teste em andamento
	TestKind  string       `json:"testKind,omitempty"`  // curto ou longo (o pedido do BrunOS)
	// 0.15.1: o resultado do último teste pedido (nunca "some"), quanto os testes levaram de verdade e se
	// o disco interrompe o teste quando é perguntado no meio (aí o BrunOS só pergunta no fim).
	LastRun   *LastRun `json:"lastRun,omitempty"`
	ShortTook int      `json:"shortTook,omitempty"` // segundos
	LongTook  int      `json:"longTook,omitempty"`
	QuietTest bool     `json:"quietTest,omitempty"`
	TestReads int      `json:"testReads,omitempty"` // leituras feitas durante o teste atual
}

// LastRun é como terminou o último teste pedido pelo BrunOS.
type LastRun struct {
	Kind   string `json:"kind"`   // curto ou longo
	Asked  int64  `json:"asked"`  // quando foi pedido
	Ended  int64  `json:"ended"`  // quando o BrunOS viu que terminou
	Result string `json:"result"` // ok, falhou, interrompido, sem-resultado
	Status string `json:"status,omitempty"`
	Quiet  bool   `json:"quiet,omitempty"` // a partir deste teste o BrunOS só pergunta no fim
}

// Attr é um atributo do SMART. ATA: número, valor normalizado, pior, limite e o valor cru. NVMe: Key é
// o nome do campo do registro de saúde e só Raw vale.
type Attr struct {
	ID      int    `json:"id,omitempty"`
	Key     string `json:"key,omitempty"`
	Name    string `json:"name"`
	Value   int    `json:"value,omitempty"`
	Worst   int    `json:"worst,omitempty"`
	Thresh  int    `json:"thresh,omitempty"`
	Raw     int64  `json:"raw"`
	RawStr  string `json:"rawStr,omitempty"`
	Failed  string `json:"failed,omitempty"` // "now" ou "past": o valor passou do limite
	Prefail bool   `json:"prefail,omitempty"`
}

// TestRun é o teste do próprio disco rodando agora.
type TestRun struct {
	Kind      string `json:"kind,omitempty"` // curto ou longo (quando o BrunOS pediu)
	Remaining int    `json:"remaining"`      // % que falta
}

// TestResult é uma linha do registro de testes do disco (o mais novo primeiro).
type TestResult struct {
	Kind   string `json:"kind"`   // texto do smartctl ("Short offline", "Extended offline", "Short")
	Status string `json:"status"` // "Completed without error", "Aborted by host"...
	Passed bool   `json:"passed"`
	Hours  int64  `json:"hours"` // horas ligado quando terminou
}

const stateVersion = 2

type state struct {
	Version   int              `json:"v"`
	Disks     map[string]*Disk `json:"disks"` // por Key (na versão 1, por Dev)
	CheckedAt int64            `json:"checkedAt"`
	Err       string           `json:"err,omitempty"`
}

// Query faz um pedido ao script do servidor (main: power.HostOutput).
type Query func(ctx context.Context, cmd string) ([]byte, error)

type Monitor struct {
	File    string
	Query   Query
	Act     Query // ações (0.15.0): só o "smart-teste"
	Events  *events.Store
	SetTemp func(dev string, celsius float64, at time.Time) // temperatura para a lista de discos
	// Note registra um acontecimento na Linha do tempo do disco (0.15.0): SMART mudou, teste.
	Note func(key, kind, level, text string)
	Now  func() time.Time

	mu       sync.Mutex
	st       state
	base     context.Context // contexto do Run (leituras em segundo plano)
	lastFull time.Time
	lastTemp time.Time
	testAt   map[string]time.Time // última leitura de cada disco com teste em andamento
	running  bool
}

// testEvery: com um teste do próprio disco em andamento, o andamento é conferido neste intervalo
// (discos que interrompem o teste quando perguntados: só depois do tempo estimado, a cada quietEvery).
var (
	testEvery  = 30 * time.Second
	quietEvery = time.Minute
)

func New(file string, q Query, ev *events.Store) *Monitor {
	m := &Monitor{File: file, Query: q, Events: ev, Now: time.Now, st: state{Version: stateVersion, Disks: map[string]*Disk{}}}
	if file != "" {
		if _, err := store.ReadJSON(file, &m.st); err != nil {
			slog.Warn("smart: estado ilegível, começando do zero", "erro", err)
		}
	}
	if m.st.Disks == nil {
		m.st.Disks = map[string]*Disk{}
	}
	if m.st.Version < stateVersion { // versão 1: chave = nome do disco; vira "dev:<nome>" até a lista dizer o número
		old := m.st.Disks
		m.st.Disks = map[string]*Disk{}
		for dev, d := range old {
			d.Dev, d.Key = dev, "dev:"+dev
			m.st.Disks[d.Key] = d
		}
		m.st.Version = stateVersion
	}
	if m.st.CheckedAt > 0 {
		m.lastFull = time.Unix(m.st.CheckedAt, 0)
	}
	return m
}

// Run confere a cada minuto se é hora de ler (6 h o SMART completo, 20 min a temperatura).
func (m *Monitor) Run(ctx context.Context) {
	m.mu.Lock()
	m.base = ctx
	m.mu.Unlock()
	t := time.NewTicker(10 * time.Second) // cada leitura tem sua hora; o passo curto é para o andamento dos testes
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
		} else if keys := m.testing(now); len(keys) > 0 {
			m.followTests(ctx, keys)
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

// ErrNoDisk: o disco pedido não existe na lista.
var ErrNoDisk = errors.New("esse disco não está na lista")

// begin marca uma leitura em andamento (uma por vez: o mesmo adaptador USB não recebe dois pedidos).
func (m *Monitor) begin() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return false
	}
	m.running = true
	return true
}

func (m *Monitor) end() { m.mu.Lock(); m.running = false; m.mu.Unlock() }

func (m *Monitor) bg() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.base != nil {
		return m.base
	}
	return context.Background()
}

// Start lê em segundo plano ("Ler agora"). O canal recebe o resultado quando terminar.
func (m *Monitor) Start() <-chan error {
	done := make(chan error, 1)
	if !m.begin() {
		done <- ErrBusy
		return done
	}
	go func() { done <- m.refresh(m.bg()) }()
	return done
}

// Refresh lê o SMART completo agora (espera terminar).
func (m *Monitor) Refresh(ctx context.Context) error {
	if !m.begin() {
		return ErrBusy
	}
	return m.refresh(ctx)
}

func (m *Monitor) refresh(ctx context.Context) error {
	defer m.end()
	now := m.Now()
	m.mu.Lock()
	m.lastFull, m.lastTemp = now, now
	m.mu.Unlock()

	keys, err := m.listDisks(ctx, now)
	if err == nil {
		var notify []events.Event
		for _, k := range keys {
			if ctx.Err() != nil {
				err = ctx.Err()
				break
			}
			m.mu.Lock()
			testing := m.st.Disks[k] != nil && m.st.Disks[k].Test != nil
			m.mu.Unlock()
			if testing { // o acompanhamento do teste lê este disco no tempo dele
				continue
			}
			evs, e := m.readFull(ctx, k, now)
			notify = append(notify, evs...)
			if e != nil {
				err = e
				break
			}
		}
		m.emit(notify)
	}
	m.mu.Lock()
	if err != nil {
		m.st.Err = friendly(err)
	} else {
		m.st.Err, m.st.CheckedAt = "", now.Unix()
	}
	msg := m.st.Err
	m.mu.Unlock()
	m.save()
	if err != nil {
		return errors.New(msg)
	}
	return nil
}

// Retry tira o "sem SMART" de um disco e tenta o modo que ainda não foi testado, em segundo plano.
func (m *Monitor) Retry(key string) <-chan error {
	done := make(chan error, 1)
	if !m.begin() {
		done <- ErrBusy
		return done
	}
	m.mu.Lock()
	d := m.st.Disks[key]
	if d == nil || d.Gone {
		m.mu.Unlock()
		m.end()
		done <- ErrNoDisk
		return done
	}
	d.NoSmart, d.NoSmartWhy, d.NoSmartAt, d.Mode = false, "", 0, ""
	d.Force = otherMode(d.FailMode)
	m.mu.Unlock()
	go func() {
		defer m.end()
		ctx := m.bg()
		evs, err := m.readFull(ctx, key, m.Now())
		m.emit(evs)
		m.mu.Lock()
		if err != nil {
			m.st.Err = friendly(err)
		}
		m.mu.Unlock()
		m.save()
		done <- err
	}()
	return done
}

// ErrTestRunning: o disco já está fazendo um teste.
var ErrTestRunning = errors.New("este disco já está fazendo um teste: espere terminar")

// SelfTest pede ao disco para testar a si mesmo (kind = hostcmd.TestShort ou TestLong). O teste roda
// dentro do disco; o andamento aparece nas leituras seguintes (a cada 3 min enquanto durar).
func (m *Monitor) SelfTest(ctx context.Context, key, kind string) error {
	if kind != hostcmd.TestShort && kind != hostcmd.TestLong {
		return errors.New("tipo de teste inválido")
	}
	if m.Act == nil {
		return errors.New("testes indisponíveis")
	}
	if !m.begin() {
		return ErrBusy
	}
	defer m.end()
	m.mu.Lock()
	d := m.st.Disks[key]
	switch {
	case d == nil || d.Gone:
		m.mu.Unlock()
		return ErrNoDisk
	case d.NoSmart || d.Mode == "":
		m.mu.Unlock()
		return errors.New("o BrunOS ainda não conseguiu ler a saúde (SMART) deste disco, então não dá para pedir o teste")
	case d.Test != nil:
		m.mu.Unlock()
		return ErrTestRunning
	}
	cmd := strings.Join([]string{hostcmd.CmdSelfTest, d.Dev, d.Mode, kind}, " ")
	m.mu.Unlock()
	qctx, cancel := context.WithTimeout(ctx, 70*time.Second)
	_, err := m.Act(qctx, cmd)
	cancel()
	if err != nil {
		return errors.New(friendly(err))
	}
	now := m.Now()
	m.mu.Lock()
	d.TestAsked, d.TestKind, d.Test, d.TestReads = now.Unix(), kind, &TestRun{Kind: kind, Remaining: 100}, 0
	if m.testAt == nil {
		m.testAt = map[string]time.Time{}
	}
	m.testAt[key] = now // a primeira conferência fica para daqui a testEvery
	quiet := d.QuietTest
	m.mu.Unlock()
	slog.Info("smart: teste pedido", "disco", d.Dev, "tipo", kind, "soNoFim", quiet)
	m.note(key, "teste", "info", "Teste "+kind+" iniciado pelo BrunOS")
	m.save()
	return nil
}

// estimate: quanto o teste deve levar (o que levou da última vez, senão o que o disco diz).
func estimate(d *Disk) time.Duration {
	took, poll, def := d.ShortTook, d.ShortMin, 2*time.Minute
	if d.TestKind == hostcmd.TestLong {
		took, poll, def = d.LongTook, d.LongMin, 2*time.Hour
	}
	switch {
	case took > 0:
		return time.Duration(took) * time.Second
	case poll > 0:
		return time.Duration(poll) * time.Minute
	}
	return def
}

// testing devolve os discos com teste em andamento que já estão na hora de ser conferidos: a cada 30 s;
// disco que interrompe o teste quando perguntado (QuietTest), só depois do tempo estimado.
func (m *Monitor) testing(now time.Time) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for k, d := range m.st.Disks {
		if d.Test == nil || d.Gone || d.NoSmart {
			continue
		}
		last := m.testAt[k]
		if d.QuietTest {
			if now.Before(time.Unix(d.TestAsked, 0).Add(estimate(d)+15*time.Second)) || now.Sub(last) < quietEvery {
				continue
			}
		} else if now.Sub(last) < testEvery {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// followTests lê de novo os discos com teste em andamento (andamento e, no fim, o resultado).
func (m *Monitor) followTests(ctx context.Context, keys []string) {
	if !m.begin() {
		return
	}
	defer m.end()
	now := m.Now()
	m.mu.Lock()
	if m.testAt == nil {
		m.testAt = map[string]time.Time{}
	}
	for _, k := range keys {
		m.testAt[k] = now
		if d := m.st.Disks[k]; d != nil {
			d.TestReads++
		}
	}
	m.mu.Unlock()
	var notify []events.Event
	for _, k := range keys {
		evs, err := m.readFull(ctx, k, now)
		notify = append(notify, evs...)
		if err != nil {
			break
		}
	}
	m.emit(notify)
	m.save()
}

func (m *Monitor) note(key, kind, level, text string) {
	if m.Note != nil {
		m.Note(key, kind, level, text)
	}
}

func otherMode(failed string) string {
	if failed == hostcmd.ModeSAT {
		return hostcmd.ModeAuto
	}
	return hostcmd.ModeSAT
}

// listed é uma linha do pedido "discos".
type listed struct{ name, tran, serial string }

var pairRe = regexp.MustCompile(`([A-Z]+)="([^"]*)"`)

// parseDisks lê a saída do "lsblk -dnP -o NAME,TRAN,SERIAL".
func parseDisks(out []byte) []listed {
	var res []listed
	for _, line := range strings.Split(string(out), "\n") {
		var l listed
		for _, p := range pairRe.FindAllStringSubmatch(line, -1) {
			switch p[1] {
			case "NAME":
				l.name = p[2]
			case "TRAN":
				l.tran = strings.ToLower(p[2])
			case "SERIAL":
				l.serial = strings.TrimSpace(p[2])
			}
		}
		if hostcmd.ValidDisk(l.name) {
			res = append(res, l)
		}
	}
	return res
}

// listDisks pede a lista e atualiza quem é quem. Devolve as chaves dos discos a ler.
func (m *Monitor) listDisks(ctx context.Context, now time.Time) ([]string, error) {
	qctx, cancel := context.WithTimeout(ctx, listWait)
	out, err := m.Query(qctx, hostcmd.CmdDisks)
	cancel()
	if err != nil {
		return nil, err
	}
	list := parseDisks(out)
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	var keys []string
	for _, l := range list {
		key := l.serial
		if key == "" {
			key = "dev:" + l.name
		}
		d := m.st.Disks[key]
		if d == nil {
			// estado da versão 1 (por nome) ou disco sem número que ganhou um: aproveita o histórico
			if old := m.st.Disks["dev:"+l.name]; old != nil && key != "dev:"+l.name {
				delete(m.st.Disks, "dev:"+l.name)
				d = old
				d.Mode = ""
			} else {
				d = &Disk{}
			}
			d.Key = key
			m.st.Disks[key] = d
		}
		d.Dev, d.Tran, d.Gone = l.name, l.tran, false
		seen[key] = true
		if !d.NoSmart {
			keys = append(keys, key)
		}
	}
	// a mesma letra não pode ficar com dois discos: quem não veio na lista sumiu (desligado, outro case)
	var gone []*Disk
	for k, d := range m.st.Disks {
		if !seen[k] {
			d.Gone = true
			gone = append(gone, d)
		}
	}
	if len(gone) > maxGone {
		sort.Slice(gone, func(i, j int) bool { return gone[i].CheckedAt < gone[j].CheckedAt })
		for _, d := range gone[:len(gone)-maxGone] {
			delete(m.st.Disks, d.Key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return m.st.Disks[keys[i]].Dev < m.st.Disks[keys[j]].Dev })
	return keys, nil
}

// modesFor: o modo pedido pelo Retry, o que já funcionou, ou a ordem de tentativa.
func modesFor(d *Disk) []string {
	switch {
	case d.Force != "":
		return []string{d.Force}
	case d.Mode != "":
		return []string{d.Mode}
	case d.Tran == "usb":
		return []string{hostcmd.ModeSAT, hostcmd.ModeAuto}
	}
	return []string{hostcmd.ModeAuto}
}

// stuck: o pedido estourou o limite dele (e não foi o BrunOS que desligou).
func stuck(err error, parent context.Context) bool {
	return errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil
}

// diskOnly: erro só daquele disco (sumiu entre a lista e o pedido): pula sem parar os outros.
func diskOnly(err error) bool { return strings.Contains(err.Error(), "não existe") }

// readFull lê um disco. Erro devolvido = problema geral (script antigo, sem conexão): para a rodada.
func (m *Monitor) readFull(ctx context.Context, key string, now time.Time) ([]events.Event, error) {
	m.mu.Lock()
	d := m.st.Disks[key]
	if d == nil {
		m.mu.Unlock()
		return nil, nil
	}
	dev, modes := d.Dev, modesFor(d)
	m.mu.Unlock()
	last := modes[len(modes)-1]
	for _, mode := range modes {
		qctx, cancel := context.WithTimeout(ctx, fullWait)
		out, err := m.Query(qctx, hostcmd.DiskCmd(hostcmd.CmdSmart, dev, mode))
		cancel()
		if err != nil {
			if stuck(err, ctx) {
				return m.markNoSmart(key, WhyStuck, mode, now), nil
			}
			if diskOnly(err) {
				return nil, nil
			}
			return nil, err
		}
		r := parseOne(out)
		m.mu.Lock()
		d.Force = ""
		if r.standby {
			d.Standby = true
			m.mu.Unlock()
			return nil, nil
		}
		if r.data == nil {
			m.mu.Unlock()
			last = mode
			continue // o adaptador recusou: tenta o próximo modo
		}
		prev := *d
		d.Mode = mode
		apply(d, r.data, now, true)
		evs := changes(prev, d)
		notes := timeline(prev, d)
		if prev.Test != nil && d.Test == nil { // o teste terminou: o resultado nunca "some"
			ev, note := finishTest(prev, d, now)
			if ev != nil {
				evs = append(evs, *ev)
			}
			notes = append(notes, note)
		}
		if d.Temp != nil && m.SetTemp != nil {
			m.SetTemp(d.Dev, *d.Temp, now)
		}
		m.mu.Unlock()
		for _, n := range notes {
			m.note(key, n[0], n[1], n[2])
		}
		if m.Events != nil { // voltou a funcionar: um próximo "sem SMART" avisa de novo
			m.Events.Resolve("smart-unsupported:"+key, nil)
		}
		return evs, nil
	}
	return m.markNoSmart(key, WhyNoData, last, now), nil
}

// markNoSmart: basta uma vez. O disco não é mais lido sozinho; o aviso explica o que fazer.
func (m *Monitor) markNoSmart(key, why, mode string, now time.Time) []events.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.st.Disks[key]
	if d == nil {
		return nil
	}
	d.NoSmart, d.NoSmartWhy, d.NoSmartAt, d.FailMode, d.Force, d.Mode = true, why, now.Unix(), mode, "", ""
	slog.Warn("smart: disco sem SMART", "disco", d.Dev, "motivo", why, "modo", mode)
	return []events.Event{unsupportedEvent(d)}
}

func unsupportedEvent(d *Disk) events.Event {
	name := d.Dev
	if d.Model != "" {
		name = fmt.Sprintf("%s (%s)", d.Dev, d.Model)
	}
	var b strings.Builder
	if d.NoSmartWhy == WhyStuck {
		b.WriteString("O adaptador deste disco não respondeu ao pedido da saúde do disco (SMART). Quando isso acontece, o Linux costuma reiniciar a conexão USB do disco, o que não faz bem a um disco montado e em uso.")
	} else {
		b.WriteString("O adaptador deste disco respondeu ao pedido da saúde do disco (SMART), mas sem nenhum dado: ele não deixa o SMART passar.")
	}
	b.WriteString(" O disco em si não tem problema nenhum por causa disso: ler e gravar arquivos continua normal. Para não forçar o adaptador, o BrunOS parou de perguntar a este disco (nem a saúde, nem a temperatura).")
	if d.Tran == "usb" {
		b.WriteString(" Isso é comum em cases USB: o chip do case nem sempre aceita os comandos do SMART. O que dá para fazer: em Uso ao vivo, abra os detalhes do disco e use \"Tentar de novo\" (o BrunOS tenta do outro jeito; pode reiniciar a conexão USB do disco por alguns segundos, então faça com o disco parado), ou troque o case por um com chip ASMedia, que costuma funcionar.")
	} else {
		b.WriteString(" Em Uso ao vivo, nos detalhes do disco, \"Tentar de novo\" tenta do outro jeito.")
	}
	return events.Event{Kind: "disk-smart-unsupported", Level: events.Info, Key: "smart-unsupported:" + d.Key,
		Title: "Sem SMART: " + name, Body: b.String()}
}

// readTemps lê só a temperatura dos discos com modo conhecido (discos parados ficam com a última medida).
func (m *Monitor) readTemps(ctx context.Context) {
	if !m.begin() {
		return
	}
	defer m.end()
	now := m.Now()
	type job struct{ key, dev, mode string }
	var jobs []job
	m.mu.Lock()
	m.lastTemp = now
	for k, d := range m.st.Disks {
		if !d.Gone && !d.NoSmart && d.Mode != "" && d.Test == nil { // em teste: só o acompanhamento pergunta
			jobs = append(jobs, job{k, d.Dev, d.Mode})
		}
	}
	m.mu.Unlock()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].dev < jobs[j].dev })
	var notify []events.Event
	for _, j := range jobs {
		qctx, cancel := context.WithTimeout(ctx, tempWait)
		out, err := m.Query(qctx, hostcmd.DiskCmd(hostcmd.CmdTemps, j.dev, j.mode))
		cancel()
		if err != nil {
			if stuck(err, ctx) {
				notify = append(notify, m.markNoSmart(j.key, WhyStuck, j.mode, now)...)
				continue
			}
			if diskOnly(err) {
				continue
			}
			break // sem conexão, script antigo: fica para a próxima
		}
		r := parseOne(out)
		if r.standby || r.data == nil {
			continue
		}
		m.mu.Lock()
		if d := m.st.Disks[j.key]; d != nil {
			apply(d, r.data, now, false)
			if d.Temp != nil && m.SetTemp != nil {
				m.SetTemp(d.Dev, *d.Temp, now)
			}
		}
		m.mu.Unlock()
	}
	m.emit(notify)
	if len(notify) > 0 {
		m.save()
	}
}

func (m *Monitor) emit(list []events.Event) {
	if m.Events == nil {
		return
	}
	for _, e := range list {
		m.Events.Emit(e)
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

// finishTest: o disco parou de mostrar o teste em andamento. Monta o resultado (que fica no LastRun e
// nunca some), aprende quanto o teste levou e marca o disco que interrompe o teste quando é perguntado.
func finishTest(prev Disk, d *Disk, now time.Time) (*events.Event, [3]string) {
	name := d.Dev
	if d.Model != "" {
		name = fmt.Sprintf("%s (%s)", d.Dev, d.Model)
	}
	id := d.Serial
	if id == "" {
		id = d.Dev
	}
	kind := prev.TestKind
	if kind == "" {
		kind = "do disco"
	}
	lr := &LastRun{Kind: prev.TestKind, Asked: prev.TestAsked, Ended: now.Unix(), Result: "sem-resultado"}
	isNew := len(d.Tests) > 0 && (len(prev.Tests) == 0 || d.Tests[0] != prev.Tests[0])
	took := 0
	if prev.TestAsked > 0 {
		took = int(now.Unix() - prev.TestAsked)
	}
	if isNew {
		t := d.Tests[0]
		lr.Status = t.Status
		switch {
		case t.Passed:
			lr.Result = "ok"
		case strings.Contains(strings.ToLower(t.Status), "aborted") || strings.Contains(strings.ToLower(t.Status), "interrupted"):
			lr.Result = "interrompido"
		default:
			lr.Result = "falhou"
		}
	}
	if lr.Result == "ok" && took > 0 {
		if prev.TestKind == hostcmd.TestLong {
			d.LongTook = took
		} else if prev.TestKind == hostcmd.TestShort {
			d.ShortTook = took
		}
	}
	// "interrompido pelo computador" depois de o BrunOS perguntar o andamento: este disco não gosta de ser
	// perguntado durante o teste; dos próximos em diante, só no fim
	if lr.Result == "interrompido" && strings.Contains(strings.ToLower(lr.Status), "host") && prev.TestReads > 0 && !d.QuietTest {
		d.QuietTest, lr.Quiet = true, true
	}
	slog.Info("smart: teste terminou", "disco", d.Dev, "tipo", kind, "resultado", lr.Result, "registro", lr.Status,
		"segundos", took, "leiturasNoMeio", prev.TestReads)
	if prev.TestAsked > 0 {
		d.LastRun = lr
	}
	d.TestAsked, d.TestKind, d.TestReads = 0, "", 0
	mins := ""
	if took >= 60 {
		mins = fmt.Sprintf(" Levou cerca de %d min.", (took+30)/60)
	}
	var ev events.Event
	note := [3]string{"teste", "info", ""}
	switch lr.Result {
	case "ok":
		ev = events.Event{Level: events.Info, Title: "Teste " + kind + " terminou sem erros: " + name, Body: "O próprio disco se testou e não achou problema." + mins}
		note[2] = "Teste " + kind + " terminou sem erros"
	case "falhou":
		ev = events.Event{Level: events.Aviso, Title: "Teste " + kind + " com problema: " + name,
			Body: "O disco terminou o teste com o resultado \"" + lr.Status + "\". Confira o backup do que está nele."}
		note[1], note[2] = "aviso", "Teste "+kind+" com problema: "+lr.Status
	case "interrompido":
		body := "O disco anotou o teste como interrompido (\"" + lr.Status + "\"). Isso acontece quando o disco é desligado, reiniciado ou entra em economia de energia no meio do teste. Pode pedir de novo na janela Discos."
		if lr.Quiet {
			body = "O disco anotou \"" + lr.Status + "\". Alguns discos param o teste quando alguém pergunta o andamento no meio dele. Nos próximos testes deste disco o BrunOS só pergunta quando o tempo estimado acabar. Pode pedir de novo na janela Discos."
		}
		ev = events.Event{Level: events.Aviso, Title: "Teste " + kind + " interrompido: " + name, Body: body}
		note[1], note[2] = "aviso", "Teste "+kind+" interrompido: "+lr.Status
	default:
		ev = events.Event{Level: events.Aviso, Title: "Teste " + kind + " terminou sem resultado: " + name,
			Body: "O disco parou de mostrar o teste em andamento, mas não anotou o resultado no registro dele. Pode pedir de novo; se acontecer sempre, este disco não guarda o resultado dos testes."}
		note[1], note[2] = "aviso", "Teste "+kind+" terminou sem resultado anotado pelo disco"
	}
	if prev.TestAsked == 0 { // teste que não foi o BrunOS que pediu: só a Linha do tempo
		return nil, note
	}
	ev.Kind, ev.Key = "disk-selftest", fmt.Sprintf("smart-test:%s:%d", id, prev.TestAsked)
	return &ev, note
}

// timeline: o que entra na Linha do tempo do disco nesta leitura (tipo, nível, texto).
func timeline(prev Disk, d *Disk) [][3]string {
	var out [][3]string
	label := map[string]string{HealthOK: "OK", HealthWarn: "Atenção", HealthFail: "Falhando"}
	if prev.Health != "" && d.Health != prev.Health {
		lvl := "info"
		if d.Health != HealthOK {
			lvl = "aviso"
		}
		out = append(out, [3]string{"smart", lvl, "Saúde (SMART) mudou de " + label[prev.Health] + " para " + label[d.Health]})
	}
	if d.Reallocated > prev.Reallocated || d.Pending > prev.Pending {
		out = append(out, [3]string{"smart", "aviso", fmt.Sprintf("Setores com problema: %d realocados, %d pendentes", d.Reallocated, d.Pending)})
	}
	return out // o fim de um teste é anotado pelo finishTest
}

// Snapshot é o que a interface recebe.
type Snapshot struct {
	Disks     []Disk `json:"disks"`
	CheckedAt int64  `json:"checkedAt"`
	Err       string `json:"err,omitempty"`
	Running   bool   `json:"running,omitempty"`
}

func (m *Monitor) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := Snapshot{CheckedAt: m.st.CheckedAt, Err: m.st.Err, Running: m.running, Disks: []Disk{}}
	for _, d := range m.st.Disks {
		if d.Gone {
			continue
		}
		cp := *d
		cp.History = append([]Change(nil), d.History...)
		cp.Attrs = append([]Attr(nil), d.Attrs...)
		cp.Tests = append([]TestResult(nil), d.Tests...)
		if d.Test != nil {
			t := *d.Test
			cp.Test = &t
		}
		if d.LastRun != nil {
			lr := *d.LastRun
			cp.LastRun = &lr
		}
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
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "deadline exceeded"):
		return "o servidor demorou demais para responder: tente de novo daqui a pouco"
	case errors.Is(err, context.Canceled):
		return "a leitura foi interrompida (o BrunOS reiniciou): fica para a próxima"
	}
	return msg
}

// ── leitura da saída do smartctl ─────────────────────────────────

type block struct {
	standby bool
	data    *smartJSON
}

type attr struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Value      int    `json:"value"`
	Worst      int    `json:"worst"`
	Thresh     int    `json:"thresh"`
	WhenFailed string `json:"when_failed"`
	Flags      struct {
		Prefailure bool `json:"prefailure"`
	} `json:"flags"`
	Raw struct {
		Value  int64  `json:"value"`
		String string `json:"string"`
	} `json:"raw"`
}

type testStatus struct {
	Value     int    `json:"value"`
	String    string `json:"string"`
	Remaining *int   `json:"remaining_percent"`
	Passed    *bool  `json:"passed"`
}

type named struct {
	Value  int    `json:"value"`
	String string `json:"string"`
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
	// o registro de saúde do NVMe inteiro, campo a campo (para a interface explicar)
	NVMeRaw map[string]json.RawMessage `json:"-"`
	// 0.15.0: andamento do teste (ATA) e o registro dos testes
	ATAData *struct {
		SelfTest *struct {
			Status  testStatus `json:"status"`
			Polling struct {
				Short    int `json:"short"`
				Extended int `json:"extended"`
			} `json:"polling_minutes"`
		} `json:"self_test"`
	} `json:"ata_smart_data"`
	ATALog *struct {
		Standard *struct {
			Table []struct {
				Type   named      `json:"type"`
				Status testStatus `json:"status"`
				Hours  int64      `json:"lifetime_hours"`
			} `json:"table"`
		} `json:"standard"`
	} `json:"ata_smart_self_test_log"`
	NVMeLog *struct {
		Current    named `json:"current_self_test_operation"`
		Completion *int  `json:"current_self_test_completion_percent"`
		Table      []struct {
			Code   named `json:"self_test_code"`
			Result named `json:"self_test_result"`
			Hours  int64 `json:"power_on_hours"`
		} `json:"table"`
	} `json:"nvme_self_test_log"`
}

// Campos do registro de saúde do NVMe que viram atributos (na ordem em que aparecem).
var nvmeFields = []string{"critical_warning", "temperature", "available_spare", "available_spare_threshold", "percentage_used",
	"data_units_read", "data_units_written", "host_reads", "host_writes", "controller_busy_time", "power_cycles",
	"power_on_hours", "unsafe_shutdowns", "media_errors", "num_err_log_entries"}

// parseOne lê o JSON do smartctl de um disco: parado, com dados, ou nada (o adaptador recusou).
func parseOne(out []byte) block {
	var b block
	var s smartJSON
	if json.Unmarshal(out, &s) != nil {
		return b
	}
	var raw struct {
		Log map[string]json.RawMessage `json:"nvme_smart_health_information_log"`
	}
	if json.Unmarshal(out, &raw) == nil {
		s.NVMeRaw = raw.Log
	}
	for _, msg := range s.Smartctl.Messages {
		u := strings.ToUpper(msg.String)
		if strings.Contains(u, "STANDBY") || strings.Contains(u, "SLEEP") {
			b.standby = true
		}
	}
	if !b.standby && (s.SmartStatus != nil || s.ATA != nil || s.NVMe != nil || s.Temperature != nil || s.ATAData != nil || s.NVMeLog != nil) {
		b.data = &s
	}
	return b
}

// attrsOf junta os atributos ATA (tabela) e os campos do registro de saúde do NVMe.
func attrsOf(s *smartJSON) []Attr {
	var out []Attr
	if s.ATA != nil {
		for _, a := range s.ATA.Table {
			if len(out) >= 64 {
				break
			}
			failed := ""
			switch w := strings.ToLower(a.WhenFailed); {
			case strings.Contains(w, "now"):
				failed = "now"
			case strings.Contains(w, "past"):
				failed = "past"
			}
			rs := a.Raw.String
			if rs == strconv.FormatInt(a.Raw.Value, 10) || len(rs) > 40 {
				rs = ""
			}
			out = append(out, Attr{ID: a.ID, Name: a.Name, Value: a.Value, Worst: a.Worst, Thresh: a.Thresh, Raw: a.Raw.Value,
				RawStr: rs, Failed: failed, Prefail: a.Flags.Prefailure})
		}
	}
	for _, k := range nvmeFields {
		v, ok := s.NVMeRaw[k]
		if !ok {
			continue
		}
		var n float64
		if json.Unmarshal(v, &n) != nil {
			continue
		}
		out = append(out, Attr{Key: k, Name: k, Raw: int64(n)})
	}
	return out
}

// testsOf lê o andamento do teste do próprio disco e os últimos resultados (até 5).
func testsOf(d *Disk, s *smartJSON, now time.Time) {
	var run *TestRun
	var res []TestResult
	known, logSeen := false, false
	if s.ATAData != nil && s.ATAData.SelfTest != nil {
		known = true
		st := s.ATAData.SelfTest
		d.ShortMin, d.LongMin = st.Polling.Short, st.Polling.Extended
		if st.Status.Value>>4 == 0xF { // 0xF_: teste em andamento
			rem := 0
			if st.Status.Remaining != nil {
				rem = *st.Status.Remaining
			}
			run = &TestRun{Remaining: rem}
		}
	}
	if s.ATALog != nil && s.ATALog.Standard != nil {
		known, logSeen = true, true
		for _, t := range s.ATALog.Standard.Table {
			if len(res) >= 5 {
				break
			}
			if t.Status.Value>>4 == 0xF {
				continue // linha do teste que ainda está rodando
			}
			res = append(res, TestResult{Kind: t.Type.String, Status: t.Status.String, Passed: t.Status.Passed != nil && *t.Status.Passed, Hours: t.Hours})
		}
	}
	if s.NVMeLog != nil {
		known, logSeen = true, true
		if s.NVMeLog.Current.Value != 0 {
			rem := 100
			if c := s.NVMeLog.Completion; c != nil {
				rem = 100 - *c
			}
			run = &TestRun{Remaining: rem}
		}
		for _, t := range s.NVMeLog.Table {
			if len(res) >= 5 {
				break
			}
			if t.Result.Value == 0xF { // linha vazia do registro
				continue
			}
			res = append(res, TestResult{Kind: t.Code.String, Status: t.Result.String, Passed: t.Result.Value == 0, Hours: t.Hours})
		}
	}
	if !known {
		// o adaptador não mostrou o andamento: um teste pedido não fica "rodando" para sempre
		if d.Test != nil && now.Unix()-d.TestAsked > 6*3600 {
			d.Test = nil
		}
		return
	}
	if run != nil {
		run.Kind = d.TestKind
	}
	d.Test = run
	if logSeen { // leitura sem o registro (o adaptador não mandou) não apaga os resultados conhecidos
		d.Tests = res
	}
}

// apply copia os números para o disco. full = leitura completa (-H -i -A), senão só temperatura (-A).
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
	if a := attrsOf(s); len(a) > 0 {
		d.Attrs = a
	}
	testsOf(d, s, now)
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
