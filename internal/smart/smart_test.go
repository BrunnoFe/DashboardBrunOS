package smart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"brunos/internal/events"
	"brunos/internal/hostcmd"
)

func ataJSON(model, serial string, passed bool, realloc, pending, temp int) string {
	return fmt.Sprintf(`{"smartctl":{"exit_status":0},"model_name":%q,"serial_number":%q,"smart_status":{"passed":%v},
"power_on_time":{"hours":12345},"power_cycle_count":321,
"ata_smart_attributes":{"table":[{"id":5,"raw":{"value":%d}},{"id":197,"raw":{"value":%d}},{"id":198,"raw":{"value":0}},
{"id":194,"raw":{"value":%d}}]}}`, model, serial, passed, realloc, pending, temp|(20<<16))
}

const (
	standbyJSON = `{"smartctl":{"exit_status":2,"messages":[{"string":"Device is in STANDBY mode, exit(2)","severity":"information"}]}}`
	// o que o smartctl devolve quando o adaptador não deixa o pedido passar (sem nenhum dado do disco)
	refusedJSON = `{"smartctl":{"exit_status":1,"messages":[{"string":"Read Device Identity failed: scsi error unsupported field in scsi command","severity":"error"}]}}`
	nvmeJSON    = `{"model_name":"NVMe X","smart_status":{"passed":true},"nvme_smart_health_information_log":{"critical_warning":0,"temperature":41,"percentage_used":3,"media_errors":0}}`
	hang        = "HANG" // resposta especial: o pedido trava até estourar o limite
)

// fakeHost imita o script do servidor: a lista de discos e a resposta de cada "smart <disco> <modo>".
type fakeHost struct {
	mu    sync.Mutex
	disks string            // saída do "discos"
	resp  map[string]string // "smart sda sat" → JSON (ou hang)
	calls []string
	err   error // erro de todos os pedidos (sem conexão, script antigo)
}

func (f *fakeHost) query(ctx context.Context, cmd string) ([]byte, error) {
	if !hostcmd.ReadAllowed(cmd) {
		return nil, fmt.Errorf("pedido não permitido: %q", cmd)
	}
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	err, disks, out := f.err, f.disks, f.resp[cmd]
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if cmd == hostcmd.CmdDisks {
		return []byte(disks), nil
	}
	if out == hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if out == "" { // o temperatura reaproveita a resposta do smart
		out = f.resp["smart"+strings.TrimPrefix(cmd, "temperatura")]
	}
	return []byte(out), nil
}

func (f *fakeHost) took(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	f.calls = nil
	return out
}

func diskLine(name, tran, serial string) string {
	return fmt.Sprintf("NAME=%q TRAN=%q SERIAL=%q\n", name, tran, serial)
}

func shortWaits(t *testing.T) {
	l, f, tw, q := listWait, fullWait, tempWait, quickWait
	listWait, fullWait, tempWait, quickWait = time.Second, 150*time.Millisecond, 150*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { listWait, fullWait, tempWait, quickWait = l, f, tw, q })
}

func byDev(s Snapshot, dev string) Disk {
	for _, d := range s.Disks {
		if d.Dev == dev {
			return d
		}
	}
	return Disk{}
}

