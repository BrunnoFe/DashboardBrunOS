package power

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

// A trava do Ubuntu (inhibitor) fez a 0.8.0 desistir depois de 3 tentativas. Agora o
// BrunOS tenta a cada minuto enquanto estiver na bateria, sem encher o sino.
func TestShutdownNeverGivesUp(t *testing.T) {
	m, ev, c, _ := setup(t)
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Simulate, cfg.Host = true, false, "192.168.1.50"
	m.MarkTested(cfg.Target())
	if err := m.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.Shutdown = func(context.Context, Config) (string, error) {
		calls++
		if calls <= 25 {
			return "", errors.New("o comando saiu com código 1: Failed to schedule shutdown: Operation denied due to active block inhibitor")
		}
		return "Shutdown scheduled", nil
	}
	m.Step(bat(20, true))
	c.add(2 * time.Minute)
	for i := 0; i < 25; i++ {
		m.Step(bat(19-i/3, true))
		if m.Status().Phase != PhaseCountdown {
			t.Fatalf("tentativa %d: deveria continuar tentando, fase %s", calls, m.Status().Phase)
		}
		c.add(time.Minute)
	}
	if calls != 25 {
		t.Fatalf("deveria ter tentado 25 vezes, tentou %d", calls)
	}
	fails := 0
	for _, s := range titles(ev) {
		if s == "Não consegui desligar o servidor" {
			fails++
		}
	}
	if fails != 3 { // tentativas 1, 10 e 20
		t.Fatalf("deveria avisar 3 vezes (1ª, 10ª e 20ª), avisou %d", fails)
	}
	list, _ := ev.List()
	if !strings.Contains(list[0].Body, "atualizações automáticas") {
		t.Fatalf("erro da trava deveria vir explicado em português: %q", list[0].Body)
	}
	m.Step(bat(10, true))
	if calls != 26 || m.Status().Phase != PhaseDone {
		t.Fatalf("a 26ª tentativa deveria desligar (chamadas %d, fase %s)", calls, m.Status().Phase)
	}
	if o := m.Outages(); o[0].Shutdown != "executado" {
		t.Fatalf("a queda deveria terminar como executado: %+v", o[0])
	}
}

// O servidor apagou no meio da queda (bateria acabou) e só ligou quando a energia voltou:
// a queda fecha no último minuto em que estava ligado, não na hora em que ligou de novo.
func TestServerDiedDuringOutage(t *testing.T) {
	m, _, c, dir := setup(t)
	m.Step(bat(100, false))
	m.Step(bat(100, true))
	for i := 0; i < 30; i++ {
		c.add(time.Minute)
		m.Step(bat(99-3*i, true))
	}
	lastSeen := c.t.Unix()
	c.add(4 * time.Hour) // desligado
	ev2 := events.New("")
	ev2.Now = c.now
	m2 := New(filepath.Join(dir, "power.json"), filepath.Join(dir, "power-events.json"), ev2, nil)
	m2.Now = c.now
	ev2.Emit(events.Event{Key: keyOutage, Title: "A energia caiu"}) // o sino guarda o aviso aberto em disco
	m2.Step(bat(5, false))                                          // ligou na tomada
	o := m2.Outages()[0]
	if !o.Off || o.End != lastSeen || o.End-o.Start != 1800 {
		t.Fatalf("a queda deveria fechar no último minuto ligado (30 min) e marcar que apagou: %+v", o)
	}
	list, _ := ev2.List()
	if !strings.Contains(list[0].Body, "apagou") || !strings.Contains(list[0].Body, "30 min") {
		t.Fatalf("aviso deveria dizer que o servidor apagou depois de 30 min: %q", list[0].Body)
	}
}

