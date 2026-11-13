package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"brunos/internal/sysinfo"
	"brunos/internal/tsdb"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func snap(cpu float64, temp *float64, rx float64) sysinfo.Snapshot {
	s := sysinfo.Snapshot{Temp: temp}
	s.CPU.Percent = cpu
	s.Net.Rx = rx
	s.DiskIO = []sysinfo.DiskIO{{Name: "sda", Read: 1e6, Write: 2e6, Busy: 5}}
	return s
}

func TestSeriesFromSamples(t *testing.T) {
	c := &clock{t: time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)}
	db := tsdb.Open("")
	db.Now = c.now
	s := New(db, "")
	temp := 50.0
	for i := 0; i < 15; i++ { // 30 s com CPU 10 %
		s.Add(snap(10, &temp, 1000), 40)
		c.t = c.t.Add(2 * time.Second)
	}
	for i := 0; i < 15; i++ { // 30 s com CPU 30 % (sem sensor de temperatura)
		s.Add(snap(30, nil, 3000), 60)
		c.t = c.t.Add(2 * time.Second)
	}
	s.Add(snap(5, nil, 0), 50) // 10:01, fecha o minuto anterior
	step, pts := s.Series("1h")
	if step != 60 || len(pts) != 2 {
		t.Fatalf("esperava 2 pontos (um fechado, um parcial): %d %+v", step, pts)
	}
	p := pts[0]
	if p.CPU != 20 || p.Mem != 50 || p.Rx != 2000 || p.Temp != 50 {
		t.Fatalf("médias erradas: %+v", p)
	}
	if p.CPUMax != 0 {
		t.Fatal("pico só nos períodos longos")
	}
	if st, mid := s.Series("7d"); st != 300 || len(mid) == 0 || mid[0].CPUMax != 30 {
		t.Fatalf("7 dias: step %d %+v", st, mid)
	}
	if _, d := db.Query("disk.sda.wr", "1h"); len(d) == 0 || d[0].V != 2e6 {
		t.Fatalf("disco: %+v", d)
	}
	if _, x := s.Series("lixo"); len(x) == 0 {
		t.Fatal("período desconhecido vira 24 h")
	}
}

func TestImportOld(t *testing.T) {
	c := &clock{t: time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	old := filepath.Join(dir, "history.json")
	now := c.t.Unix() / 60 * 60
	body := `{"version":1,"minutes":[{"t":` + itoa(now-120) + `,"cpu":12,"mem":40,"rx":10,"tx":5}],` +
		`"hours":[{"t":` + itoa(now/3600*3600-2*3600) + `,"cpu":30,"mem":45,"temp":51,"rx":1,"tx":1}]}`
	if err := os.WriteFile(old, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	db := tsdb.Open(filepath.Join(dir, "tsdb"))
	db.Now = c.now
	s := New(db, old)
	if _, m := s.Series("1h"); len(m) != 1 || m[0].CPU != 12 {
		t.Fatalf("minutos antigos: %+v", m)
	}
	if _, h := s.Series("30d"); len(h) != 3 || h[0].Temp != 51 {
		t.Fatalf("horas antigas na camada de 20 min: %+v", h)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("o arquivo antigo tem que ser apagado depois de trazido")
	}
}

func itoa(v int64) string {
	b := []byte{}
	if v == 0 {
		return "0"
	}
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
