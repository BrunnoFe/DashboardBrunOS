package netmon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"brunos/internal/events"
)

func TestOutage(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ev := events.New("")
	m := New(filepath.Join(t.TempDir(), "internet.json"), ev)
	m.Now = func() time.Time { return now }
	online := true
	m.Dial = func(ctx context.Context, addr string) (time.Duration, error) {
		if !online {
			return 0, errors.New("sem rota")
		}
		if addr == "8.8.8.8:443" {
			return 12 * time.Millisecond, nil
		}
		return 30 * time.Millisecond, nil
	}
	step := func() { m.Check(context.Background()); now = now.Add(time.Minute) }

	step()
	st := m.Status()
	if !st.Up || st.Ms != 12 || len(st.Points) != 1 {
		t.Fatalf("online: %+v", st)
	}
	online = false
	step() // um minuto fora: ainda não avisa
	if l, _ := ev.List(); len(l) != 0 {
		t.Fatal("um minuto só não deveria avisar")
	}
	step() // dois: caiu
	l, _ := ev.List()
	if len(l) != 1 || l[0].Kind != "internet-down" || m.Status().Up {
		t.Fatalf("esperava aviso de queda: %+v", l)
	}
	step()
	step()
	online = true
	step()
	l, _ = ev.List()
	if len(l) != 2 || l[0].Kind != "internet-up" || l[0].Body != "Ficou cerca de 4 minutos sem internet." {
		t.Fatalf("esperava aviso de volta: %+v", l)
	}
	st = m.Status()
	if !st.Up || len(st.Outages) != 1 || st.Outages[0].End-st.Outages[0].Start != 240 {
		t.Fatalf("queda registrada: %+v", st.Outages)
	}
	if st.Uptime == nil || *st.Uptime != 33.33 { // 2 de 6 minutos no ar
		t.Fatalf("uptime: %v", st.Uptime)
	}
	// quedas sobrevivem a reinício
	m2 := New(m.File, nil)
	m2.Now = m.Now
	if o := m2.Status().Outages; len(o) != 1 || o[0].End == 0 {
		t.Fatalf("quedas relidas: %+v", o)
	}
}
