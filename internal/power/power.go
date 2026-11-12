// Package power cuida da energia do servidor (um notebook: a bateria é o nobreak).
//
//   - Registra as quedas de energia (tomada → bateria → tomada) em /data/power-events.json,
//     com duração e quanto da bateria foi gasto, e guarda o último consumo medido na bateria
//     (assim a autonomia estimada não se perde quando o BrunOS reinicia).
//   - Desligamento seguro: fora da tomada e com a bateria no limite configurado, avisa,
//     espera o tempo de folga (dá para cancelar) e desliga o servidor por SSH com a chave
//     restrita (pacote hostcmd). Começa em modo simulação: só registra o que faria.
package power

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"brunos/internal/events"
	"brunos/internal/hostcmd"
	"brunos/internal/store"
)

const (
	maxOutages    = 100
	seenSaveEvery = time.Minute // na bateria: grava "ainda ligado" (e o consumo) a cada minuto
	retryAfter    = time.Minute
	notifyEvery   = 10 // depois da 1ª falha, avisa de novo a cada 10 tentativas
	runTimeout    = 30 * time.Second

	keyOutage   = "power-outage"
	keyShutdown = "power-shutdown"
	keySetupOld = "power-setup-old"
)

// Fases do desligamento seguro.
const (
	PhaseOff       = "off"       // desligamento seguro desativado
	PhaseArmed     = "armed"     // vigiando a bateria
	PhaseCountdown = "countdown" // contagem regressiva para desligar
	PhaseRunning   = "running"   // mandando desligar agora
	PhaseDone      = "done"      // já desligou (ou simulou) nesta queda
	PhaseCancelled = "cancelled" // cancelado nesta queda; volta a vigiar quando a energia voltar
	PhaseFailed    = "failed"    // sem como desligar (nenhum executor configurado)
)

