package smart

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"brunos/internal/events"
)

func ataJSON(model, serial string, passed bool, realloc, pending, temp int) string {
	return fmt.Sprintf(`{"smartctl":{"exit_status":0},"model_name":%q,"serial_number":%q,"smart_status":{"passed":%v},
"power_on_time":{"hours":12345},"power_cycle_count":321,
"ata_smart_attributes":{"table":[{"id":5,"raw":{"value":%d}},{"id":197,"raw":{"value":%d}},{"id":198,"raw":{"value":0}},
{"id":194,"raw":{"value":%d}}]}}`, model, serial, passed, realloc, pending, temp|(20<<16))
}

const standbyJSON = `{"smartctl":{"exit_status":2,"messages":[{"string":"Device is in STANDBY mode, exit(2)","severity":"information"}]}}`

func TestSmart(t *testing.T) {
	dir := t.TempDir()
	ev := events.New(filepath.Join(dir, "events.json"))
	out := ""
	temps := map[string]float64{}
	now := time.Unix(1_800_000_000, 0)
	m := New(filepath.Join(dir, "smart.json"), func(_ context.Context, cmd string) ([]byte, error) {
		if cmd != "smart" && cmd != "temperatura" {
			t.Fatalf("pedido inesperado %q", cmd)
		}
		return []byte(out), nil
	}, ev)
	m.Now = func() time.Time { return now }
	m.SetTemp = func(dev string, c float64, _ time.Time) { temps[dev] = c }

	// 1. sda saudável (USB, temperatura pelo atributo 194), sdb parado, nvme ok
	out = "=== /dev/sda\n" + ataJSON("WDC WD10JPVX", "WX1", true, 0, 0, 33) + "\n=== /dev/sdb\n" + standbyJSON +
		"\n=== /dev/nvme0n1\n" + `{"model_name":"NVMe X","smart_status":{"passed":true},"nvme_smart_health_information_log":{"critical_warning":0,"temperature":41,"percentage_used":3,"media_errors":0}}` + "\n"
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := m.Snapshot()
	if len(s.Disks) != 3 {
		t.Fatalf("discos: %+v", s.Disks)
	}
	sda, sdb, nv := s.Disks[1], s.Disks[2], s.Disks[0] // ordem: nvme0n1, sda, sdb
	if sda.Health != HealthOK || *sda.Temp != 33 || *sda.PowerOnHours != 12345 || temps["sda"] != 33 {
		t.Fatalf("sda: %+v temps=%v", sda, temps)
	}
	if !sdb.Standby || sdb.Health != "" {
		t.Fatalf("sdb parado: %+v", sdb)
	}
	if nv.Health != HealthOK || *nv.PercentUsed != 3 || *nv.Temp != 41 {
		t.Fatalf("nvme: %+v", nv)
	}
	if list, _ := ev.List(); len(list) != 0 {
		t.Fatalf("disco saudável não avisa: %+v", list)
	}

	// 2. sda ganha setores realocados → aviso uma vez; a mesma leitura de novo não repete
	now = now.Add(6 * time.Hour)
	out = "=== /dev/sda\n" + ataJSON("WDC WD10JPVX", "WX1", true, 8, 1, 35) + "\n"
	_ = m.Refresh(context.Background())
	_ = m.Refresh(context.Background())
	list, _ := ev.List()
	if len(list) != 1 || list[0].Level != "aviso" || !strings.Contains(list[0].Title, "sda") {
		t.Fatalf("aviso de setores: %+v", list)
	}
	if d := m.Snapshot().Disks[1]; d.Health != HealthWarn || len(d.History) != 2 {
		t.Fatalf("atenção e histórico: %+v", d)
	}

	// 3. o disco declara falha → crítico
	out = "=== /dev/sda\n" + ataJSON("WDC WD10JPVX", "WX1", false, 8, 1, 35) + "\n"
	_ = m.Refresh(context.Background())
	list, _ = ev.List()
	if len(list) != 2 || list[0].Level != "critico" || m.Snapshot().Disks[1].Health != HealthFail {
		t.Fatalf("falha: %+v", list)
	}

	// 4. leitura só de temperatura não mexe no selo
	out = "=== /dev/sda\n" + `{"ata_smart_attributes":{"table":[{"id":194,"raw":{"value":39}}]}}` + "\n"
	m.readTemps(context.Background())
	if d := m.Snapshot().Disks[1]; *d.Temp != 39 || d.Health != HealthFail || temps["sda"] != 39 {
		t.Fatalf("temperatura: %+v", d)
	}

	// 5. relê do disco
	m2 := New(filepath.Join(dir, "smart.json"), nil, nil)
	if len(m2.Snapshot().Disks) != 3 || m2.Snapshot().CheckedAt != now.Unix() {
		t.Fatalf("releitura: %+v", m2.Snapshot())
	}
}

func TestSmartScriptMissing(t *testing.T) {
	m := New("", func(context.Context, string) ([]byte, error) {
		return nil, fmt.Errorf("o smartmontools não está instalado (sudo apt install smartmontools)")
	}, nil)
	if err := m.Refresh(context.Background()); err == nil || !strings.Contains(m.Snapshot().Err, "apt install smartmontools") {
		t.Fatalf("erro amigável: %v %q", err, m.Snapshot().Err)
	}
}
