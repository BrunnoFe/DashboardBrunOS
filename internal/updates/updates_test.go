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
	"brunos/internal/hostcmd"
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
	// o caso do servidor do Bruno depois de atualizar: o apt-check diz 0 (desconta a liberação gradual do
	// Ubuntu) e a lista ainda traz os 5 segurados; antes, a lista virava "5 atualizações"
	phased := "apt 0;0\n=== pacotes\ndistro-info/resolute-updates 1.15ubuntu0.1 amd64\npython3-distro-info/x 1\npython3-software-properties/x 1\nsnapd/x 2\nsoftware-properties-common/x 1\n"
	if o := ParseOS([]byte(phased)); o.Total != 0 || o.Phased != 5 || len(o.Packages) != 5 {
		t.Fatalf("liberação gradual: %+v", o)
	}
}

func TestCheckPart(t *testing.T) {
	var mu sync.Mutex
	var acts, queries []string
	have := "brunos-host 8"
	c := New("")
	c.Apps = func() []runtipi.Update { return []runtipi.Update{{URN: "plex:migrated", Name: "Plex"}} }
	c.Query = func(_ context.Context, cmd string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		queries = append(queries, cmd)
		if cmd == "versao" {
			return []byte(have), nil
		}
		return []byte("apt 3;1\n"), nil
	}
	c.Act = func(_ context.Context, cmd string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		acts = append(acts, cmd)
		return []byte("lojas atualizadas\n"), nil
	}
	// só os apps: não pergunta do Linux; script 8 ainda não sabe atualizar as lojas
	if err := c.CheckPart(context.Background(), PartApps); err != nil {
		t.Fatal(err)
	}
	if s := c.Snapshot(); len(s.Apps) != 1 || s.OS.CheckedAt != 0 || len(acts) != 0 || strings.Contains(strings.Join(queries, ","), "atualizacoes") {
		t.Fatalf("só apps: %+v acts=%v queries=%v", s, acts, queries)
	}
	// script 9: atualiza as lojas do Runtipi antes de conferir os apps
	have = "brunos-host 9"
	_ = c.CheckPart(context.Background(), PartApps)
	if s := c.Snapshot(); len(acts) != 1 || acts[0] != "atualizar-lojas" || s.StoresAt == 0 || s.StoresErr != "" {
		t.Fatalf("lojas: %+v acts=%v", s, acts)
	}
	// só o Linux: não mexe nas lojas nem nos apps
	_ = c.CheckPart(context.Background(), PartLinux)
	if s := c.Snapshot(); s.OS.Total != 3 || len(acts) != 1 {
		t.Fatalf("só Linux: %+v acts=%v", s, acts)
	}
	if err := c.CheckPart(context.Background(), "tudo"); !errors.Is(err, ErrPart) {
		t.Fatalf("parte inválida: %v", err)
	}
	// loja que falha: o aviso aparece, e os apps são conferidos mesmo assim
	c.Act = func(context.Context, string) ([]byte, error) { return nil, errors.New("o Runtipi não respondeu") }
	_ = c.CheckPart(context.Background(), PartApps)
	if s := c.Snapshot(); !strings.Contains(s.StoresErr, "lojas do Runtipi") || len(s.Apps) != 1 {
		t.Fatalf("loja falhou: %+v", s)
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
	s := ParseStatus([]byte(out), 1000, false)
	if !s.Running || !s.Started || s.Done || len(s.Lines) != 3 || s.Lines[1] != "Reading package lists..." || s.Lines[2] != "Progress: 90%" {
		t.Fatalf("%+v", s)
	}
	s = ParseStatus([]byte("parado\n=== registro\ninicio 1000 atualizar-app immich:migrated\nok\nfim 0\n"), 1000, false)
	if s.Running || !s.Done || s.Exit != 0 || len(s.Lines) != 1 {
		t.Fatalf("fim: %+v", s)
	}
	// registro de uma atualização antiga (antes do pedido) não conta
	if s := ParseStatus([]byte("parado\n=== registro\ninicio 100 atualizar-linux\nfim 0\n"), 1000, false); s.Started || s.Done || len(s.Lines) != 0 {
		t.Fatalf("antiga: %+v", s)
	}
	// backup do Runtipi antes: a fase aparece; backup que falhou não atualiza
	s = ParseStatus([]byte("rodando\n=== registro\ninicio 1000 atualizar-app immich:migrated sim\nbackup-inicio\nBacking up app...\n"), 1000, false)
	if s.Phase != "backup" || len(s.Lines) != 1 {
		t.Fatalf("fase backup: %+v", s)
	}
	s = ParseStatus([]byte("parado\n=== registro\ninicio 1000 atualizar-app immich:migrated sim\nbackup-inicio\nbackup-ok\nUpdating...\nfim 0\n"), 1000, false)
	if s.Phase != "atualizando" || !s.Done || s.Exit != 0 {
		t.Fatalf("fase atualizando: %+v", s)
	}
	s = ParseStatus([]byte("parado\n=== registro\ninicio 1000 atualizar-app immich:migrated sim\nbackup-inicio\nNo space left on device\nbackup-falhou 1\nfim 1\n"), 1000, false)
	if !s.BackupFailed || s.Exit != 1 {
		t.Fatalf("backup falhou: %+v", s)
	}
	// o caso do servidor do Bruno: o repositório da Ookla quebrado vira aviso, o resto instala
	s = ParseStatus([]byte("parado\n=== registro\ninicio 1000 atualizar-linux\nErr:9 https://packagecloud.io/ookla/speedtest-cli/ubuntu noble Release\nE: The repository 'https://packagecloud.io/ookla/speedtest-cli/ubuntu noble Release' does not have a Release file.\n5 upgraded\naviso-repositorio\nfim 0\n"), 1000, false)
	if !s.Warn || s.Exit != 0 || !strings.Contains(s.Repo, "packagecloud.io/ookla") {
		t.Fatalf("aviso do repositório: %+v", s)
	}
	// o bug da 0.12.1 no servidor do Bruno: registro comprido, o script 7 só mandava as últimas 30
	// linhas e o "inicio" ficava de fora. Rodando ou já visto rodando, o registro é desta atualização.
	var long strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&long, "Unpacking pacote-%d...\n", i)
	}
	run := "rodando\n=== registro\n" + long.String()
	if s := ParseStatus([]byte(run), 1000, false); !s.Started || !s.Running || len(s.Lines) != 15 {
		t.Fatalf("rodando sem o inicio: %+v", s)
	}
	end := "parado\n=== registro\n" + long.String() + "fim 0\n"
	if s := ParseStatus([]byte(end), 1000, true); !s.Done || s.Exit != 0 {
		t.Fatalf("já vista rodando, terminou: %+v", s)
	}
	if s := ParseStatus([]byte(end), 1000, false); s.Started || s.Done {
		t.Fatalf("nunca vista e sem inicio: não dá para saber se é desta: %+v", s)
	}
	// script 8: a primeira linha vem antes do fim do registro
	if s := ParseStatus([]byte("parado\n=== registro\ninicio 1000 atualizar-linux\n"+long.String()+"fim 0\n"), 1000, false); !s.Done || s.Exit != 0 {
		t.Fatalf("script 8 com a primeira linha: %+v", s)
	}
}

