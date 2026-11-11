package updates

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"brunos/internal/events"
	"brunos/internal/runtipi"
)

const aptOut = `apt 12;3
reiniciar
=== pacotes
linux-image-generic/noble-updates 6.8.0-50 amd64 [upgradable from: 6.8.0-49]
openssl/noble-security 3.0.13-0ubuntu3.5 amd64 [upgradable from: 3.0.13-0ubuntu3.4]
`

func TestParseOS(t *testing.T) {
	o := ParseOS([]byte(aptOut))
	if o.Total != 12 || o.Security != 3 || !o.Reboot || len(o.Packages) != 2 || o.Packages[1] != "openssl" {
		t.Fatalf("%+v", o)
	}
	if o := ParseOS([]byte("=== pacotes\na/x 1\nb/y 2\n")); o.Total != 2 || o.Reboot {
		t.Fatalf("sem apt-check: %+v", o)
	}
}

func TestCheckAndNotify(t *testing.T) {
	dir := t.TempDir()
	ev := events.New(filepath.Join(dir, "events.json"))
	out := aptOut
	notifyApps := true
	c := New(filepath.Join(dir, "updates.json"))
	c.Events = ev
	c.NotifyApps = func() bool { return notifyApps }
	c.Apps = func() []runtipi.Update {
		return []runtipi.Update{{URN: "plex:migrated", Name: "Plex", Installed: "1.41", Available: "1.42"}}
	}
	c.Query = func(context.Context, string) ([]byte, error) { return []byte(out), nil }
	c.Check(context.Background())
	c.Check(context.Background()) // a mesma situação não repete os avisos
	list, _ := ev.List()
	if len(list) != 3 {
		t.Fatalf("esperava 3 avisos (app, reinício, segurança): %+v", list)
	}
	if u, ok := c.For("plex:migrated"); !ok || u.Available != "1.42" {
		t.Fatal("For deveria achar o Plex")
	}
	// reiniciou: o aviso de reinício é resolvido
	out = "apt 0;0\n=== pacotes\n"
	c.Now = func() time.Time { return time.Now().Add(time.Hour) }
	c.Check(context.Background())
	if ev.IsActive("os-reboot") {
		t.Fatal("o aviso de reinício deveria ser resolvido")
	}
	// relê do disco
	if s := New(filepath.Join(dir, "updates.json")).Snapshot(); len(s.Apps) != 1 || s.OS.Reboot {
		t.Fatalf("releitura: %+v", s)
	}
}