type Config struct {
	Enabled   bool   `json:"enabled"`
	Threshold int    `json:"threshold"` // % da bateria
	GraceSec  int    `json:"graceSec"`  // espera antes de desligar
	Simulate  bool   `json:"simulate"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	TestedAt  int64  `json:"testedAt"` // último teste de conexão que passou (definido pelo servidor)
	// HostSetup é a versão dos comandos colados no servidor (hostcmd.SetupVersion). Definido
	// pelo servidor: no primeiro teste de conexão ou quando o usuário confirma que atualizou.
	HostSetup int `json:"hostSetup"`
	// Limite de carga desejado (definido pelo servidor em /api/power/charge): o BrunOS confere
	// e reaplica se o notebook esquecer (alguns zeram ao reiniciar). 0 / "" = não mexe.
	ChargeLimit   int    `json:"chargeLimit"`   // 50..95 (limite contínuo)
	ChargeProtect string `json:"chargeProtect"` // "on" | "off" (proteção liga/desliga)
}

// NeedsUpdate diz se o servidor foi configurado com um comando antigo que precisa ser trocado.
func (c Config) NeedsUpdate() bool { return c.TestedAt > 0 && c.HostSetup < hostcmd.SetupVersion }

func DefaultConfig() Config {
	return Config{Threshold: 20, GraceSec: 120, Simulate: true, Port: 22, User: hostcmd.DefaultUser}
}

func (c Config) Target() hostcmd.Target {
	return hostcmd.Target{Host: c.Host, Port: c.Port, User: c.User}
}

type Outage struct {
	Start    int64  `json:"start"`
	End      int64  `json:"end"` // 0 = ainda na bateria
	StartPct int    `json:"startPct"`
	EndPct   int    `json:"endPct"`
	MinPct   int    `json:"minPct"`
	Shutdown string `json:"shutdown,omitempty"` // simulado | executado | cancelado | falhou
	// Seen é a última vez que o BrunOS estava ligado durante a queda (gravado a cada minuto).
	// Se o servidor apagar, a queda fecha nesse horário, e não quando ele ligar de novo.
	Seen int64 `json:"seen,omitempty"`
	Off  bool  `json:"off,omitempty"` // o servidor apagou durante a queda (a bateria acabou)
}

type stateFile struct {
	Version int      `json:"version"`
	DrainW  float64  `json:"drainW"`
	Outages []Outage `json:"outages"` // mais antiga primeiro
}

type Status struct {
	Phase      string `json:"phase"`
	Deadline   int64  `json:"deadline,omitempty"`
	Remaining  int    `json:"remaining,omitempty"` // segundos até desligar (pelo relógio do servidor)
	HasBattery bool   `json:"hasBattery"`
	OnBattery  bool   `json:"onBattery"`
	Percent    int    `json:"percent"`
	Simulate   bool   `json:"simulate"`
	LastResult string `json:"lastResult,omitempty"`
}

type Monitor struct {
	ConfigFile string
	StateFile  string
	Events     *events.Store
	Now        func() time.Time
	// Shutdown desliga o servidor de verdade. Padrão: hostcmd.Runner.Run.
	Shutdown func(ctx context.Context, cfg Config) (string, error)
	// SuggestHost sugere o endereço do servidor (IP do Runtipi) quando ainda não há um.
	SuggestHost func() string

	// HostRun faz um pedido ao script do servidor (limite de carga). Padrão: hostcmd.Runner.Run.
	HostRun func(ctx context.Context, cfg Config, cmd string) (string, error)
	// SysDir é onde ler o /sys (testes usam uma pasta falsa).
	SysDir string

	runner     *hostcmd.Runner
	charge     chargeCap
	chargeBusy bool
	chargeTry  time.Time
	mu         sync.Mutex
	cfg        Config
	st         stateFile
	onBat      bool
	resumed    bool // abriu com uma queda em aberto no arquivo
	seen       bool
	percent    int
	phase      string
	deadline   time.Time
	attempts   int
	lastSave   time.Time
	lastText   string
	execMu     sync.Mutex // um desligamento por vez
}

// ErrHostNotReady: a conexão SSH com o servidor ainda não foi configurada e testada.
var ErrHostNotReady = errors.New("configure e teste a conexão com o servidor em Configurações → Energia (o Tailscale usa a mesma conexão)")

// HostOutput faz um pedido de leitura ao script do servidor (ex.: hostcmd.CmdTailscale), pela mesma
// conexão do desligamento seguro. Só com a configuração nova (chave atual) já testada.
func (m *Monitor) HostOutput(ctx context.Context, cmd string) ([]byte, error) {
	return m.HostOutputInput(ctx, cmd, nil)
}

// HostOutputInput é o HostOutput mandando input pela entrada padrão (o script novo no "atualizar-script").
func (m *Monitor) HostOutputInput(ctx context.Context, cmd string, input []byte) ([]byte, error) {
	c := m.Config()
	if m.runner == nil || c.Host == "" || c.TestedAt == 0 || c.HostSetup < hostcmd.SetupVersion {
		return nil, ErrHostNotReady
	}
	return m.runner.OutputInput(ctx, c.Target(), cmd, input)
}

// New lê a configuração e o histórico. Arquivos vazios = só em memória (testes).
func New(configFile, stateFile string, ev *events.Store, runner *hostcmd.Runner) *Monitor {
	m := &Monitor{ConfigFile: configFile, StateFile: stateFile, Events: ev, Now: time.Now, cfg: DefaultConfig(), runner: runner, SysDir: "/sys"}
	if runner != nil {
		m.HostRun = func(ctx context.Context, c Config, cmd string) (string, error) {
			return runner.Run(ctx, c.Target(), cmd, false)
		}
		// servidor ainda não atualizado: usa a chave antiga (lá, qualquer sessão desliga)
		m.Shutdown = func(ctx context.Context, c Config) (string, error) {
			return runner.Run(ctx, c.Target(), hostcmd.CmdShutdown, c.HostSetup < hostcmd.SetupVersion)
		}
	}
	if configFile != "" {
		if _, err := store.ReadJSON(configFile, &m.cfg); err != nil {
			slog.Warn("energia: configuração ilegível, usando o padrão", "erro", err)
			m.cfg = DefaultConfig()
		}
	}
	if stateFile != "" {
		if _, err := store.ReadJSON(stateFile, &m.st); err != nil {
			slog.Warn("energia: histórico ilegível, começando do zero", "erro", err)
		}
	}
	if n := len(m.st.Outages); n > 0 && m.st.Outages[n-1].End == 0 {
		m.onBat = true   // o BrunOS reiniciou no meio de uma queda
		m.resumed = true // ...ou o servidor apagou e ligou de novo (decide na 1ª leitura)
	}
	m.phase = PhaseOff
	if m.cfg.Enabled {
		m.phase = PhaseArmed
	}
	if m.cfg.Enabled && !m.cfg.Simulate && m.cfg.NeedsUpdate() {
		m.emitLocked(events.Event{Kind: "power-setup-old", Level: events.Aviso, Key: keySetupOld,
			Title: "Atualize a configuração do desligamento",
			Body:  "O Ubuntu passou a recusar o comando de desligar que o BrunOS usava. Abra Configurações → Energia, cole os comandos novos no servidor e clique em Testar."})
	}
	return m
}

// DrainW é o último consumo medido na bateria (W), para estimar a autonomia na tomada.
func (m *Monitor) DrainW() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.DrainW
}

// Reading é uma leitura da bateria (vem do sysinfo; separado para testar sem /sys).
type Reading struct {
	Percent     int
	OnBattery   bool    // fora da tomada
	PowerW      float64 // consumo (média móvel)
	EnergyWh    float64 // carga atual
	MinutesLeft *int
}

func fmtMin(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	if m%60 == 0 {
		return fmt.Sprintf("%d h", m/60)
	}
	return fmt.Sprintf("%d h %02d min", m/60, m%60)
}

// Run vigia a bateria a cada 5 s até ctx acabar. latest devolve a leitura mais recente.
// Detecta o limite de carga uma vez e confere a cada 10 minutos (nada, se não houver).
func (m *Monitor) Run(ctx context.Context, latest func() *Reading) {
	m.DetectCharge()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	every := int(chargeCheckEvery / (5 * time.Second))
	for i := 1; ; i++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Step(latest())
			if i%every == 0 {
				m.checkCharge()
			}
		}
	}
}

// Step avalia uma leitura da bateria. Exportado para os testes.
func (m *Monitor) Step(b *Reading) {
	if b == nil {
		return
	}
	m.mu.Lock()
	now := m.Now()
	on, pct := b.OnBattery, b.Percent
	m.seen, m.percent = true, pct

	// ── quedas de energia ──
	if on && !m.onBat {
		m.st.Outages = append(m.st.Outages, Outage{Start: now.Unix(), StartPct: pct, EndPct: pct, MinPct: pct})
		if len(m.st.Outages) > maxOutages {
			m.st.Outages = append([]Outage(nil), m.st.Outages[len(m.st.Outages)-maxOutages:]...)
		}
		m.saveStateLocked()
		body := fmt.Sprintf("O servidor está funcionando na bateria (%d%%).", pct)
		if b.MinutesLeft != nil {
			body += fmt.Sprintf(" Autonomia estimada: %s.", fmtMin(time.Duration(*b.MinutesLeft)*time.Minute))
		} else if m.st.DrainW > 0 && b.EnergyWh > 0 {
			body += fmt.Sprintf(" Autonomia estimada: %s.", fmtMin(time.Duration(b.EnergyWh/m.st.DrainW*60)*time.Minute))
		}
		m.emitLocked(events.Event{Kind: "power-outage", Level: events.Aviso, Title: "A energia caiu", Body: body, Key: keyOutage})
	}
	if on {
		if n := len(m.st.Outages); n > 0 {
			o := &m.st.Outages[n-1]
			o.EndPct, o.Seen = pct, now.Unix()
			if pct < o.MinPct {
				o.MinPct = pct
			}
		}
		if b.PowerW > 0.3 {
			m.st.DrainW = b.PowerW
		}
		// grava a cada minuto: se a bateria acabar, a queda fecha no último minuto ligado
		if now.Sub(m.lastSave) >= seenSaveEvery {
			m.saveStateLocked()
		}
	}
	if !on && m.onBat {
		var start Outage
		off := false
		if n := len(m.st.Outages); n > 0 {
			o := &m.st.Outages[n-1]
			// abriu com a queda em aberto e já está na tomada: o servidor apagou no meio dela
			if m.resumed && o.Seen > 0 && now.Unix()-o.Seen > 3*60 {
				o.End, o.Off, off = o.Seen, true, true
			} else {
				o.End, o.EndPct = now.Unix(), pct
			}
			start = *o
		}
		m.saveStateLocked()
		body := "O servidor voltou para a tomada."
		switch {
		case off:
			body = fmt.Sprintf("O servidor apagou com a bateria em %d%%, depois de %s na bateria, e ligou de novo quando a energia voltou.",
				start.EndPct, fmtMin(time.Unix(start.End, 0).Sub(time.Unix(start.Start, 0))))
		case start.Start > 0:
			used := start.StartPct - start.MinPct
			body = fmt.Sprintf("Ficou %s na bateria e gastou %d%% da carga.", fmtMin(now.Sub(time.Unix(start.Start, 0))), max(used, 0))
		}
		m.resolveLocked(keyOutage, &events.Event{Kind: "power-restored", Level: events.Info, Title: "A energia voltou", Body: body})
	}
	m.onBat, m.resumed = on, false

	// ── desligamento seguro ──
	if !m.cfg.Enabled {
		if m.phase == PhaseCountdown {
			m.resolveLocked(keyShutdown, &events.Event{Kind: "power-shutdown-cancelled", Level: events.Info, Title: "Desligamento cancelado", Body: "O desligamento seguro foi desativado."})
		}
		if m.phase != PhaseRunning {
			m.phase = PhaseOff
		}
		m.mu.Unlock()
		return
	}
	if !on {
		switch m.phase {
		case PhaseCountdown:
			m.resolveLocked(keyShutdown, &events.Event{Kind: "power-shutdown-cancelled", Level: events.Info, Title: "Desligamento cancelado", Body: "A energia voltou antes do fim da contagem."})
			m.phase = PhaseArmed
		case PhaseOff, PhaseDone, PhaseCancelled, PhaseFailed:
			m.phase = PhaseArmed
			m.resolveLocked(keyShutdown, nil)
		}
		m.attempts = 0
		m.mu.Unlock()
		return
	}
	if m.phase == PhaseOff {
		m.phase = PhaseArmed
	}
	// Bateria velha ou mal calibrada pode pular de 31% para 6% de uma vez (aconteceu em
	// 07/11/2026). Abaixo da metade do limite (ou de 5%) não dá tempo de esperar: desliga já.
	critical := pct <= m.cfg.Threshold/2 || pct <= 5
	if m.phase == PhaseArmed && pct <= m.cfg.Threshold {
		grace := time.Duration(m.cfg.GraceSec) * time.Second
		if critical {
			grace = 0
		}
		m.phase, m.deadline, m.attempts = PhaseCountdown, now.Add(grace), 0
		title := fmt.Sprintf("O servidor vai desligar em %s", fmtMin(grace))
		body := fmt.Sprintf("A bateria chegou a %d%% sem energia da tomada. Para cancelar: Configurações → Energia.", pct)
		if critical {
			title = "O servidor vai desligar agora"
			body = fmt.Sprintf("A bateria caiu de repente para %d%% sem energia da tomada: não dá tempo de esperar.", pct)
		}
		if m.cfg.Simulate {
			title = fmt.Sprintf("Simulação: o servidor desligaria em %s", fmtMin(grace))
			body = fmt.Sprintf("A bateria chegou a %d%% sem energia da tomada. No modo simulação nada é desligado de verdade.", pct)
		}
		m.emitLocked(events.Event{Kind: "power-shutdown", Level: events.Critico, Title: title, Body: body, Key: keyShutdown,
			Meta: map[string]any{"deadline": m.deadline.Unix(), "simulate": m.cfg.Simulate}})
	} else if m.phase == PhaseCountdown && critical && m.attempts == 0 && now.Before(m.deadline) {
		m.deadline = now // durante a contagem a bateria despencou: não espera o resto
	}
	if m.phase == PhaseCountdown && !now.Before(m.deadline) {
		m.executeLocked(now)
	}
	m.mu.Unlock()
}

// executeLocked desliga (ou simula). Solta o mutex durante a conexão SSH.
func (m *Monitor) executeLocked(now time.Time) {
	cfg := m.cfg
	if cfg.Simulate {
		m.phase = PhaseDone
		m.markOutageLocked("simulado")
		m.lastText = "simulado"
		m.resolveLocked(keyShutdown, &events.Event{Kind: "power-shutdown-simulated", Level: events.Info, Title: "Simulação: o servidor teria desligado agora",
			Body: "Tudo funcionou. Quando quiser que desligue de verdade, desative o modo simulação em Configurações → Energia."})
		return
	}
	if m.Shutdown == nil {
		m.phase = PhaseFailed
		m.markOutageLocked("falhou")
		return
	}
	m.phase = PhaseRunning
	m.attempts++
	attempt := m.attempts
	m.mu.Unlock()
	m.execMu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	out, err := m.Shutdown(ctx, cfg)
	cancel()
	m.execMu.Unlock()
	m.mu.Lock()
	now = m.Now()
	if m.phase != PhaseRunning { // a energia voltou ou desativaram enquanto conectava
		return
	}
	if err == nil {
		slog.Warn("energia: desligamento enviado ao servidor", "saida", out)
		m.phase = PhaseDone
		m.lastText = "executado"
		m.markOutageLocked("executado")
		m.resolveLocked(keyShutdown, &events.Event{Kind: "power-shutdown-sent", Level: events.Critico, Title: "Desligando o servidor",
			Body: "O comando de desligar foi enviado. O servidor desliga em 1 minuto (para cancelar no servidor: sudo shutdown -c)."})
		return
	}
	slog.Error("energia: não consegui desligar o servidor", "tentativa", attempt, "erro", err)
	msg := friendlyError(err)
	m.lastText = msg
	if attempt == 1 {
		m.markOutageLocked("falhou")
	}
	// Nunca desiste enquanto estiver na bateria: tenta de novo a cada minuto. Avisa na 1ª falha
	// e depois a cada 10 tentativas, para não encher o sino.
	m.phase, m.deadline = PhaseCountdown, now.Add(retryAfter)
	if attempt == 1 || attempt%notifyEvery == 0 {
		m.emitLocked(events.Event{Kind: "power-shutdown-failed", Level: events.Critico, Title: "Não consegui desligar o servidor",
			Body: fmt.Sprintf("%s Continuo tentando a cada minuto enquanto estiver na bateria (tentativa %d).", msg, attempt)})
	}
}

// friendlyError traduz as falhas mais comuns do desligamento para o português.
func friendlyError(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "inhibitor"):
		return "O Ubuntu recusou desligar porque um programa (as atualizações automáticas, por exemplo) está segurando o desligamento. Atualize a configuração do servidor em Configurações → Energia."
	case strings.Contains(s, "a password is required") || strings.Contains(s, "sudo:"):
		return "O sudo do servidor não liberou o comando de desligar. Cole de novo os comandos de Configurações → Energia."
	}
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

func (m *Monitor) markOutageLocked(result string) {
	if n := len(m.st.Outages); n > 0 && m.st.Outages[n-1].End == 0 {
		m.st.Outages[n-1].Shutdown = result
		m.saveStateLocked()
	}
}

func (m *Monitor) emitLocked(e events.Event) {
	if m.Events != nil {
		m.Events.Emit(e)
	}
}

func (m *Monitor) resolveLocked(key string, e *events.Event) {
	if m.Events != nil {
		m.Events.Resolve(key, e)
	}
}

func (m *Monitor) saveStateLocked() {
	m.lastSave = m.Now()
	if m.StateFile == "" {
		return
	}
	m.st.Version = 1
	if err := store.WriteJSON(m.StateFile, m.st, 0o600); err != nil {
		slog.Error("energia: não foi possível gravar o histórico", "erro", err)
	}
}

// Cancel cancela a contagem regressiva desta queda de energia.
func (m *Monitor) Cancel() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.phase != PhaseCountdown {
		return errors.New("não há desligamento em andamento")
	}
	m.phase = PhaseCancelled
	m.markOutageLocked("cancelado")
	m.resolveLocked(keyShutdown, &events.Event{Kind: "power-shutdown-cancelled", Level: events.Info, Title: "Desligamento cancelado",
		Body: "Você cancelou o desligamento. O BrunOS volta a vigiar a bateria na próxima queda de energia."})
	return nil
}

func (m *Monitor) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{Phase: m.phase, HasBattery: m.seen, OnBattery: m.onBat, Percent: m.percent, Simulate: m.cfg.Simulate, LastResult: m.lastText}
	if m.phase == PhaseCountdown {
		s.Deadline = m.deadline.Unix()
		s.Remaining = max(0, int(m.deadline.Sub(m.Now()).Seconds()+0.5))
	}
	return s
}

func (m *Monitor) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Outages devolve as quedas de energia da mais nova para a mais antiga.
func (m *Monitor) Outages() []Outage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Outage, len(m.st.Outages))
	for i, o := range m.st.Outages {
		out[len(out)-1-i] = o
	}
	return out
}

var userRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// HostOK valida o endereço do servidor (definido em main com a mesma regra do terminal).
var HostOK = func(string) bool { return true }

// SetConfig valida e grava. TestedAt é controlado pelo servidor: zera se o destino mudar.
func (m *Monitor) SetConfig(c Config) error {
	if c.Threshold < 5 || c.Threshold > 90 {
		return errors.New("o limite da bateria deve ficar entre 5% e 90%")
	}
	if c.GraceSec < 30 || c.GraceSec > 1800 {
		return errors.New("a espera deve ficar entre 30 segundos e 30 minutos")
	}
	if c.Port == 0 {
		c.Port = 22
	}
	if c.User == "" {
		c.User = hostcmd.DefaultUser
	}
	if c.Port < 1 || c.Port > 65535 || !userRe.MatchString(c.User) || (c.Host != "" && !HostOK(c.Host)) {
		return errors.New("endereço, porta ou usuário do servidor inválido")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.Host != m.cfg.Host || c.Port != m.cfg.Port || c.User != m.cfg.User {
		c.TestedAt = 0
	} else {
		c.TestedAt = m.cfg.TestedAt
	}
	c.HostSetup, c.ChargeLimit, c.ChargeProtect = m.cfg.HostSetup, m.cfg.ChargeLimit, m.cfg.ChargeProtect
	if c.Enabled && !c.Simulate && c.TestedAt == 0 {
		return errors.New("teste a conexão com o servidor antes de desligar o modo simulação")
	}
	if err := m.writeConfigLocked(c); err != nil {
		return err
	}
	if !c.Enabled && m.phase == PhaseCountdown {
		m.resolveLocked(keyShutdown, &events.Event{Kind: "power-shutdown-cancelled", Level: events.Info, Title: "Desligamento cancelado", Body: "O desligamento seguro foi desativado."})
	}
	switch {
	case !c.Enabled && m.phase != PhaseRunning:
		m.phase = PhaseOff
	case c.Enabled && m.phase == PhaseOff:
		m.phase = PhaseArmed
	}
	return nil
}

// MarkTested grava o destino que acabou de passar no teste de conexão. O teste usa a chave
// atual, que só entra num servidor com a configuração atual: passar prova que está atualizado.
func (m *Monitor) MarkTested(t hostcmd.Target) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.cfg
	c.Host, c.Port, c.User, c.TestedAt, c.HostSetup = t.Host, t.Port, t.User, m.Now().Unix(), hostcmd.SetupVersion
	if err := m.writeConfigLocked(c); err != nil {
		return err
	}
	m.resolveLocked(keySetupOld, nil)
	return nil
}

func (m *Monitor) writeConfigLocked(c Config) error {
	if m.ConfigFile != "" {
		if err := store.WriteJSON(m.ConfigFile, c, 0o600); err != nil {
			return errors.New("não foi possível salvar a configuração")
		}
	}
	m.cfg = c
	return nil
}