func TestExitMessage(t *testing.T) {
	for lines, want := range map[string]string{
		"E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 123": "outra instalação",
		"write error: No space left on device":                                         "espaço",
		"Temporary failure resolving 'archive.ubuntu.com'":                             "sem internet",
		"qualquer outra coisa":                                                         "código 100",
	} {
		if got := exitMessage(100, []string{lines}); !strings.Contains(got, want) {
			t.Errorf("%q → %q (queria %q)", lines, got, want)
		}
	}
}

func TestCheckScript(t *testing.T) {
	ver := "brunos-host 6"
	upgraded := 0
	c := New("")
	c.Query = func(_ context.Context, cmd string) ([]byte, error) {
		if cmd != "versao" {
			t.Fatalf("pedido %q", cmd)
		}
		return []byte(ver + "\n"), nil
	}
	c.UpgradeScript = func(context.Context) error { upgraded++; return nil }
	// script 6 ainda não sabe se atualizar sozinho: fica para o Terminal
	c.CheckScript(context.Background())
	if s := c.Snapshot().Script; s.Have != 6 || s.Want != hostcmd.ScriptVersion || !s.Auto || upgraded != 0 {
		t.Fatalf("script 6: %+v (trocas %d)", s, upgraded)
	}
	// script 7 já sabe se atualizar: troca sozinho pelo desta versão
	ver = "brunos-host 7"
	c.CheckScript(context.Background())
	if s := c.Snapshot().Script; s.Have != hostcmd.ScriptVersion || s.From != 7 || upgraded != 1 || s.Err != "" {
		t.Fatalf("script 7 → atual: %+v (trocas %d)", s, upgraded)
	}
	// em dia: nada a fazer
	ver = fmt.Sprintf("brunos-host %d", hostcmd.ScriptVersion)
	c.CheckScript(context.Background())
	if s := c.Snapshot().Script; s.Have != hostcmd.ScriptVersion || upgraded != 1 {
		t.Fatalf("script em dia: %+v", s)
	}
	// a troca falhou: o erro aparece e o sino avisa
	ver = "brunos-host 7"
	c.UpgradeScript = func(context.Context) error {
		return errors.New("assinatura inválida: este script não foi publicado pelo BrunOS")
	}
	c.CheckScript(context.Background())
	if s := c.Snapshot().Script; s.Have != 7 || !strings.Contains(s.Err, "sozinho") {
		t.Fatalf("troca falhou: %+v", s)
	}
	// sem conexão: o erro aparece em português
	c.Query = func(context.Context, string) ([]byte, error) { return nil, errors.New("pedido inválido") }
	c.CheckScript(context.Background())
	if s := c.Snapshot().Script; s.Have != 0 || !strings.Contains(s.Err, "Configurações → Energia") {
		t.Fatalf("erro: %+v", s)
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

	if err := c.Start(context.Background(), "app", "plex:migrated", false); !errors.Is(err, ErrNoUpdate) {
		t.Fatalf("app sem atualização: %v", err)
	}
	if err := c.Start(context.Background(), "tudo", "", false); !errors.Is(err, ErrKind) {
		t.Fatalf("tipo inválido: %v", err)
	}
	t0 := now
	f.set(fmt.Sprintf("rodando\n=== registro\ninicio %d atualizar-app immich:migrated\nPulling images...\n", t0))
	if err := c.Start(context.Background(), "app", "immich:migrated", true); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background(), "linux", "", false); !errors.Is(err, ErrBusy) {
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
	if first != "atualizar-app immich:migrated sim" {
		t.Fatalf("pedido: %v", first)
	}

	// Linux com erro: aviso com a última linha
	t1 := t0 + 100
	setNow(t1)
	f.set(fmt.Sprintf("parado\n=== registro\ninicio %d atualizar-linux\nE: Could not get lock /var/lib/dpkg/lock-frontend\nfim 100\n", t1))
	if err := c.Start(context.Background(), "linux", "", false); err != nil {
		t.Fatal(err)
	}
	if j := waitDone(t, c); j.OK || !strings.Contains(j.Err, "outra instalação está rodando") {
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
	if err := c.Start(context.Background(), "linux", "", false); err != nil {
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