func TestSetupVersion(t *testing.T) {
	m, _, _, _ := setup(t)
	m.MarkTested(Config{Host: "a", Port: 22, User: "brunos-power"}.Target())
	if m.Config().NeedsUpdate() {
		t.Fatal("configuração nova já usa os comandos atuais")
	}
	// servidor configurado na 0.8.0 (sem hostSetup)
	c := m.Config()
	c.HostSetup = 0
	m.mu.Lock()
	m.writeConfigLocked(c)
	m.mu.Unlock()
	if !m.Config().NeedsUpdate() {
		t.Fatal("servidor configurado com o comando antigo deveria pedir atualização")
	}
	cfg := m.Config()
	cfg.HostSetup = 99 // o navegador não decide isso
	m.SetConfig(cfg)
	if !m.Config().NeedsUpdate() {
		t.Fatal("hostSetup vindo do navegador deveria ser ignorado")
	}
	// o teste de conexão usa a chave nova: passar prova que o servidor foi atualizado
	m.MarkTested(c.Target())
	if m.Config().NeedsUpdate() {
		t.Fatal("depois de um teste que passou, não deveria pedir atualização")
	}
	up := UpdateCommands("brunos-power", "x")
	if !strings.Contains(up, "--check-inhibitors=no") || !strings.Contains(up, "<<'EOF'") || strings.Contains(up, "useradd") ||
		!strings.Contains(up, "NOPASSWD: /usr/local/sbin/brunos-host'") || strings.Contains(up, "\t") || strings.Contains(up, "sudo tee /etc/brunos-host.conf") {
		t.Fatalf("comandos de atualização errados (sem pasta do Runtipi, sem a linha do conf):\n%s", up)
	}
	// pasta do Runtipi conhecida: o script fica sabendo onde está o runtipi-cli; caminho estranho fica de fora
	old := RuntipiHost
	t.Cleanup(func() { RuntipiHost = old })
	RuntipiHost = "/home/bruno/runtipi/"
	if up := UpdateCommands("brunos-power", "x"); !strings.HasSuffix(up, "echo 'RUNTIPI=/home/bruno/runtipi' | sudo tee /etc/brunos-host.conf >/dev/null") {
		t.Fatalf("linha do conf:\n%s", up)
	}
	for _, bad := range []string{"runtipi", "/home/x/../etc", "/home/a b", "/home/x'; rm -rf /", "/home/$(id)"} {
		RuntipiHost = bad
		if strings.Contains(UpdateCommands("brunos-power", "x"), "sudo tee /etc/brunos-host.conf") {
			t.Fatalf("caminho %q deveria ficar de fora", bad)
		}
	}
}

// Pulo da bateria (31% → 6%, como em 07/11/2026): abaixo da metade do limite não espera a contagem.
func TestCriticalDropShutsDownNow(t *testing.T) {
	m, ev, c, _ := setup(t)
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Simulate, cfg.Host = true, false, "192.168.1.50"
	m.MarkTested(cfg.Target())
	m.SetConfig(cfg)
	calls := 0
	m.Shutdown = func(context.Context, Config) (string, error) { calls++; return "ok", nil }
	m.Step(bat(31, true))
	c.add(5 * time.Minute)
	m.Step(bat(6, true))
	if calls != 1 || m.Status().Phase != PhaseDone {
		t.Fatalf("deveria desligar na hora (chamadas %d, fase %s)", calls, m.Status().Phase)
	}
	if got := titles(ev); !contains(got, "O servidor vai desligar agora") {
		t.Fatalf("aviso deveria dizer que desliga agora: %v", got)
	}
	// durante a contagem normal, se despencar, também não espera o resto
	m2, _, c2, _ := setup(t)
	m2.MarkTested(cfg.Target())
	m2.SetConfig(cfg)
	n2 := 0
	m2.Shutdown = func(context.Context, Config) (string, error) { n2++; return "ok", nil }
	m2.Step(bat(19, true))
	c2.add(10 * time.Second)
	m2.Step(bat(9, true)) // aplica o atalho
	m2.Step(bat(9, true)) // executa
	if n2 != 1 {
		t.Fatalf("contagem deveria ser encurtada quando a bateria despenca, chamadas %d", n2)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// fakeSys monta um /sys de mentira com uma bateria e os arquivos pedidos.
func fakeSys(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	all := map[string]string{"class/power_supply/BAT0/type": "Battery\n", "class/power_supply/AC/type": "Mains\n",
		"class/dmi/id/sys_vendor": "Dell Inc.\n", "class/dmi/id/product_name": "Inspiron 3542\n"}
	for k, v := range files {
		all[k] = v
	}
	for k, v := range all {
		p := filepath.Join(dir, k)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(v), 0o644)
	}
	return dir
}

func TestChargeDetect(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		kind  string
	}{
		{"Inspiron 3542 (nada)", nil, ChargeNone},
		{"ThinkPad (contínuo)", map[string]string{"class/power_supply/BAT0/charge_control_end_threshold": "80\n"}, ChargeRange},
		{"Samsung (liga/desliga)", map[string]string{"class/power_supply/BAT0/charge_types": "[Standard] Long_Life\n"}, ChargeToggle},
		{"IdeaPad antigo (conservação)", map[string]string{"bus/platform/drivers/ideapad_acpi/VPC2004_00/conservation_mode": "0\n"}, ChargeToggle},
		{"só Standard/Fast (não serve)", map[string]string{"class/power_supply/BAT0/charge_types": "[Standard] Fast\n"}, ChargeNone},
	}
	for _, tc := range cases {
		c := detectCharge(fakeSys(t, tc.files))
		if c.kind != tc.kind {
			t.Errorf("%s: esperava %s, veio %s", tc.name, tc.kind, c.kind)
		}
		if c.model != "Dell Inspiron 3542" {
			t.Errorf("modelo errado: %q", c.model)
		}
	}
}

