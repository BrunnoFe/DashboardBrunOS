package power

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"brunos/internal/events"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func bat(pct int, onBattery bool) *Reading {
	return &Reading{Percent: pct, OnBattery: onBattery, PowerW: 9, EnergyWh: 30}
}
func titles(ev *events.Store) []string {
	list, _ := ev.List()
	out := make([]string, len(list))
	for i, e := range list {
		out[len(list)-1-i] = e.Title // do mais antigo para o mais novo
	}
	return out
}

func setup(t *testing.T) (*Monitor, *events.Store, *clock, string) {
	t.Helper()
	dir := t.TempDir()
	ev := events.New("")
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	ev.Now = c.now
	m := New(filepath.Join(dir, "power.json"), filepath.Join(dir, "power-events.json"), ev, nil)
	m.Now = c.now
	return m, ev, c, dir
}

func TestOutageRecorded(t *testing.T) {
	m, ev, c, dir := setup(t)
	m.Step(bat(100, false))
	m.Step(bat(100, true)) // caiu
	c.add(10 * time.Minute)
	m.Step(bat(91, true))
	m.Step(bat(88, true))
	c.add(5 * time.Minute)
	m.Step(bat(88, false)) // voltou
	got := titles(ev)
	if len(got) != 2 || got[0] != "A energia caiu" || got[1] != "A energia voltou" {
		t.Fatalf("avisos inesperados: %v", got)
	}
	list, _ := ev.List()
	if !strings.Contains(list[0].Body, "15 min") || !strings.Contains(list[0].Body, "12%") {
		t.Fatalf("resumo da queda errado: %q", list[0].Body)
	}
	o := m.Outages()
	if len(o) != 1 || o[0].StartPct != 100 || o[0].MinPct != 88 || o[0].End-o[0].Start != 900 {
		t.Fatalf("queda gravada errada: %+v", o)
	}
	if m.DrainW() != 9 {
		t.Fatalf("consumo medido deveria ser guardado, veio %v", m.DrainW())
	}
	// recarrega do disco
	m2 := New(filepath.Join(dir, "power.json"), filepath.Join(dir, "power-events.json"), nil, nil)
	if len(m2.Outages()) != 1 || m2.DrainW() != 9 {
		t.Fatal("histórico e consumo deveriam sobreviver a um reinício")
	}
}

func TestRestartDuringOutage(t *testing.T) {
	m, _, c, dir := setup(t)
	m.Step(bat(100, true))
	c.add(time.Minute)
	ev2 := events.New("")
	m2 := New(filepath.Join(dir, "power.json"), filepath.Join(dir, "power-events.json"), ev2, nil)
	m2.Now = c.now
	m2.Step(bat(97, true)) // continua na bateria: não abre outra queda
	if len(m2.Outages()) != 1 {
		t.Fatalf("não deveria abrir outra queda ao reiniciar: %+v", m2.Outages())
	}
	m2.Step(bat(97, false))
	if o := m2.Outages(); o[0].End == 0 {
		t.Fatal("a queda deveria fechar quando a energia voltar")
	}
}

func TestShutdownSimulated(t *testing.T) {
	m, ev, c, _ := setup(t)
	cfg := DefaultConfig()
	cfg.Enabled = true
	if err := m.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	called := false
	m.Shutdown = func(context.Context, Config) (string, error) { called = true; return "", nil }
	m.Step(bat(50, true))
	m.Step(bat(21, true))
	if m.Status().Phase != PhaseArmed {
		t.Fatalf("acima do limite deveria só vigiar, fase %s", m.Status().Phase)
	}
	m.Step(bat(20, true))
	st := m.Status()
	if st.Phase != PhaseCountdown || st.Deadline != c.t.Unix()+120 {
		t.Fatalf("deveria começar a contagem de 2 min: %+v", st)
	}
	c.add(119 * time.Second)
	m.Step(bat(19, true))
	if m.Status().Phase != PhaseCountdown {
		t.Fatal("ainda não deveria desligar")
	}
	c.add(time.Second)
	m.Step(bat(19, true))
	if m.Status().Phase != PhaseDone || called {
		t.Fatalf("simulação não pode chamar o desligamento de verdade (fase %s, chamado %v)", m.Status().Phase, called)
	}
	if o := m.Outages(); o[0].Shutdown != "simulado" {
		t.Fatalf("a queda deveria registrar a simulação: %+v", o[0])
	}
	got := titles(ev)
	if !strings.HasPrefix(got[1], "Simulação: o servidor desligaria") || got[2] != "Simulação: o servidor teria desligado agora" {
		t.Fatalf("avisos inesperados: %v", got)
	}
	// continua na bateria: não repete
	c.add(time.Hour)
	m.Step(bat(10, true))
	if m.Status().Phase != PhaseDone {
		t.Fatal("não deveria repetir na mesma queda")
	}
	// energia volta: vigia de novo
	m.Step(bat(10, false))
	if m.Status().Phase != PhaseArmed {
		t.Fatal("deveria voltar a vigiar quando a energia voltar")
	}
}

