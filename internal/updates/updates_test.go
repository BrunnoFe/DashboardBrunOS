package updates

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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
	if s := New(filepath.Join(dir, "updates.json")).Snapshot(); len(s.Apps) != 1 || s.OS.Reboot || s.Every != "24h" {
		t.Fatalf("releitura: %+v", s)
	}
}

func TestEvery(t *testing.T) {
	dir := t.TempDir()
	c := New(filepath.Join(dir, "updates.json"))
	if err := c.SetEvery("12h"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEvery("1m"); err == nil {
		t.Fatal("frequência inválida deveria ser recusada")
	}
	if New(filepath.Join(dir, "updates.json")).Snapshot().Every != "12h" {
		t.Fatal("a frequência deveria ficar guardada")
	}
}

func TestParseStatus(t *testing.T) {
	out := "rodando\n=== registro\ninicio 1000 atualizar-linux \nHit:1 http://archive.ubuntu.com\n\x1b[32mReading package lists...\x1b[0m\nProgress: 10%\rProgress: 90%\r\n"
	s := ParseStatus([]byte(out), 1000)
	if !s.Running || !s.Started || s.Done || len(s.Lines) != 3 || s.Lines[1] != "Reading package lists..." || s.Lines[2] != "Progress: 90%" {
		t.Fatalf("%+v", s)
	}
	s = ParseStatus([]byte("parado\n=== registro\ninicio 1000 atualizar-app immich:migrated\nok\nfim 0\n"), 1000)
	if s.Running || !s.Done || s.Exit != 0 || len(s.Lines) != 1 {
		t.Fatalf("fim: %+v", s)
	}
	// registro de uma atualização antiga (antes do pedido) não conta
	if s := ParseStatus([]byte("parado\n=== registro\ninicio 100 atualizar-linux\nfim 0\n"), 1000); s.Started || s.Done || len(s.Lines) != 0 {
		t.Fatalf("antiga: %+v", s)
	}
}

// fakeServer imita o script: "atualizar-*" começa, "atualizacao-estado" mostra o andamento.
type fakeServer struct {
	mu     sync.Mutex
	cmds   []string
	status string
}

func (f *fakeServer) act(_ context.Context, cmd string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, cmd)
	if cmd == "atualizacao-estado" {
		return []byte(f.status), nil
	}
	return []byte("iniciado\n"), nil
}

func (f *fakeServer) set(s string) { f.mu.Lock(); f.status = s; f.mu.Unlock() }

func waitDone(t *testing.T, c *Checker) Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if j := c.Snapshot().Job; j != nil && !j.Running {
			return *j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a atualização não terminou")
	return Job{}
}

func TestStartAndFollow(t *testing.T) {
	old := pollEvery
	pollEvery = 20 * time.Millisecond
	t.Cleanup(func() { pollEvery = old })
	dir := t.TempDir()
	ev := events.New(filepath.Join(dir, "events.json"))
	f := &fakeServer{}
	c := New(filepath.Join(dir, "updates.json"))
	c.Events = ev
	c.Act = f.act
	var amu sync.Mutex
	apps := []runtipi.Update{{URN: "immich:migrated", Name: "Immich", Installed: "v2.1", Available: "v2.2"}}
	c.Apps = func() []runtipi.Update { amu.Lock(); defer amu.Unlock(); return apps }
	c.Query = func(context.Context, string) ([]byte, error) { return []byte("apt 0;0\n"), nil }
	c.Check(context.Background())
	now := time.Now().Unix()
	var nmu sync.Mutex
	c.Now = func() time.Time { nmu.Lock(); defer nmu.Unlock(); return time.Unix(now, 0) }
	setNow := func(v int64) { nmu.Lock(); now = v; nmu.Unlock() }

	if err := c.Start(context.Background(), "app", "plex:migrated"); !errors.Is(err, ErrNoUpdate) {
		t.Fatalf("app sem atualização: %v", err)
	}
	if err := c.Start(context.Background(), "tudo", ""); !errors.Is(err, ErrKind) {
		t.Fatalf("tipo inválido: %v", err)
	}
	t0 := now
	f.set(fmt.Sprintf("rodando\n=== registro\ninicio %d atualizar-app immich:migrated\nPulling images...\n", t0))
	if err := c.Start(context.Background(), "app", "immich:migrated"); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background(), "linux", ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("duas ao mesmo tempo: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if j := c.Snapshot().Job; !j.Running || len(j.Lines) != 1 || j.Lines[0] != "Pulling images..." || j.From != "v2.1" {
		t.Fatalf("andamento: %+v", j)
	}
	// terminou bem: aviso no sino e confere de novo (o Immich sai da lista)
	amu.Lock()
	apps = nil
	amu.Unlock()
	f.set(fmt.Sprintf("parado\n=== registro\ninicio %d atualizar-app immich:migrated\nPulling images...\nApp updated\nfim 0\n", t0))
	if j := waitDone(t, c); !j.OK || j.Err != "" {
		t.Fatalf("fim: %+v", j)
	}
	time.Sleep(50 * time.Millisecond) // a conferência depois de atualizar
	if len(c.Snapshot().Apps) != 0 {
		t.Fatal("depois de atualizar, confere de novo")
	}
	list, _ := ev.List()
	if len(list) == 0 || list[0].Title != "Immich atualizado" || !strings.Contains(list[0].Body, "v2.1 para v2.2") {
		t.Fatalf("aviso: %+v", list)
	}
	f.mu.Lock()
	first := f.cmds[0]
	f.mu.Unlock()
	if first != "atualizar-app immich:migrated" {
		t.Fatalf("pedido: %v", first)
	}

	// Linux com erro: aviso com a última linha
	t1 := t0 + 100
	setNow(t1)
	f.set(fmt.Sprintf("parado\n=== registro\ninicio %d atualizar-linux\nE: Could not get lock /var/lib/dpkg/lock-frontend\nfim 100\n", t1))
	if err := c.Start(context.Background(), "linux", ""); err != nil {
		t.Fatal(err)
	}
	if j := waitDone(t, c); j.OK || !strings.Contains(j.Err, "código 100") {
		t.Fatalf("erro: %+v", j)
	}
	list, _ = ev.List()
	if list[0].Level != events.Aviso || !strings.Contains(list[0].Body, "Could not get lock") {
		t.Fatalf("aviso de erro: %+v", list[0])
	}

	// BrunOS reiniciou no meio: o andamento fica gravado para retomar
	t2 := t1 + 100
	setNow(t2)
	f.set(fmt.Sprintf("rodando\n=== registro\ninicio %d atualizar-linux\n", t2))
	if err := c.Start(context.Background(), "linux", ""); err != nil {
		t.Fatal(err)
	}
	if j := New(filepath.Join(dir, "updates.json")).Snapshot().Job; j == nil || !j.Running {
		t.Fatalf("o andamento deveria ficar gravado: %+v", j)
	}
	// termina e espera o acompanhamento sair (antes de o teste restaurar o intervalo)
	f.set(fmt.Sprintf("parado\n=== registro\ninicio %d atualizar-linux\nfim 0\n", t2))
	waitDone(t, c)
	for i := 0; i < 300; i++ {
		c.mu.Lock()
		busy := c.following
		c.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("o acompanhamento não terminou")
}
