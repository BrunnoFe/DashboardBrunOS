package diskhist

import (
	"path/filepath"
	"testing"
	"time"

	"brunos/internal/events"
)

const GB = 1 << 30

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newStore(t *testing.T) (*Store, *clock) {
	c := &clock{t: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	s := New(filepath.Join(t.TempDir(), "disk-history.json"))
	s.Now = c.now
	return s, c
}

func TestRecordOnePointPerDay(t *testing.T) {
	s, c := newStore(t)
	s.Record([]Usage{{Name: "HD", Used: 10 * GB, Free: 90 * GB}})
	c.t = c.t.Add(3 * time.Hour)
	s.Record([]Usage{{Name: "HD", Used: 11 * GB, Free: 89 * GB}})
	f := s.Forecasts()
	if len(f) != 1 || len(f[0].Points) != 1 || f[0].Points[0].Used != 11*GB {
		t.Fatalf("esperava 1 ponto com o último valor do dia: %+v", f)
	}
	if f[0].Enough || f[0].DaysLeft != nil {
		t.Fatal("com um dia só não dá para prever")
	}
	// relê do arquivo
	s2 := New(s.File)
	if got := s2.Forecasts(); len(got) != 1 || got[0].Points[0].Used != 11*GB {
		t.Fatalf("não gravou: %+v", got)
	}
}

func TestForecastLinear(t *testing.T) {
	s, c := newStore(t)
	// ganha 2 GB por dia, com 100 GB livres no fim
	for i := 0; i < 10; i++ {
		used := uint64(100+2*i) * GB
		s.Record([]Usage{{Name: "HD", Used: used, Free: 118*GB - uint64(2*i)*GB}})
		c.t = c.t.AddDate(0, 0, 1)
	}
	f := s.Forecasts()[0]
	if !f.Enough || f.DaysLeft == nil {
		t.Fatalf("deveria prever: %+v", f)
	}
	if f.PerDay != 2*GB {
		t.Fatalf("perDay = %v, esperava %v", f.PerDay, 2*GB)
	}
	if *f.DaysLeft != 50 { // 100 GB livres / 2 GB por dia
		t.Fatalf("daysLeft = %d", *f.DaysLeft)
	}
}

func TestForecastShrinkingOrFlat(t *testing.T) {
	s, c := newStore(t)
	for i := 0; i < 8; i++ {
		s.Record([]Usage{{Name: "A", Used: uint64(50-i) * GB, Free: uint64(50+i) * GB}, {Name: "B", Used: 30 * GB, Free: 70 * GB}})
		c.t = c.t.AddDate(0, 0, 1)
	}
	for _, f := range s.Forecasts() {
		if !f.Enough || f.DaysLeft != nil {
			t.Fatalf("%s: disco que não cresce não tem previsão de encher: %+v", f.Name, f)
		}
	}
}

func TestPrune(t *testing.T) {
	s, c := newStore(t)
	s.Record([]Usage{{Name: "Velho", Used: GB, Free: GB}})
	c.t = c.t.AddDate(0, 0, keepDays+1)
	s.Record([]Usage{{Name: "Novo", Used: GB, Free: GB}})
	f := s.Forecasts()
	if len(f) != 1 || f[0].Name != "Novo" {
		t.Fatalf("disco sumido há meses deveria sair: %+v", f)
	}
}

func TestCheckAlerts(t *testing.T) {
	s, c := newStore(t)
	ev := events.New("")
	// cresce 1 GB/dia com 20 GB livres no fim → ~20 dias
	for i := 0; i < 10; i++ {
		s.Record([]Usage{{Name: "HD", Used: uint64(170+i) * GB, Free: uint64(29-i) * GB}})
		c.t = c.t.AddDate(0, 0, 1)
	}
	now := []Usage{{Name: "HD", Used: 179 * GB, Free: 20 * GB}}
	s.Check(ev, now, false) // previsão desligada (padrão): não avisa
	if l, _ := ev.List(); len(l) != 0 {
		t.Fatalf("com a previsão desligada não deveria avisar: %+v", l)
	}
	s.Check(ev, now, true)
	s.Check(ev, now, true) // não repete
	list, _ := ev.List()
	if len(list) != 1 || list[0].Kind != "disk-full-soon" {
		t.Fatalf("esperava um aviso de 'vai encher': %+v", list)
	}
	if list[0].Title != "O disco HD deve encher em ~20 dias" {
		t.Fatalf("título: %q", list[0].Title)
	}
	s.Check(ev, now, false) // desligou a previsão: fecha o aviso aberto
	if ev.IsActive("disk-full-soon:HD") {
		t.Fatal("desligar a previsão deveria encerrar o aviso")
	}

	// quase cheio (≥ 90%) avisa mesmo com a previsão desligada e, ao liberar (< 85%), avisa que voltou
	full := []Usage{{Name: "HD", Used: 91 * GB, Free: 9 * GB}}
	s.Check(ev, full, false)
	if !ev.IsActive("disk-full:HD") {
		t.Fatal("deveria avisar disco quase cheio")
	}
	s.Check(ev, []Usage{{Name: "HD", Used: 87 * GB, Free: 13 * GB}}, false)
	if !ev.IsActive("disk-full:HD") {
		t.Fatal("entre 85% e 90% o aviso continua aberto")
	}
	s.Check(ev, []Usage{{Name: "HD", Used: 50 * GB, Free: 50 * GB}}, false)
	if ev.IsActive("disk-full:HD") {
		t.Fatal("liberou espaço: o aviso deveria fechar")
	}
}

func TestHuman(t *testing.T) {
	for in, want := range map[float64]string{512: "512 B", 1500: "1,5 KB", 2e9: "2 GB", 1.3e9: "1,3 GB", 12.4e9: "12,4 GB"} {
		if got := human(in); got != want {
			t.Errorf("human(%v) = %q, esperava %q", in, got, want)
		}
	}
}
