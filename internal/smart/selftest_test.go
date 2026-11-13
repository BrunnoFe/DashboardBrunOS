package smart

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"brunos/internal/events"
	"brunos/internal/hostcmd"
)

// ataTestJSON é a resposta do "smart" do script 10 (-c -l selftest): atributos completos, andamento do
// teste (status 249 = rodando, 90% faltando) e o registro com os testes que já terminaram.
func ataTestJSON(running bool, lastPassed bool) string {
	status := `{"value":0,"string":"completed without error","passed":true}`
	if running {
		status = `{"value":249,"string":"in progress, 90% remaining","remaining_percent":90}`
	}
	last := `{"type":{"value":1,"string":"Short offline"},"status":{"value":0,"string":"Completed without error","passed":true},"lifetime_hours":12340}`
	if !lastPassed {
		last = `{"type":{"value":1,"string":"Short offline"},"status":{"value":121,"string":"Completed: read failure","remaining_percent":10,"passed":false},"lifetime_hours":12345}`
	}
	return `{"model_name":"HD X","serial_number":"S1","smart_status":{"passed":true},"power_on_time":{"hours":12345},
"ata_smart_attributes":{"table":[
 {"id":5,"name":"Reallocated_Sector_Ct","value":100,"worst":100,"thresh":10,"when_failed":"","flags":{"prefailure":true},"raw":{"value":0,"string":"0"}},
 {"id":194,"name":"Temperature_Celsius","value":64,"worst":45,"thresh":0,"when_failed":"","flags":{"prefailure":false},"raw":{"value":36,"string":"36 (Min/Max 20/55)"}}]},
"ata_smart_data":{"self_test":{"status":` + status + `,"polling_minutes":{"short":2,"extended":180}}},
"ata_smart_self_test_log":{"standard":{"table":[` + last + `]}}}`
}

type testHost struct {
	mu     sync.Mutex
	smart  string
	acts   []string
	actErr error
}