func TestChargeApplyAndReapply(t *testing.T) {
	m, _, _, _ := setup(t)
	sys := fakeSys(t, map[string]string{"class/power_supply/BAT0/charge_control_end_threshold": "100\n"})
	m.SysDir = sys
	m.DetectCharge()
	end := filepath.Join(sys, "class/power_supply/BAT0/charge_control_end_threshold")
	var smu sync.Mutex
	var sentList []string // a reaplicação roda em outra goroutine: sempre pelo mutex
	sent := func() []string { smu.Lock(); defer smu.Unlock(); return append([]string(nil), sentList...) }
	m.HostRun = func(_ context.Context, _ Config, cmd string) (string, error) {
		smu.Lock()
		sentList = append(sentList, cmd)
		smu.Unlock()
		var n int
		fmt.Sscanf(cmd, "limite %d", &n)
		os.WriteFile(end, []byte(fmt.Sprintf("%d\n", n)), 0o644) // o "servidor" aplica
		return "limite " + strconv.Itoa(n), nil
	}
	ctx := context.Background()
	if _, err := m.SetCharge(ctx, 80, ""); err == nil {
		t.Fatal("sem servidor configurado e testado não deveria aplicar")
	}
	m.MarkTested(Config{Host: "a", Port: 22, User: "brunos-power"}.Target())
	for _, bad := range []int{0, 49, 101} {
		if _, err := m.SetCharge(ctx, bad, ""); err == nil {
			t.Errorf("limite %d deveria ser recusado", bad)
		}
	}
	info, err := m.SetCharge(ctx, 80, "")
	if s := sent(); err != nil || info.Current != 80 || info.Want != 80 || s[len(s)-1] != "limite 80" {
		t.Fatalf("aplicar 80%%: %+v %v %v", info, err, s)
	}
	// o notebook esqueceu (reiniciou): a conferência reaplica
	os.WriteFile(end, []byte("100\n"), 0o644)
	m.checkCharge()
	for i := 0; i < 200 && len(sent()) < 2; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if s := sent(); len(s) != 2 || s[1] != "limite 80" {
		t.Fatalf("deveria reaplicar o limite esquecido: %v", s)
	}
	waitIdle := func() { // espera a goroutine da reaplicação terminar
		for i := 0; i < 200; i++ {
			m.mu.Lock()
			busy := m.chargeBusy
			m.mu.Unlock()
			if !busy {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitIdle()
	// 100% = sem limite: o BrunOS para de mexer
	if info, _ := m.SetCharge(ctx, 100, ""); info.Want != 0 {
		t.Fatalf("100%% deveria parar de gerenciar: %+v", info)
	}
	os.WriteFile(end, []byte("60\n"), 0o644)
	m.mu.Lock()
	m.chargeTry = time.Time{}
	m.mu.Unlock()
	m.checkCharge()
	time.Sleep(50 * time.Millisecond)
	waitIdle()
	if s := sent(); len(s) != 3 {
		t.Fatalf("sem pedido não deveria reaplicar nada: %v", s)
	}
}

func TestChargeNoneDoesNothing(t *testing.T) {
	m, _, _, _ := setup(t)
	m.SysDir = fakeSys(t, nil)
	m.DetectCharge()
	m.MarkTested(Config{Host: "a", Port: 22, User: "brunos-power"}.Target())
	called := false
	m.HostRun = func(context.Context, Config, string) (string, error) { called = true; return "", nil }
	info, err := m.SetCharge(context.Background(), 80, "")
	if err == nil || info.Kind != ChargeNone || info.Model != "Dell Inspiron 3542" {
		t.Fatalf("notebook sem limite deveria recusar e dizer o modelo: %+v %v", info, err)
	}
	m.checkCharge()
	if called {
		t.Fatal("notebook sem limite não deveria chamar o servidor")
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