// Os casos do servidor do Bruno: case ASMedia (sat funciona), JMicron JMS583 com HD SATA (sat funciona,
// auto trava), disco interno, NVMe, HD parado, NVMe em case USB (sat recusa na hora, auto funciona) e
// um case que trava com qualquer modo.
func TestModesAndNoSmart(t *testing.T) {
	shortWaits(t)
	dir := t.TempDir()
	ev := events.New(filepath.Join(dir, "events.json"))
	f := &fakeHost{
		disks: diskLine("sda", "usb", "ASM1") + diskLine("sdb", "usb", "JMS1") + diskLine("sdc", "sata", "SSD1") +
			diskLine("nvme0n1", "nvme", "NV1") + diskLine("sde", "usb", "PARADO") + diskLine("sdf", "usb", "USBNVME") +
			diskLine("sdg", "usb", "RUIM") + diskLine("sr0", "sata", "DVD") + diskLine("loop0", "", ""),
		resp: map[string]string{
			"smart sda sat":      ataJSON("ST1000LM035", "S1", true, 0, 0, 36),
			"smart sdb sat":      ataJSON("WDC WD10", "S2", true, 0, 0, 33),
			"smart sdb auto":     hang,
			"smart sdc auto":     ataJSON("SSD 360GB", "S3", true, 0, 0, 40),
			"smart nvme0n1 auto": nvmeJSON,
			"smart sde sat":      standbyJSON,
			"smart sdf sat":      refusedJSON,
			"smart sdf auto":     nvmeJSON,
			"smart sdg sat":      hang,
			"smart sdg auto":     ataJSON("HD velho", "S7", true, 0, 0, 30),
		},
	}
	temps := map[string]float64{}
	m := New(filepath.Join(dir, "smart.json"), f.query, ev)
	m.SetTemp = func(dev string, c float64, _ time.Time) { temps[dev] = c }

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := f.took("smart")
	want := []string{"smart nvme0n1 auto", "smart sda sat", "smart sdb sat", "smart sdc auto", "smart sde sat", "smart sdf sat", "smart sdf auto", "smart sdg sat"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("pedidos:\n%v\nqueria\n%v", got, want)
	}
	s := m.Snapshot()
	if len(s.Disks) != 7 || s.Err != "" {
		t.Fatalf("discos (sr0 e loop0 ficam de fora): %+v", s)
	}
	for dev, mode := range map[string]string{"sda": "sat", "sdb": "sat", "sdc": "auto", "nvme0n1": "auto", "sdf": "auto", "sde": ""} {
		if d := byDev(s, dev); d.Mode != mode || d.NoSmart {
			t.Errorf("%s: modo %q (queria %q) noSmart=%v", dev, d.Mode, mode, d.NoSmart)
		}
	}
	if d := byDev(s, "sde"); !d.Standby {
		t.Errorf("sde parado: %+v", d)
	}
	sdg := byDev(s, "sdg")
	if !sdg.NoSmart || sdg.NoSmartWhy != WhyStuck || sdg.FailMode != "sat" || sdg.Key != "RUIM" {
		t.Fatalf("sdg deveria ficar sem SMART por travar no sat: %+v", sdg)
	}
	if temps["sda"] != 36 || temps["sdb"] != 33 {
		t.Fatalf("temperaturas: %v", temps)
	}
	list, _ := ev.List()
	if len(list) != 1 || list[0].Kind != "disk-smart-unsupported" || list[0].Level != events.Info ||
		!strings.Contains(list[0].Body, "Tentar de novo") || !strings.Contains(list[0].Body, "ASMedia") {
		t.Fatalf("um aviso descritivo do sdg: %+v", list)
	}

	// 2. basta uma vez: o sdg não é mais lido, nem na leitura completa nem na temperatura
	_ = m.Refresh(context.Background())
	for _, c := range f.took("smart") {
		if strings.Contains(c, "sdg") {
			t.Fatalf("sdg foi lido de novo: %s", c)
		}
	}
	if list, _ := ev.List(); len(list) != 1 {
		t.Fatalf("não avisa de novo: %+v", list)
	}
	m.readTemps(context.Background())
	tw := f.took("temperatura")
	if strings.Join(tw, ",") != "temperatura nvme0n1 auto,temperatura sda sat,temperatura sdb sat,temperatura sdc auto,temperatura sdf auto" {
		t.Fatalf("temperatura só com o modo que funcionou (sem o parado e sem o sdg): %v", tw)
	}

	// 3. Tentar de novo: testa o outro modo (auto) e volta a funcionar
	if err := <-m.Retry("RUIM"); err != nil {
		t.Fatal(err)
	}
	if got := f.took("smart"); len(got) != 1 || got[0] != "smart sdg auto" {
		t.Fatalf("retry deveria tentar o auto: %v", got)
	}
	if d := byDev(m.Snapshot(), "sdg"); d.NoSmart || d.Mode != "auto" || d.Health != HealthOK {
		t.Fatalf("sdg depois do retry: %+v", d)
	}
	if ev.IsActive("smart-unsupported:RUIM") {
		t.Fatal("voltou a funcionar: o próximo sem SMART deve avisar de novo")
	}
	if err := <-m.Retry("NAO-EXISTE"); !errors.Is(err, ErrNoDisk) {
		t.Fatalf("disco desconhecido: %v", err)
	}

	// 4. as letras mudam ao reiniciar: o modo segue o número de série
	f.mu.Lock()
	f.disks = diskLine("sda", "usb", "JMS1") + diskLine("sdb", "usb", "ASM1")
	f.resp["smart sda sat"], f.resp["smart sdb sat"] = ataJSON("WDC WD10", "S2", true, 0, 0, 34), ataJSON("ST1000LM035", "S1", true, 0, 0, 37)
	f.resp["smart sda auto"] = hang
	f.mu.Unlock()
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	s = m.Snapshot()
	if len(s.Disks) != 2 || byDev(s, "sda").Key != "JMS1" || byDev(s, "sda").Mode != "sat" || byDev(s, "sda").Model != "WDC WD10" {
		t.Fatalf("troca de letras: %+v", s.Disks)
	}

	// 5. relê do disco, sumidos lembrados (o "sem SMART" não se perde ao desligar um case)
	m2 := New(filepath.Join(dir, "smart.json"), nil, nil)
	if len(m2.Snapshot().Disks) != 2 || m2.st.Disks["RUIM"] == nil || !m2.st.Disks["RUIM"].Gone {
		t.Fatalf("releitura: %+v", m2.st.Disks)
	}
}

