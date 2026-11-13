package tsdb

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock               { return &clock{time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)} }
func near(a, b float32) bool         { return math.Abs(float64(a-b)) < 1e-3 }
func feed(db *DB, c *clock, name string, v float64, every time.Duration, n int) {
	for i := 0; i < n; i++ {
		db.AddAt(name, v, c.t)
		c.add(every)
	}
}

func TestMinuteAverageAndTiers(t *testing.T) {
	c := newClock()
	db := Open("")
	db.Now = c.now
	// 10 minutos: CPU 10 % com um pico de 90 % numa amostra do minuto 3
	for m := 0; m < 10; m++ {
		for s := 0; s < 4; s++ { // 4 amostras por minuto (a cada 15 s)
			v := 10.0
			if m == 3 && s == 2 {
				v = 90
			}
			db.AddAt("sys.cpu", v, c.t)
			c.add(15 * time.Second)
		}
	}
	step, pts := db.Query("sys.cpu", "1h")
	if step != 60 || len(pts) != 10 {
		t.Fatalf("fina: step %d, %d pontos", step, len(pts))
	}
	if !near(pts[3].V, 30) { // (10+10+90+10)/4
		t.Fatalf("média do minuto 3 = %v", pts[3].V)
	}
	step, mid := db.Query("sys.cpu", "7d")
	if step != 300 || len(mid) != 2 {
		t.Fatalf("média: step %d, %d pontos", step, len(mid))
	}
	if !near(mid[0].Max, 90) {
		t.Fatalf("o pico do minuto 3 tem que aparecer na camada de 5 min: %v", mid[0].Max)
	}
	if !near(mid[0].V, 14) { // (10+10+10+30+10)/5
		t.Fatalf("média de 5 min = %v", mid[0].V)
	}
	_, coarse := db.Query("sys.cpu", "30d")
	if len(coarse) != 1 || !near(coarse[0].Max, 90) {
		t.Fatalf("grossa: %+v", coarse)
	}
}

func TestGapsAndRingWrap(t *testing.T) {
	c := newClock()
	db := Open("")
	db.Now = c.now
	feed(db, c, "x", 1, time.Minute, 3)
	c.add(10 * time.Minute) // buraco: o servidor dormiu
	feed(db, c, "x", 2, time.Minute, 3)
	_, pts := db.Query("x", "1h")
	if len(pts) != 6 {
		t.Fatalf("buraco não pode virar ponto: %d pontos", len(pts))
	}
	if pts[3].T-pts[2].T != 11*60 {
		t.Fatalf("o buraco tem que aparecer no tempo: %d", pts[3].T-pts[2].T)
	}
	// 3 dias depois: a camada fina (48 h) não guarda mais nada antigo
	c.add(72 * time.Hour)
	feed(db, c, "x", 5, time.Minute, 2)
	_, pts = db.Query("x", "48h")
	for _, p := range pts {
		if p.V != 5 {
			t.Fatalf("ponto de 3 dias atrás sobrou na camada fina: %+v", p)
		}
	}
	_, mid := db.Query("x", "7d")
	if len(mid) < 2 {
		t.Fatalf("a camada de 7 dias tem que lembrar de 3 dias atrás: %d", len(mid))
	}
}

func TestPartialCurrentPeriod(t *testing.T) {
	c := newClock()
	db := Open("")
	db.Now = c.now
	feed(db, c, "x", 4, 10*time.Second, 3) // 30 s do minuto atual
	_, pts := db.Query("x", "5m")
	if len(pts) != 1 || !near(pts[0].V, 4) {
		t.Fatalf("o minuto em andamento entra como ponto parcial: %+v", pts)
	}
	if _, p := db.Query("x", "2h"); p != nil {
		t.Fatal("período desconhecido devolve nil")
	}
}

func TestNoSamplesClosesPeriods(t *testing.T) {
	c := newClock()
	db := Open("")
	db.Now = c.now
	feed(db, c, "app.x.cpu", 3, time.Minute, 2)
	c.add(30 * time.Minute) // o app parou: nenhuma amostra nova
	_, mid := db.Query("app.x.cpu", "7d")
	if len(mid) != 1 || !near(mid[0].V, 3) {
		t.Fatalf("os 5 min fechados sem amostra nova também aparecem: %+v", mid)
	}
}

func TestSaveJournalReopen(t *testing.T) {
	dir := t.TempDir()
	c := newClock()
	db := Open(dir)
	db.Now = c.now
	feed(db, c, "sys.mem", 50, time.Minute, 30)
	db.Save() // foto
	feed(db, c, "sys.mem", 70, time.Minute, 10)
	c.add(6 * time.Minute)
	db.Tick() // diário (5 min depois)
	if _, err := os.Stat(filepath.Join(dir, "journal.log")); err != nil {
		t.Fatal("o diário tem que existir depois do Tick")
	}
	// reinício brusco: abre de novo (foto + diário)
	db2 := Open(dir)
	db2.Now = c.now
	_, pts := db2.Query("sys.mem", "1h")
	if len(pts) < 39 {
		t.Fatalf("depois de reabrir: %d pontos (esperava ~40)", len(pts))
	}
	if !near(pts[len(pts)-1].V, 70) || !near(pts[0].V, 50) {
		t.Fatalf("valores errados: primeiro %v, último %v", pts[0].V, pts[len(pts)-1].V)
	}
	db2.Save()
	if _, err := os.Stat(filepath.Join(dir, "journal.log")); !os.IsNotExist(err) {
		t.Fatal("a foto zera o diário")
	}
}

func TestCorruptFilesStartEmpty(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "snapshot.bin"), []byte("lixo"), 0o600)
	os.WriteFile(filepath.Join(dir, "journal.log"), []byte("0 x 1\nnão é número\n"), 0o600)
	db := Open(dir)
	if n := len(db.Names("")); n != 0 {
		t.Fatalf("arquivo ruim não pode virar série: %d", n)
	}
}

func TestForgetOldAndLimit(t *testing.T) {
	dir := t.TempDir()
	c := newClock()
	db := Open(dir)
	db.Now = c.now
	feed(db, c, "app.velho.cpu", 1, time.Minute, 2)
	c.add(31 * 24 * time.Hour)
	feed(db, c, "app.novo.cpu", 1, time.Minute, 2)
	db.Save()
	if n := db.Names("app."); len(n) != 1 || n[0] != "app.novo.cpu" {
		t.Fatalf("série parada há 31 dias tem que ser esquecida: %v", n)
	}
	for i := 0; i < MaxSeries+10; i++ {
		db.AddAt("s"+string(rune('a'+i%26))+string(rune('a'+i/26)), 1, c.t)
	}
	if n := len(db.Names("")); n > MaxSeries {
		t.Fatalf("passou do limite: %d", n)
	}
}

func TestCleanNames(t *testing.T) {
	if Clean("app.meu app\n.cpu") != "app.meu_app_.cpu" {
		t.Fatal(Clean("app.meu app\n.cpu"))
	}
}

func TestImport(t *testing.T) {
	c := newClock()
	db := Open("")
	db.Now = c.now
	db.Import("sys.cpu", 2, c.t.Unix()-3600, 12, 40)
	_, p := db.Query("sys.cpu", "30d")
	if len(p) != 1 || !near(p[0].Max, 40) {
		t.Fatalf("import: %+v", p)
	}
}

func TestConcurrent(t *testing.T) {
	db := Open("")
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				db.Add("x", float64(i))
				db.Query("x", "1h")
			}
		}(g)
	}
	wg.Wait()
}
