package power

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"brunos/internal/hostcmd"
)

// pronto deixa a conexão "testada" e grava os pedidos que iriam ao script do servidor.
func pronto(m *Monitor, fail error) *[]string {
	var sent []string
	m.cfg.Host, m.cfg.TestedAt, m.cfg.HostSetup = "192.168.1.50", 1, hostcmd.SetupVersion
	m.HostRun = func(_ context.Context, _ Config, cmd string) (string, error) {
		sent = append(sent, cmd)
		return "", fail
	}
	return &sent
}

func TestManualNeedsHost(t *testing.T) {
	m, _, _, _ := setup(t)
	if _, err := m.RequestManual(context.Background(), ActReboot); !errors.Is(err, ErrHostNotReady) {
		t.Fatalf("sem conexão configurada: %v", err)
	}
	pronto(m, nil)
	if _, err := m.RequestManual(context.Background(), "formatar"); !errors.Is(err, ErrBadAction) {
		t.Fatalf("pedido estranho: %v", err)
	}
}

func TestManualRebootAndCancel(t *testing.T) {
	m, ev, c, _ := setup(t)
	sent := pronto(m, nil)
	cur, err := m.RequestManual(context.Background(), ActReboot)
	if err != nil || cur.Action != ActReboot || cur.Remaining != 60 {
		t.Fatalf("pedido: %+v %v", cur, err)
	}
	if _, err := m.RequestManual(context.Background(), ActShutdown); !errors.Is(err, ErrManualBusy) {
		t.Fatalf("segundo pedido deveria ser recusado: %v", err)
	}
	c.add(20 * time.Second)
	if s := m.ManualStatus(); s == nil || s.Remaining != 40 {
		t.Fatalf("contagem: %+v", s)
	}
	if err := m.CancelManual(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.ManualStatus() != nil {
		t.Fatal("depois de cancelar não deveria ter pedido")
	}
	if strings.Join(*sent, ",") != "reiniciar,cancelar-desligar" {
		t.Fatalf("pedidos ao servidor: %v", *sent)
	}
	got := titles(ev)
	if len(got) != 2 || got[0] != "O servidor vai reiniciar em 1 minuto" || got[1] != "Reinício cancelado" {
		t.Fatalf("avisos: %v", got)
	}
	// depois de cancelar, um pedido novo avisa de novo (a chave saiu dos ativos); o desligar do menu vai
	// pelo "desligar-agendado" (temporizador), não pelo "desligar" da bateria
	if _, err := m.RequestManual(context.Background(), ActShutdown); err != nil {
		t.Fatal(err)
	}
	if last := (*sent)[len(*sent)-1]; last != "desligar-agendado" {
		t.Fatalf("desligar pelo menu mandou %q", last)
	}
	if got := titles(ev); got[len(got)-1] != "O servidor vai desligar em 1 minuto" {
		t.Fatalf("segundo pedido sem aviso: %v", got)
	}
}

func TestManualExpires(t *testing.T) {
	m, _, c, _ := setup(t)
	pronto(m, nil)
	if _, err := m.RequestManual(context.Background(), ActShutdown); err != nil {
		t.Fatal(err)
	}
	c.add(manualDelay + manualGrace + time.Second) // o servidor não desligou: o pedido some
	if m.ManualStatus() != nil {
		t.Fatal("pedido vencido continua")
	}
	if err := m.CancelManual(context.Background()); !errors.Is(err, ErrNoManual) {
		t.Fatalf("cancelar sem pedido: %v", err)
	}
}

func TestManualOldScript(t *testing.T) {
	m, ev, _, _ := setup(t)
	pronto(m, errors.New("o comando saiu com código 2: pedido inválido"))
	_, err := m.RequestManual(context.Background(), ActReboot)
	if err == nil || !strings.Contains(err.Error(), "se atualiza sozinho") {
		t.Fatalf("script antigo: %v", err)
	}
	if m.ManualStatus() != nil || len(titles(ev)) != 0 {
		t.Fatal("pedido que falhou não pode ficar agendado nem avisar")
	}
}