func TestHealthEvents(t *testing.T) {
	shortWaits(t)
	dir := t.TempDir()
	ev := events.New(filepath.Join(dir, "events.json"))
	f := &fakeHost{disks: diskLine("sda", "sata", "WX1"), resp: map[string]string{"smart sda auto": ataJSON("WDC WD10JPVX", "WX1", true, 0, 0, 33)}}
	now := time.Unix(1_800_000_000, 0)
	m := New(filepath.Join(dir, "smart.json"), f.query, ev)
	m.Now = func() time.Time { return now }
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := byDev(m.Snapshot(), "sda"); d.Health != HealthOK || *d.PowerOnHours != 12345 {
		t.Fatalf("sda: %+v", d)
	}
	// setores realocados → aviso uma vez; a mesma leitura de novo não repete
	now = now.Add(6 * time.Hour)
	f.resp["smart sda auto"] = ataJSON("WDC WD10JPVX", "WX1", true, 8, 1, 35)
	_ = m.Refresh(context.Background())
	_ = m.Refresh(context.Background())
	list, _ := ev.List()
	if len(list) != 1 || list[0].Level != "aviso" || !strings.Contains(list[0].Title, "sda") {
		t.Fatalf("aviso de setores: %+v", list)
	}
	if d := byDev(m.Snapshot(), "sda"); d.Health != HealthWarn || len(d.History) != 2 {
		t.Fatalf("atenção e histórico: %+v", d)
	}
	// o disco declara falha → crítico
	f.resp["smart sda auto"] = ataJSON("WDC WD10JPVX", "WX1", false, 8, 1, 35)
	_ = m.Refresh(context.Background())
	list, _ = ev.List()
	if len(list) != 2 || list[0].Level != "critico" || byDev(m.Snapshot(), "sda").Health != HealthFail {
		t.Fatalf("falha: %+v", list)
	}
	// leitura só de temperatura não mexe no selo
	f.resp["temperatura sda auto"] = `{"ata_smart_attributes":{"table":[{"id":194,"raw":{"value":39}}]}}`
	m.readTemps(context.Background())
	if d := byDev(m.Snapshot(), "sda"); *d.Temp != 39 || d.Health != HealthFail {
		t.Fatalf("temperatura: %+v", d)
	}
}

