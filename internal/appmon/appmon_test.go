package appmon

import (
	"testing"
	"time"

	"brunos/internal/events"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }
func (c *clock) tick()          { c.t = c.t.Add(time.Minute) }

func setup() (*Monitor, *clock, *events.Store) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	m := New()
	m.Now = c.now
	return m, c, events.New("")
}

func TestHistoryAndUptime(t *testing.T) {
	m, c, _ := setup()
	for i := 0; i < 10; i++ {
		up := Up
		if i == 3 || i == 4 {
			up = Down
		}
		if i == 7 {
			up = Unknown // parado de propósito: não conta
		}
		m.Add("immich_migrated", Sample{CPU: float64(i), MemMB: 100, Up: up})
		c.tick()
	}
	h := m.History("immich_migrated")
	if len(h) != 10 || h[0].CPU != 0 || h[9].CPU != 9 {
		t.Fatalf("histórico: %d pontos %+v", len(h), h)
	}
	if pct, ok := m.Uptime("immich_migrated"); !ok || pct != 77.78 { // 7 de 9 minutos medidos
		t.Fatalf("uptime = %v %v", pct, ok)
	}
	// 25 h depois os pontos antigos saem
	c.t = c.t.Add(25 * time.Hour)
	m.Add("immich_migrated", Sample{Up: Up})
	if h := m.History("immich_migrated"); len(h) != 1 {
		t.Fatalf("pontos velhos deveriam sair: %d", len(h))
	}
	if _, ok := m.Uptime("nada"); ok {
		t.Fatal("app sem medição")
	}
}

func TestAlert(t *testing.T) {
	m, c, ev := setup()
	add := func(up int8) {
		m.Add("plex_migrated", Sample{Up: up})
		m.Alert(ev, "plex_migrated", "plex:migrated", "Plex")
		c.tick()
	}

	add(Up)
	add(Down) // um minuto só: não avisa
	if l, _ := ev.List(); len(l) != 0 {
		t.Fatalf("um soluço não deveria avisar: %+v", l)
	}
	add(Down) // segundo seguido: avisa
	add(Down) // não repete
	l, _ := ev.List()
	if len(l) != 1 || l[0].Kind != "app-down" || l[0].Title != "Plex parou de responder" {
		t.Fatalf("esperava um aviso de queda: %+v", l)
	}
	if m.Health("plex_migrated") != Down {
		t.Fatal("estado deveria ser fora do ar")
	}
	add(Up)
	l, _ = ev.List()
	if len(l) != 2 || l[0].Kind != "app-up" {
		t.Fatalf("esperava aviso de volta: %+v", l)
	}
	// parado de propósito não avisa
	add(Unknown)
	add(Unknown)
	if l, _ = ev.List(); len(l) != 2 {
		t.Fatalf("parado de propósito não avisa: %+v", l)
	}
	// sem medição há mais de 2 min: desconhecido
	c.t = c.t.Add(10 * time.Minute)
	if m.Health("plex_migrated") != Unknown {
		t.Fatal("sem medição recente = desconhecido")
	}
}
