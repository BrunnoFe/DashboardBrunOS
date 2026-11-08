package history

import (
	"path/filepath"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestAverages(t *testing.T) {
	c := &clock{t: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}
	s := New("")
	s.Now = c.now
	temp := 50.0
	// minuto 10:00: 30 s com CPU 10% e 30 s com CPU 30% → média 20%
	for i := 0; i < 15; i++ {
		s.Add(10, 40, &temp, 1000, 100, 2)
		c.t = c.t.Add(2 * time.Second)
	}
	for i := 0; i < 15; i++ {
		s.Add(30, 60, nil, 3000, 300, 2)
		c.t = c.t.Add(2 * time.Second)
	}
	s.Add(5, 50, nil, 0, 0, 2) // 10:01, fecha o minuto anterior
	step, pts := s.Series("1h")
	if step != 60 || len(pts) != 2 {
		t.Fatalf("esperava 2 pontos (um fechado, um parcial): %d %+v", step, pts)
	}
	p := pts[0]
	if p.CPU != 20 || p.Mem != 50 || p.Rx != 2000 || p.Temp != 50 {
		t.Fatalf("médias erradas: %+v", p)
	}
	// 7 dias: o ponto da hora em andamento aparece
	if step, h := s.Series("7d"); step != 3600 || len(h) != 1 {
		t.Fatalf("horas: %d %+v", step, h)
	}
}

func TestHoursAndPersistence(t *testing.T) {
	c := &clock{t: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "history.json")
	s := New(path)
	s.Now = c.now
	for h := 0; h < 30; h++ { // 30 horas, uma amostra por minuto
		for m := 0; m < 60; m++ {
			s.Add(float64(h), 50, nil, 0, 0, 60)
			c.t = c.t.Add(time.Minute)
		}
	}
	_, mins := s.Series("24h")
	if len(mins) != minuteSlots-1 { // 24 h menos o minuto atual, que ainda não teve amostra
		t.Fatalf("minutos: %d", len(mins))
	}
	_, hours := s.Series("7d")
	if len(hours) != 30 || hours[29].CPU != 29 {
		t.Fatalf("horas: %d %+v", len(hours), hours[len(hours)-1])
	}
	s.Save()
	s2 := New(path)
	s2.Now = c.now
	if _, h := s2.Series("7d"); len(h) != 30 {
		t.Fatalf("relido: %d horas", len(h))
	}
	if _, m := s2.Series("24h"); len(m) < 1430 {
		t.Fatalf("relido: %d minutos", len(m))
	}
}
