package power

// Desligar e reiniciar pedidos pela interface (0.25.0, decisão 64): o menu de energia e as Configurações
// mandam o pedido com a senha do BrunOS (conferida em main.go). O script do servidor agenda com
// "systemctl ... --when=+1min", então quem decide desligar é o próprio servidor: mesmo que o BrunOS caia,
// o desligamento acontece; cancelar manda "cancelar-desligar" ao script.

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"brunos/internal/events"
	"brunos/internal/hostcmd"
)

const (
	ActShutdown = hostcmd.CmdShutdown // "desligar"
	ActReboot   = hostcmd.CmdReboot   // "reiniciar"
	keyManual   = "power-manual"
	manualDelay = time.Minute     // igual ao --when=+1min do script
	manualGrace = 2 * time.Minute // depois do prazo, o pedido some do estado (o servidor já devia ter ido)
)

// Manual é um desligamento ou reinício pedido pela interface e ainda não feito.
type Manual struct {
	Action    string `json:"action"`
	Deadline  int64  `json:"deadline"`
	Remaining int    `json:"remaining"`
}

var (
	ErrBadAction    = errors.New("pedido inválido: use desligar ou reiniciar")
	ErrManualBusy   = errors.New("já tem um desligamento ou reinício agendado: cancele antes de pedir outro")
	ErrNoManual     = errors.New("não há desligamento nem reinício agendado")
	errScriptTooOld = errors.New("o script do servidor ainda não conhece este pedido: ele se atualiza sozinho em alguns minutos (veja o botão de atualizações) e aí é só tentar de novo")
)

// HostReady diz se a conexão com o servidor (chave atual) já foi configurada e testada.
func (m *Monitor) HostReady() bool {
	c := m.Config()
	return m.HostRun != nil && c.Host != "" && c.TestedAt > 0 && c.HostSetup >= hostcmd.SetupVersion
}

func manualLabels(action string) (verb, noun string) {
	if action == ActReboot {
		return "reiniciar", "Reinício"
	}
	return "desligar", "Desligamento"
}

// manualLocked devolve o pedido em andamento (nil se não houver ou se já passou do prazo + folga).
func (m *Monitor) manualLocked(now time.Time) *Manual {
	if m.manual == nil {
		return nil
	}
	if now.Unix() > m.manual.Deadline+int64(manualGrace/time.Second) {
		m.manual = nil
		m.resolveLocked(keyManual, nil)
		return nil
	}
	out := *m.manual
	out.Remaining = max(0, int(out.Deadline-now.Unix()))
	return &out
}

// ManualStatus é o pedido em andamento, para a interface (nil se não houver).
func (m *Monitor) ManualStatus() *Manual {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.manualLocked(m.Now())
}

// RequestManual pede ao servidor para desligar ou reiniciar em 1 minuto.
func (m *Monitor) RequestManual(ctx context.Context, action string) (*Manual, error) {
	if action != ActShutdown && action != ActReboot {
		return nil, ErrBadAction
	}
	if !m.HostReady() {
		return nil, ErrHostNotReady
	}
	m.mu.Lock()
	if m.manualLocked(m.Now()) != nil {
		m.mu.Unlock()
		return nil, ErrManualBusy
	}
	cfg := m.cfg
	m.mu.Unlock()

	m.execMu.Lock()
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	out, err := m.HostRun(ctx, cfg, action)
	cancel()
	m.execMu.Unlock()
	if err != nil {
		slog.Error("energia: pedido da interface falhou", "pedido", action, "erro", err, "saida", out)
		return nil, manualError(err)
	}
	slog.Warn("energia: servidor vai "+action+" em 1 minuto (pedido pela interface)", "saida", out)

	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.Now()
	m.manual = &Manual{Action: action, Deadline: now.Add(manualDelay).Unix()}
	verb, _ := manualLabels(action)
	m.emitLocked(events.Event{Kind: keyManual, Level: events.Critico, Key: keyManual,
		Title: "O servidor vai " + verb + " em 1 minuto",
		Body:  "Pedido pelo BrunOS. Até lá dá para cancelar pelo botão de energia ou por este aviso.",
		Meta:  map[string]any{"deadline": m.manual.Deadline, "action": action}})
	return m.manualLocked(now), nil
}

// CancelManual cancela o desligamento ou reinício agendado.
func (m *Monitor) CancelManual(ctx context.Context) error {
	m.mu.Lock()
	cur := m.manualLocked(m.Now())
	cfg := m.cfg
	m.mu.Unlock()
	if cur == nil {
		return ErrNoManual
	}
	if !m.HostReady() {
		return ErrHostNotReady
	}
	m.execMu.Lock()
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	out, err := m.HostRun(ctx, cfg, hostcmd.CmdCancelShutdown)
	cancel()
	m.execMu.Unlock()
	if err != nil {
		slog.Error("energia: não deu para cancelar", "erro", err, "saida", out)
		return manualError(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manual = nil
	_, noun := manualLabels(cur.Action)
	m.resolveLocked(keyManual, &events.Event{Kind: "power-manual-cancelled", Level: events.Info,
		Title: noun + " cancelado", Body: "O servidor continua ligado."})
	return nil
}

func manualError(err error) error {
	s := err.Error()
	if strings.Contains(s, "pedido inválido") {
		return errScriptTooOld
	}
	return errors.New(strings.TrimSuffix(friendlyError(err), "."))
}
