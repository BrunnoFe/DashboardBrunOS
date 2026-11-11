package energy

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestMeterAutoAndCost(t *testing.T) {
	file := filepath.Join(t.TempDir(), "energy.json")
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	plugW, plugErr := 16.9, error(nil)
	batW, onBat := 0.0, false
	var uj uint64 = 1_000_000
	raplCalls := 0
	m := New(file, Sources{
		Plug:    func(context.Context, string) (float64, error) { return plugW, plugErr },
		Plugs:   func() []string { return []string{"tuya1"} },
		Battery: func() (float64, bool) { return batW, onBat },
		RAPL: func(context.Context) (uint64, uint64, error) {
			raplCalls++
			uj += 4_000_000 * 60 // 4 W por minuto
			return uj, 0, nil
		},
	})
	m.Now = func() time.Time { return now }
	tick := func() { now = now.Add(time.Minute); m.Sample(context.Background()) }

	// 1. tomada respondendo: Automático usa a tomada e NÃO lê o processador (ainda sem calibração)
	tick()
	c := m.Current()
	if c.Meter != MeterPlug || c.W == nil || *c.W != 16.9 || raplCalls != 0 {
		t.Fatalf("tomada: %+v rapl=%d", c, raplCalls)
	}
	// 2. queda de energia: a tomada lê 0 W, a bateria descarrega 11 W → bateria, e calibra (11 - 4 = 7)
	plugW, batW, onBat = 0, 11, true
	tick()
	tick()
	tick()
	c = m.Current()
	if c.Meter != MeterBattery || *c.W != 11 || c.Offset == nil || math.Abs(*c.Offset-7) > 0.01 {
		t.Fatalf("queda: %+v", c)
	}
	// 3. a energia volta, mas a tomada saiu do ar: estimativa = processador + resto aprendido
	plugErr, onBat, batW = errors.New("tomada fora"), false, 0
	tick()
	c = m.Current()
	if c.Meter != MeterEstimate || c.W == nil || math.Abs(*c.W-11) > 0.01 {
		t.Fatalf("estimativa: %+v", c)
	}
	// 4. Medidor fixo na bateria, com a energia normal: erro explicado e nada somado
	before := m.Current().TodayKWh
	if err := m.SetConfig(Config{Meter: MeterBattery, Tariff: 1}); err != nil {
		t.Fatal(err)
	}
	tick()
	c = m.Current()
	if c.Err == "" || c.W != nil || c.TodayKWh != before {
		t.Fatalf("bateria fixa sem queda: %+v", c)
	}
	// energia: (16,9 + 11×3 + 11) W durante 1 min cada = 60,9 W·min = 0,001015 kWh
	m.mu.Lock()
	kwh := m.st.Days["2026-11-10"]
	m.mu.Unlock()
	if math.Abs(kwh-60.9/60/1000) > 1e-9 || c.Tariff != 1 {
		t.Fatalf("energia do dia: %v %+v", kwh, c)
	}
	// custo e projeção com valores grandes: 9 kWh até o meio-dia do dia 10 (9,5 dias) de 30
	m.mu.Lock()
	m.st.Days["2026-11-01"] = 8.999
	m.mu.Unlock()
	c = m.Current()
	if math.Abs(c.MonthCost-9) > 0.01 || math.Abs(c.Projection-9/9.5*30) > 0.05 {
		t.Fatalf("custo do mês e projeção: %+v", c)
	}
	if err := m.SetConfig(Config{Meter: "inventado", Tariff: 1}); err == nil {
		t.Fatal("Medidor inválido deveria ser recusado")
	}
	// 5. gravou e relê (dias, calibração e Medidor escolhido)
	m.Save()
	m2 := New(file, Sources{})
	if m2.Config().Meter != MeterBattery || m2.offset() == nil || len(m2.Days()) != 2 || m2.Days()[1].Date != "2026-11-10" {
		t.Fatalf("releitura: %+v %v %+v", m2.Config(), m2.offset(), m2.Days())
	}
	pts := m.Series()
	if len(pts) != points || pts[len(pts)-1].T != now.Truncate(time.Minute).Unix() {
		t.Fatalf("série: %d pontos, último %d", len(pts), pts[len(pts)-1].T)
	}
}

func TestRAPLWrap(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	vals := []uint64{900, 100} // deu a volta (max 1000)
	i := 0
	m := New("", Sources{RAPL: func(context.Context) (uint64, uint64, error) {
		v := vals[i]
		i++
		return v * 1_000_000, 1000 * 1_000_000, nil
	}})
	m.Now = func() time.Time { return now }
	if _, ok := m.readRAPL(context.Background()); ok {
		t.Fatal("primeira leitura não dá potência")
	}
	now = now.Add(20 * time.Second)
	w, ok := m.readRAPL(context.Background())
	if !ok || math.Abs(w-10) > 0.001 { // (1000-900+100) J / 20 s = 10 W
		t.Fatalf("volta do contador: %v %v", w, ok)
	}
}