func TestCountdownCancelledByPowerAndByUser(t *testing.T) {
	m, ev, c, _ := setup(t)
	cfg := DefaultConfig()
	cfg.Enabled = true
	m.SetConfig(cfg)
	m.Step(bat(15, true))
	if m.Status().Phase != PhaseCountdown {
		t.Fatal("deveria contar")
	}
	m.Step(bat(15, false)) // energia voltou
	if m.Status().Phase != PhaseArmed || ev.IsActive(keyShutdown) {
		t.Fatal("energia voltando deveria cancelar a contagem")
	}
	m.Step(bat(15, true))
	if err := m.Cancel(); err != nil {
		t.Fatal(err)
	}
	c.add(time.Hour)
	m.Step(bat(5, true))
	if m.Status().Phase != PhaseCancelled {
		t.Fatalf("cancelado pelo usuário não deveria desligar nesta queda, fase %s", m.Status().Phase)
	}
	if err := m.Cancel(); err == nil {
		t.Fatal("cancelar sem contagem deveria dar erro")
	}
	if o := m.Outages(); o[0].Shutdown != "cancelado" {
		t.Fatalf("a queda deveria registrar o cancelamento: %+v", o[0])
	}
}

func TestRealShutdownRetries(t *testing.T) {
	m, ev, c, _ := setup(t)
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Simulate, cfg.Host = true, false, "192.168.1.50"
	if err := m.SetConfig(cfg); err == nil {
		t.Fatal("desligar a simulação sem testar a conexão deveria ser recusado")
	}
	m.MarkTested(cfg.Target())
	if err := m.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.Shutdown = func(_ context.Context, got Config) (string, error) {
		calls++
		if got.Host != "192.168.1.50" || got.User != "brunos-power" {
			t.Errorf("destino errado: %+v", got)
		}
		if calls == 1 {
			return "", errors.New("sem rota até 192.168.1.50:22")
		}
		return "Shutdown scheduled", nil
	}
	m.Step(bat(20, true))
	c.add(2 * time.Minute)
	m.Step(bat(19, true))
	if calls != 1 || m.Status().Phase != PhaseCountdown {
		t.Fatalf("falha deveria reagendar (chamadas %d, fase %s)", calls, m.Status().Phase)
	}
	c.add(time.Minute)
	m.Step(bat(18, true))
	if calls != 2 || m.Status().Phase != PhaseDone {
		t.Fatalf("segunda tentativa deveria desligar (chamadas %d, fase %s)", calls, m.Status().Phase)
	}
	got := titles(ev)
	if got[len(got)-1] != "Desligando o servidor" {
		t.Fatalf("último aviso deveria ser o desligamento: %v", got)
	}
	// trocar o destino zera o teste
	cfg.Host = "outro"
	cfg.Simulate = true
	m.SetConfig(cfg)
	if m.Config().TestedAt != 0 {
		t.Fatal("mudar o servidor deveria exigir novo teste")
	}
}

func TestConfigValidation(t *testing.T) {
	m, _, _, _ := setup(t)
	for _, c := range []Config{
		{Threshold: 2, GraceSec: 60},
		{Threshold: 20, GraceSec: 5},
		{Threshold: 20, GraceSec: 60, User: "root; rm"},
		{Threshold: 20, GraceSec: 60, Port: 70000},
	} {
		if err := m.SetConfig(c); err == nil {
			t.Errorf("%+v deveria ser recusado", c)
		}
	}
	if err := m.SetConfig(Config{Threshold: 25, GraceSec: 60}); err != nil {
		t.Fatal(err)
	}
	if c := m.Config(); c.User != "brunos-power" || c.Port != 22 {
		t.Fatalf("padrões não aplicados: %+v", c)
	}
	if !strings.Contains(SetupCommands("brunos-power", `restrict,command="x" ssh-ed25519 AAA brunos-power`), "/etc/sudoers.d/brunos-power") {
		t.Fatal("comandos de configuração incompletos")
	}
}

func TestDisabledDoesNothing(t *testing.T) {
	m, _, c, _ := setup(t)
	m.Step(bat(5, true))
	c.add(time.Hour)
	m.Step(bat(1, true))
	if m.Status().Phase != PhaseOff {
		t.Fatal("desativado não deveria contar nem desligar")
	}
}
