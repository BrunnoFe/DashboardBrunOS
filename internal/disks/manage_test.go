package disks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"brunos/internal/events"
	"brunos/internal/hostcmd"
)

// mgrHost imita o script do servidor para montar, desmontar, rótulo e formatar.
type mgrHost struct {
	mu     sync.Mutex
	acts   []string
	input  string
	status string // saída do discos-estado
	fail   error
	unOut  string
}

func (h *mgrHost) query(ctx context.Context, cmd string) ([]byte, error) {
	if !hostcmd.ReadAllowed(cmd) {
		return nil, fmt.Errorf("pedido não permitido: %q", cmd)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if cmd == hostcmd.CmdDiskStatus {
		return []byte(h.status), nil
	}
	return []byte(lsblkSample), nil
}

func (h *mgrHost) act(ctx context.Context, cmd string) ([]byte, error) {
	if !hostcmd.DiskActAllowed(cmd) {
		return nil, fmt.Errorf("pedido não permitido: %q", cmd)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.acts = append(h.acts, cmd)
	if h.fail != nil {
		return nil, h.fail
	}
	if strings.HasPrefix(cmd, hostcmd.CmdUnmount) {
		return []byte(h.unOut), nil
	}
	return []byte("ok\n"), nil
}

func (h *mgrHost) actIn(ctx context.Context, cmd string, in []byte) ([]byte, error) {
	if !hostcmd.FormatAllowed(cmd) {
		return nil, fmt.Errorf("pedido não permitido: %q", cmd)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.acts, h.input = append(h.acts, cmd), string(in)
	if h.fail != nil {
		return nil, h.fail
	}
	return []byte("iniciado\n"), nil
}

func (h *mgrHost) set(f func(*mgrHost)) { h.mu.Lock(); f(h); h.mu.Unlock() }

func newMgr(t *testing.T) (*Monitor, *mgrHost, *events.Store) {
	dir := t.TempDir()
	ev := events.New(filepath.Join(dir, "events.json"))
	h := &mgrHost{}
	m := New(filepath.Join(dir, "disk-events.json"), h.query, h.act, ev)
	m.ActInput = h.actIn
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m, h, ev
}

func TestMountUnmountLabel(t *testing.T) {
	m, h, _ := newMgr(t)
	ctx := context.Background()
	var mapped, unmapped, released, unreleased []string
	m.OnMount = func(f string) { mapped = append(mapped, f) }
	m.OnUnmount = func(f string) { unmapped = append(unmapped, f) }
	m.Release = func(f string) { released = append(released, f) }
	m.Unrelease = func(f string) { unreleased = append(unreleased, f) }
	// montar: só partição desmontada, fora do sistema, com sistema de arquivos
	for _, c := range []struct{ uuid, folder, want string }{
		{"2222-bac0", "Backup", ""},
		{"1111-f070", "X", "já está montada"},
		{"AAAA-1111", "X", "disco do sistema"},
		{"abcd", "X", "não está na lista"},
		{"2222-bac0", "../etc", "nome de pasta inválido"},
		{"uuid backup", "X", "partição inválida"},
	} {
		err := m.Mount(ctx, c.uuid, c.folder)
		if c.want == "" {
			if err != nil {
				t.Errorf("montar %s: %v", c.uuid, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("montar %s: queria %q, veio %v", c.uuid, c.want, err)
		}
	}
	if len(h.acts) != 1 || h.acts[0] != "montar 2222-bac0 Backup" || len(mapped) != 1 || mapped[0] != "Backup" {
		t.Fatalf("montar: %v mapeados=%v", h.acts, mapped)
	}
	// desmontar: solta a pasta antes; o que ainda segura o disco volta para a interface
	h.set(func(h *mgrHost) { h.acts = nil; h.unOut = "fstab tirado\npreso brunos plex\ndesmontado /mnt/disco1\n" })
	res, err := m.Unmount(ctx, "disco1")
	if err != nil || len(res.Stuck) != 2 || res.Stuck[1] != "plex" || res.FstabKept {
		t.Fatalf("desmontar: %+v %v", res, err)
	}
	if len(released) != 1 || released[0] != "disco1" || len(unmapped) != 1 || len(unreleased) != 0 {
		t.Fatalf("soltar/desmapear: %v %v %v", released, unmapped, unreleased)
	}
	// recusado pelo servidor (disco em uso): a pasta volta a abrir e o mapeamento fica
	h.set(func(h *mgrHost) { h.fail = errors.New("o disco está em uso por: plex") })
	if _, err := m.Unmount(ctx, "disco1"); err == nil || !strings.Contains(err.Error(), "plex") {
		t.Fatalf("desmontar recusado: %v", err)
	}
	if len(unreleased) != 1 || len(unmapped) != 1 {
		t.Fatalf("recusado: %v %v", unreleased, unmapped)
	}
	if _, err := m.Unmount(ctx, "nada"); err == nil || !strings.Contains(err.Error(), "não há disco montado") {
		t.Fatalf("desmontar pasta sem disco: %v", err)
	}
	// rótulo: só desmontada
	h.set(func(h *mgrHost) { h.fail = nil; h.acts = nil })
	if err := m.Label(ctx, "1111-f070", "Novo"); err == nil || !strings.Contains(err.Error(), "desmonte") {
		t.Fatalf("rótulo montada: %v", err)
	}
}

func TestFormat(t *testing.T) {
	m, h, ev := newMgr(t)
	ctx := context.Background()
	followStep = 10 * time.Millisecond
	t.Cleanup(func() { followStep = 2 * time.Second })
	for _, c := range []struct{ dev, fs, label, pw, want string }{
		{"sda", "ext4", "Novo", "x", "montado"},        // HD montado
		{"nvme0n1", "ext4", "Novo", "x", "do sistema"}, // disco do sistema
		{"sdd", "ext4", "Novo", "x", "número de série"},
		{"sdb", "fat32", "Novo", "x", "sistema de arquivos"},
		{"sdb", "exfat", "NOMEMUITOLONGO", "x", "rótulo"},
		{"sdb", "ext4", "Novo", "", "Senha de formatação"},
		{"sdb", "ext4", "Novo", "a\nb", "Senha de formatação"},
		{"sdz", "ext4", "Novo", "x", "não está na lista"},
	} {
		if err := m.Format(ctx, c.dev, c.fs, c.label, c.pw); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("formatar %s: queria %q, veio %v", c.dev, c.want, err)
		}
	}
	if len(h.acts) != 0 {
		t.Fatalf("recusas mandaram pedido ao servidor: %v", h.acts)
	}
	// senha errada (o servidor confere): erro amigável e nada fica rodando
	h.set(func(h *mgrHost) { h.fail = errors.New("Senha de formatação errada (tentativa 1 de 5)") })
	if err := m.Format(ctx, "sdb", "ext4", "Novo", "errada"); err == nil || !strings.Contains(err.Error(), "tentativa 1") {
		t.Fatalf("senha errada: %v", err)
	}
	// certo: a senha vai pela entrada padrão, nunca na linha de comando
	h.set(func(h *mgrHost) {
		h.fail = nil
		h.status = fmt.Sprintf("rodando\nsenha sim\n=== registro\ninicio %d sdb ext4 Novo\netapa formatando\n", time.Now().Unix())
	})
	if err := m.Format(ctx, "sdb", "ext4", "Novo", "certa-123"); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	last, in := h.acts[len(h.acts)-1], h.input
	h.mu.Unlock()
	if last != "formatar sdb HD-BBB ext4 Novo" || in != "certa-123\n" || strings.Contains(last, "certa") {
		t.Fatalf("pedido: %q entrada %q", last, in)
	}
	if err := m.Format(ctx, "sdb", "ext4", "Novo", "certa-123"); !errors.Is(err, ErrFormatBusy) {
		t.Fatalf("duas ao mesmo tempo: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if st := m.FormatInfo(ctx); !st.HasPassword || st.Job == nil || !st.Job.Running || st.Job.Step != "formatando" {
		t.Fatalf("andamento: %+v %+v", st, st.Job)
	}
	h.set(func(h *mgrHost) {
		h.status = fmt.Sprintf("parado\nsenha sim\n=== registro\ninicio %d sdb ext4 Novo\netapa formatando\nuuid nova-uuid\nfim 0\n", time.Now().Unix())
	})
	for i := 0; i < 200; i++ {
		if st := m.FormatInfo(ctx); st.Job != nil && !st.Job.Running {
			if !st.Job.OK || st.Job.UUID != "nova-uuid" {
				t.Fatalf("fim: %+v", st.Job)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	list, _ := ev.List()
	found := false
	for _, e := range list {
		found = found || (e.Kind == "disk-formatted" && strings.Contains(e.Title, "Novo"))
	}
	if !found {
		t.Fatalf("sem aviso de disco formatado: %+v", list)
	}
	if tl := m.Timeline("HD-BBB"); len(tl) < 2 || tl[0].Kind != "formatar" || !strings.Contains(tl[0].Text, "Formatado em ext4") {
		t.Fatalf("linha do tempo: %+v", tl)
	}
}

// Registro de uma formatação anterior não vale para a atual; serviço que parou sem "fim" vira erro.
func TestApplyLog(t *testing.T) {
	j := &FormatJob{Started: 1000, Running: true}
	applyLog(j, true, []string{"inicio 500 sdb", "fim 0"})
	if !j.Running {
		t.Fatal("registro antigo não pode terminar a formatação atual")
	}
	applyLog(j, false, []string{"inicio 1001 sdb", "etapa apagando", "wipefs: erro ao abrir /dev/sdb"})
	if j.Running || j.OK || !strings.Contains(j.Err, "wipefs") {
		t.Fatalf("parou sem fim: %+v", j)
	}
	if _, st, _ := parseStatus([]byte("parado\nsenha nao\ntrava 3 0\n=== registro\n")); st.HasPassword || st.Fails != 3 {
		t.Fatalf("estado: %+v", st)
	}
}

var _ = http.MethodGet