// Estado da 0.11.0 (versão 1, por nome do disco): o histórico passa para a chave nova.
func TestMigrateV1(t *testing.T) {
	shortWaits(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "smart.json")
	v1 := `{"v":1,"checkedAt":1700000000,"disks":{"sdb":{"dev":"sdb","model":"WDC","health":"ok","reallocated":0,"pending":0,"uncorrectable":0,"crcErrors":0,"mediaErrors":0,"checkedAt":1700000000,"history":[{"at":1700000000,"reallocated":0,"pending":0,"passed":true}]}}}`
	if err := os.WriteFile(file, []byte(v1), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeHost{disks: diskLine("sdb", "usb", "JMS1"), resp: map[string]string{"smart sdb sat": ataJSON("WDC", "S2", true, 0, 0, 33)}}
	m := New(file, f.query, nil)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := m.st.Disks["JMS1"]; d == nil || d.Dev != "sdb" || len(d.History) != 1 || d.Mode != "sat" || m.st.Disks["dev:sdb"] != nil {
		t.Fatalf("migração: %+v", m.st.Disks)
	}
}

func TestErrors(t *testing.T) {
	shortWaits(t)
	f := &fakeHost{err: hostcmd.ErrOldScript}
	m := New("", f.query, nil)
	if err := m.Refresh(context.Background()); err == nil || !strings.Contains(m.Snapshot().Err, "Configurações → Energia") {
		t.Fatalf("script antigo: %v %q", err, m.Snapshot().Err)
	}
	f.err = errors.New("o smartmontools não está instalado (sudo apt install smartmontools)")
	_ = m.Refresh(context.Background())
	if !strings.Contains(m.Snapshot().Err, "apt install smartmontools") {
		t.Fatalf("erro amigável: %q", m.Snapshot().Err)
	}
	// nunca o erro cru em inglês
	if got := friendly(context.DeadlineExceeded); strings.Contains(got, "deadline") {
		t.Fatalf("friendly: %q", got)
	}
}

func TestHandlers(t *testing.T) {
	shortWaits(t)
	f := &fakeHost{disks: diskLine("sda", "usb", "A") + diskLine("sdb", "usb", "B"),
		resp: map[string]string{"smart sda sat": ataJSON("A", "A", true, 0, 0, 30), "smart sdb sat": hang, "smart sdb auto": hang}}
	m := New("", f.query, nil)
	mux := http.NewServeMux()
	m.Register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	post := func(path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		var v map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		return rec.Code, v
	}
	// a leitura demora (o sdb trava): responde 202 "lendo" e termina sozinha
	fullWait = 300 * time.Millisecond
	if code, v := post("/api/smart/refresh", "{}"); code != 202 || v["running"] != true {
		t.Fatalf("refresh lento: %d %v", code, v)
	}
	if code, _ := post("/api/smart/refresh", "{}"); code != 429 {
		t.Fatalf("segunda leitura ao mesmo tempo: %d", code)
	}
	deadline := time.Now().Add(3 * time.Second)
	for m.Snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if d := byDev(m.Snapshot(), "sdb"); !d.NoSmart {
		t.Fatalf("sdb sem SMART depois de travar: %+v", d)
	}
	if code, _ := post("/api/smart/retry", `{}`); code != 400 {
		t.Fatalf("retry sem disco: %d", code)
	}
	if code, _ := post("/api/smart/retry", `{"key":"X"}`); code != 404 {
		t.Fatalf("retry de disco desconhecido: %d", code)
	}
	// erro rápido (sem conexão configurada) volta na hora, em português
	f.mu.Lock()
	f.err = errors.New("configure e teste a conexão com o servidor em Configurações → Energia")
	f.mu.Unlock()
	if code, v := post("/api/smart/refresh", "{}"); code != 502 || !strings.Contains(fmt.Sprint(v["error"]), "Energia") {
		t.Fatalf("sem conexão: %d %v", code, v)
	}
}