func (h *testHost) query(ctx context.Context, cmd string) ([]byte, error) {
	if !hostcmd.ReadAllowed(cmd) {
		return nil, fmt.Errorf("pedido não permitido: %q", cmd)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if cmd == hostcmd.CmdDisks {
		return []byte(diskLine("sda", "usb", "S1")), nil
	}
	return []byte(h.smart), nil
}

func (h *testHost) act(ctx context.Context, cmd string) ([]byte, error) {
	if !hostcmd.DiskActAllowed(cmd) {
		return nil, fmt.Errorf("pedido não permitido: %q", cmd)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.acts = append(h.acts, cmd)
	if h.actErr != nil {
		return nil, h.actErr
	}
	return []byte("teste iniciado\n"), nil
}

func (h *testHost) set(s string) { h.mu.Lock(); h.smart = s; h.mu.Unlock() }

// Atributos completos, pedido do teste, andamento, fim com aviso no sino e na Linha do tempo.
func TestSelfTestFlow(t *testing.T) {
	shortWaits(t)
	dir := t.TempDir()
	ev := events.New(filepath.Join(dir, "events.json"))
	h := &testHost{smart: ataTestJSON(false, true)}
	m := New(filepath.Join(dir, "smart.json"), h.query, ev)
	m.Act = h.act
	now := time.Unix(1_800_000_000, 0)
	m.Now = func() time.Time { return now }
	var mu sync.Mutex
	var notes []string
	m.Note = func(key, kind, level, text string) {
		mu.Lock()
		notes = append(notes, key+"|"+kind+"|"+level+"|"+text)
		mu.Unlock()
	}
	ctx := context.Background()
	if err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	d := byDev(m.Snapshot(), "sda")
	if len(d.Attrs) != 2 || d.Attrs[0].Name != "Reallocated_Sector_Ct" || d.Attrs[0].Thresh != 10 || !d.Attrs[0].Prefail || d.Attrs[1].RawStr != "36 (Min/Max 20/55)" {
		t.Fatalf("atributos: %+v", d.Attrs)
	}
	if d.ShortMin != 2 || d.LongMin != 180 || d.Test != nil || len(d.Tests) != 1 || !d.Tests[0].Passed || d.Tests[0].Hours != 12340 {
		t.Fatalf("teste: %+v %+v %d %d", d.Test, d.Tests, d.ShortMin, d.LongMin)
	}
	if err := m.SelfTest(ctx, "S1", "medio"); err == nil {
		t.Fatal("tipo inválido deveria ser recusado")
	}
	if err := m.SelfTest(ctx, "NAO-EXISTE", hostcmd.TestShort); !errors.Is(err, ErrNoDisk) {
		t.Fatalf("disco desconhecido: %v", err)
	}
	if err := m.SelfTest(ctx, "S1", hostcmd.TestShort); err != nil {
		t.Fatal(err)
	}
	if len(h.acts) != 1 || h.acts[0] != "smart-teste sda sat curto" {
		t.Fatalf("pedido: %v", h.acts)
	}
	if d := byDev(m.Snapshot(), "sda"); d.Test == nil || d.Test.Kind != "curto" || d.Test.Remaining != 100 || d.TestAsked == 0 {
		t.Fatalf("depois de pedir: %+v", d.Test)
	}
	if err := m.SelfTest(ctx, "S1", hostcmd.TestLong); !errors.Is(err, ErrTestRunning) {
		t.Fatalf("segundo teste com o primeiro rodando: %v", err)
	}
	// antes de 3 min não lê de novo; depois lê e vê o andamento
	if keys := m.testing(now.Add(time.Minute)); keys != nil {
		t.Fatalf("leu cedo demais: %v", keys)
	}
	h.set(ataTestJSON(true, true))
	now = now.Add(4 * time.Minute)
	m.followTests(ctx, m.testing(now))
	if d := byDev(m.Snapshot(), "sda"); d.Test == nil || d.Test.Remaining != 90 || d.Test.Kind != "curto" {
		t.Fatalf("andamento: %+v", d.Test)
	}
	// terminou com erro de leitura: aviso no sino e na Linha do tempo
	h.set(ataTestJSON(false, false))
	now = now.Add(4 * time.Minute)
	m.followTests(ctx, m.testing(now))
	d = byDev(m.Snapshot(), "sda")
	if d.Test != nil || d.TestAsked != 0 || len(d.Tests) != 1 || d.Tests[0].Passed {
		t.Fatalf("fim: %+v %+v", d.Test, d.Tests)
	}
	found := false
	list, _ := ev.List()
	for _, e := range list {
		if e.Kind == "disk-selftest" && e.Level == events.Aviso && strings.Contains(e.Title, "curto") {
			found = true
		}
	}
	if !found {
		t.Fatalf("sem aviso do fim do teste: %+v", list)
	}
	mu.Lock()
	joined := strings.Join(notes, "\n")
	mu.Unlock()
	if !strings.Contains(joined, "S1|teste|info|Teste curto iniciado") || !strings.Contains(joined, "S1|teste|aviso|Teste terminou: Short offline · Completed: read failure") {
		t.Fatalf("linha do tempo: %s", joined)
	}
	// o servidor recusou o pedido: erro amigável e nada fica "rodando"
	h.actErr = errors.New("o script do servidor é antigo: em Configurações → Energia, copie os comandos de atualização e cole no servidor")
	if err := m.SelfTest(ctx, "S1", hostcmd.TestLong); err == nil || !strings.Contains(err.Error(), "antigo") {
		t.Fatalf("erro do servidor: %v", err)
	}
	if d := byDev(m.Snapshot(), "sda"); d.Test != nil {
		t.Fatalf("pedido recusado não pode deixar teste rodando: %+v", d.Test)
	}
}

// NVMe: os campos do registro de saúde viram atributos; o registro de testes do NVMe também é lido.
func TestNVMeAttrsAndLog(t *testing.T) {
	d := &Disk{}
	r := parseOne([]byte(`{"model_name":"NV","smart_status":{"passed":true},"nvme_smart_health_information_log":{"critical_warning":0,
"temperature":40,"available_spare":100,"available_spare_threshold":10,"percentage_used":2,"data_units_read":123456,"unsafe_shutdowns":7,"media_errors":0},
"nvme_self_test_log":{"current_self_test_operation":{"value":1,"string":"Short self-test in progress"},"current_self_test_completion_percent":30,
"table":[{"self_test_code":{"value":1,"string":"Short"},"self_test_result":{"value":0,"string":"Completed without error"},"power_on_hours":900}]}}`))
	if r.data == nil {
		t.Fatal("sem dados")
	}
	apply(d, r.data, time.Unix(1_800_000_000, 0), true)
	keys := map[string]int64{}
	for _, a := range d.Attrs {
		keys[a.Key] = a.Raw
	}
	if keys["unsafe_shutdowns"] != 7 || keys["available_spare"] != 100 || keys["data_units_read"] != 123456 || len(d.Attrs) != 8 {
		t.Fatalf("atributos NVMe: %+v", d.Attrs)
	}
	if d.Test == nil || d.Test.Remaining != 70 || len(d.Tests) != 1 || !d.Tests[0].Passed || d.Tests[0].Kind != "Short" {
		t.Fatalf("teste NVMe: %+v %+v", d.Test, d.Tests)
	}
}
