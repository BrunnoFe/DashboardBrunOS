package hostcmd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"brunos/internal/sshknown"
)

// servidor SSH de teste: aceita só as chaves autorizadas, conta as sessões abertas e guarda
// o último pedido (o que o OpenSSH entregaria ao programa forçado em SSH_ORIGINAL_COMMAND).
type fakeServer struct {
	host     string
	port     int
	hostKey  ssh.Signer
	sessions atomic.Int32
	mu       sync.Mutex
	last     string
}

func (f *fakeServer) lastCmd() string { f.mu.Lock(); defer f.mu.Unlock(); return f.last }

func startServer(t *testing.T, authorized func() []ssh.PublicKey) *fakeServer {
	t.Helper()
	_, pk, _ := ed25519.GenerateKey(rand.Reader)
	hk, _ := ssh.NewSignerFromKey(pk)
	fs := &fakeServer{hostKey: hk}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		for _, a := range authorized() {
			if c.User() == DefaultUser && bytes.Equal(a.Marshal(), k.Marshal()) {
				return nil, nil
			}
		}
		return nil, errors.New("negado")
	}}
	cfg.AddHostKey(hk)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					ch, creqs, _ := nch.Accept()
					fs.sessions.Add(1)
					go func() {
						for r := range creqs {
							if r.Type == "exec" {
								var p struct{ Cmd string }
								ssh.Unmarshal(r.Payload, &p)
								fs.mu.Lock()
								fs.last = p.Cmd
								fs.mu.Unlock()
								r.Reply(true, nil)
								ch.Write([]byte("ok " + p.Cmd + "\n"))
								ch.CloseWrite()
								ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{0}))
								ch.Close()
								return
							}
							r.Reply(false, nil)
						}
					}()
				}
			}()
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	fs.host, fs.port = a.IP.String(), a.Port
	return fs
}

func TestKeyCreatedOnce(t *testing.T) {
	dir := t.TempDir()
	r := New(filepath.Join(dir, "hostcmd_key_v2"), sshknown.New(""))
	if r.HasKey() {
		t.Fatal("não deveria existir chave antes de pedir")
	}
	line1, fp1, err := r.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line1, "ssh-ed25519 ") || !strings.HasSuffix(line1, " brunos-power") {
		t.Fatalf("linha da chave inesperada: %q", line1)
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(r.KeyFile)
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("chave deveria ter permissão 0600, tem %v", st.Mode().Perm())
		}
	}
	// outra instância lê a mesma chave do disco
	r2 := New(r.KeyFile, sshknown.New(""))
	_, fp2, _ := r2.PublicKey()
	if fp1 != fp2 {
		t.Fatal("a chave deveria ser reaproveitada, não recriada")
	}
	al, _ := r.AuthorizedLine()
	if !strings.HasPrefix(al, `restrict,command="/usr/local/sbin/brunos-host" ssh-ed25519 `) {
		t.Fatalf("linha do authorized_keys inesperada: %q", al)
	}
}

func TestTestNeverOpensSession(t *testing.T) {
	dir := t.TempDir()
	r := New(filepath.Join(dir, "hostcmd_key_v2"), sshknown.New(filepath.Join(dir, "known_hosts.json")))
	signer, err := r.key()
	if err != nil {
		t.Fatal(err)
	}
	srv := startServer(t, func() []ssh.PublicKey { return []ssh.PublicKey{signer.PublicKey()} })
	tg := Target{Host: srv.host, Port: srv.port}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. servidor desconhecido: pede confirmação
	err = r.Test(ctx, tg, "")
	var unk *sshknown.UnknownHostError
	if !errors.As(err, &unk) || unk.Fingerprint != ssh.FingerprintSHA256(srv.hostKey.PublicKey()) {
		t.Fatalf("esperava UnknownHostError, veio %v", err)
	}
	// 2. Run também recusa servidor não confirmado (nunca confia sozinho)
	if _, err := r.Run(ctx, tg, CmdShutdown); !errors.As(err, &unk) {
		t.Fatalf("Run com servidor desconhecido deveria recusar, veio %v", err)
	}
	// 3. confirmado: autentica e NÃO abre sessão
	if err := r.Test(ctx, tg, unk.Fingerprint); err != nil {
		t.Fatalf("teste deveria passar: %v", err)
	}
	if err := r.Test(ctx, tg, ""); err != nil {
		t.Fatalf("segundo teste deveria usar a chave guardada: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if n := srv.sessions.Load(); n != 0 {
		t.Fatalf("o teste abriu %d sessão(ões): isso desligaria o servidor de verdade", n)
	}
	// 4. Run abre exatamente uma sessão e entrega o pedido ao programa forçado
	out, err := r.Run(ctx, tg, CmdLimit(80))
	if err != nil || !strings.Contains(out, "ok limite 80") || srv.lastCmd() != "limite 80" {
		t.Fatalf("Run: %q %v (pedido %q)", out, err, srv.lastCmd())
	}
	if n := srv.sessions.Load(); n != 1 {
		t.Fatalf("Run deveria abrir 1 sessão, abriu %d", n)
	}
}

// Servidor ainda com a configuração antiga (só aceita a chave antiga): a chave atual é recusada na
// autenticação e nenhuma sessão é aberta (o servidor antigo desligaria com qualquer sessão).
func TestWrongKeyRefused(t *testing.T) {
	dir := t.TempDir()
	r := New(filepath.Join(dir, "hostcmd_key_v2"), sshknown.New(""))
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	otherSigner, _ := ssh.NewSignerFromKey(other)
	srv := startServer(t, func() []ssh.PublicKey { return []ssh.PublicKey{otherSigner.PublicKey()} })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := r.Test(ctx, Target{Host: srv.host, Port: srv.port}, ssh.FingerprintSHA256(srv.hostKey.PublicKey()))
	if err == nil || !strings.Contains(err.Error(), "Energia") {
		t.Fatalf("chave não autorizada deveria dar erro explicando o que fazer, veio %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if n := srv.sessions.Load(); n != 0 {
		t.Fatalf("nenhuma sessão deveria ter sido aberta, abriu %d", n)
	}
	if _, err := r.Run(ctx, Target{}, CmdShutdown); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("sem host deveria dar ErrNotConfigured, veio %v", err)
	}
}

// O script do servidor recusa qualquer pedido fora da lista antes de chegar ao sudo.
// Roda o script de verdade com o sh (só onde existe sh; no CI, Linux).
func TestHostScriptRefuses(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil || runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("precisa de sh e de um usuário comum")
	}
	script := filepath.Join(t.TempDir(), "brunos-host")
	os.WriteFile(script, []byte(HostScript), 0o755)
	run := func(req string) (string, int) {
		c := exec.Command(sh, script)
		c.Env = []string{"SSH_ORIGINAL_COMMAND=" + req}
		out, _ := c.CombinedOutput()
		return string(out), c.ProcessState.ExitCode()
	}
	for _, bad := range []string{"", "rm -rf /", "limite", "limite 49", "limite 101", "limite 8O", "limite 80 90",
		"limite 80; reboot", "limite $(id)", "limite *", "protecao talvez", "desligar agora", "versao x", "sh", "limite -1",
		"tailscale up", "tailscale down", "tailscale status --json; reboot",
		"smart /dev/sda", "smart; reboot", "temperatura sda", "energia 1", "atualizacoes sim", "apt upgrade",
		"smart", "smart sda", "smart sda xyz", "smart ../sda sat", "smart /dev/sda sat", "smart sda sat x",
		"smart sda1 sat", "smart nvme0n1p1 auto", "smart sdabc sat", "smart SDA sat", "smart sd* sat",
		"temperatura sda -d", "temperatura sda sat;reboot", "discos sda", "discos; reboot", "protecao on off",
		"desligar agora já",
		"executar atualizar-linux", "executar atualizar-app immich:migrated", "atualizar-app", "atualizar-app immich",
		"atualizar-app Immich:migrated", "atualizar-app a:b:c", "atualizar-app ../x:y", "atualizar-app -x:y",
		"atualizar-app immich:migrated;reboot", "atualizar-app immich:migrated x", "atualizar-linux agora",
		"atualizacao-estado x", "atualizar-app $(id):x", "atualizar-app immich:migrated", "atualizar-app immich:migrated talvez",
		"tamanho-backup", "tamanho-backup ../x:y", "tamanho-backup immich", "tamanho-backup immich:migrated x",
		"atualizar-script", "atualizar-script abc;def", "atualizar-script a b", "atualizar-script $(id)",
		"atualizar-lojas x", "atualizar-lojas; reboot",
		// script 10 (janela Discos)
		"discos-detalhe sda", "discos-detalhe; reboot", "estado", "estado sda1", "estado /dev/sda", "estado sda x", "estado sd*",
		"erros", "erros ../sda", "erros sda;reboot", "erros nvme0n1p1", "velocidade", "velocidade sda1", "velocidade sda 2048",
		"velocidade /dev/sda", "ejetar", "ejetar sda1", "ejetar sda agora", "ejetar $(id)", "smart-teste", "smart-teste sda",
		"smart-teste sda sat", "smart-teste sda sat medio", "smart-teste sda xyz curto", "smart-teste sda1 sat curto",
		"smart-teste sda sat curto x", "smart-teste sda sat longo;reboot",
		// script 11 (montar, desmontar, rótulo, formatar)
		"montar", "montar abcd", "montar abcd ../etc", "montar abcd x/y", "montar ab;cd disco", "montar $(id) disco",
		"montar abcd disco x", "montar zzzz disco", "desmontar", "desmontar ../etc", "desmontar a b", "desmontar disco/../x",
		"rotulo abcd", "rotulo abcd nome;reboot", "rotulo abcd nome-comprido-demais-xx", "formatar", "formatar sda S1 ext4",
		"formatar sda1 S1 ext4 X", "formatar sda S1 fat32 X", "formatar sda S;1 ext4 X", "formatar sda S1 exfat NOMEMUITOLONGO",
		"formatar-executar sda S1 ext4 X", "discos-estado x", "senha-formatar", "esquecer-senha-formatar"} {
		if out, code := run(bad); code != 2 {
			t.Errorf("pedido %q deveria ser recusado com código 2, veio %d: %s", bad, code, out)
		}
	}
	// pedidos válidos passam da conferência (como usuário comum vão para o sudo, que falha aqui)
	for _, ok := range []string{"smart sda sat", "smart sdab auto", "temperatura nvme0n1 auto", "discos", "limite 80", "protecao off",
		"atualizar-linux", "atualizar-app immich:migrated sim", "atualizar-app nextcloud_mini:brunnofe-store nao", "atualizacao-estado",
		"tamanho-backup immich:migrated", "atualizar-script QUJDRA==", "atualizar-lojas",
		"discos-detalhe", "estado sdb", "erros nvme0n1", "velocidade sdab", "ejetar sdc", "smart-teste sda sat curto", "smart-teste nvme0n1 auto longo",
		"montar 1a2b-3C4D Disco_4", "desmontar Disco4", "rotulo 1a2b fotos", "formatar sdb SN-123_A.b:c ext4 Fotos", "formatar nvme0n1 X btrfs b",
		"discos-estado"} {
		if out, code := run(ok); code == 2 || strings.Contains(out, "inválido") {
			t.Errorf("pedido %q deveria passar da conferência, veio %d: %s", ok, code, out)
		}
	}
	// a Senha de formatação nunca pela chave do BrunOS (a mensagem explica o caminho certo)
	if out, _ := run("senha-formatar"); !strings.Contains(out, "Terminal do servidor") {
		t.Errorf("senha-formatar pelo SSH: %s", out)
	}
	if out, code := run("versao"); code != 0 || !strings.Contains(out, "brunos-host 11") || ParseVersion([]byte(out)) != ScriptVersion {
		t.Fatalf("versao: %d %q", code, out)
	}
	// "executar" só como root dentro do systemd: com INVOCATION_ID mas sem ser root, recusa
	c := exec.Command(sh, script, "executar", "atualizar-linux")
	c.Env = []string{"INVOCATION_ID=abc"}
	if out, _ := c.CombinedOutput(); c.ProcessState.ExitCode() != 2 {
		t.Fatalf("executar sem ser root: %d %s", c.ProcessState.ExitCode(), out)
	}
	if strings.Contains(HostScript, "\t") {
		t.Fatal("o script não pode ter tabs (colado no terminal, vira autocompletar)")
	}
}

func TestReadAllowed(t *testing.T) {
	for cmd, want := range map[string]bool{
		"discos": true, "smart sda sat": true, "smart nvme0n1 auto": true, "temperatura sdab sat": true,
		"tailscale": true, "atualizacoes": true, "energia": true,
		"smart": false, "smart sda": false, "smart sda -a": false, "smart /dev/sda sat": false, "smart sda1 sat": false,
		"discos sda": false, "desligar": false, "limite 80": false, "": false, "tailscale up": false,
		"versao": true, "tamanho-backup immich:migrated": true, "tamanho-backup immich": false, "tamanho-backup a:b x": false,
		"atualizar-script X": false,
		"discos-detalhe":     true, "discos-detalhe x": false, "estado sdb": true, "estado sdb1": false, "estado": false,
		"erros nvme0n1": true, "erros nvme0n1p2": false, "velocidade sda": false, "ejetar sda": false, "smart-teste sda sat curto": false,
	} {
		if got := ReadAllowed(cmd); got != want {
			t.Errorf("ReadAllowed(%q) = %v, queria %v", cmd, got, want)
		}
	}
	if DiskCmd(CmdSmart, "sdb", ModeSAT) != "smart sdb sat" {
		t.Fatal(DiskCmd(CmdSmart, "sdb", ModeSAT))
	}
	for cmd, want := range map[string]bool{
		"atualizar-linux": true, "atualizacao-estado": true, "atualizar-app immich:migrated sim": true, "atualizar-lojas": true, "atualizar-lojas x": false,
		"atualizar-app nextcloud_mini:brunnofe-store nao": true, "atualizar-app immich:migrated": false, "atualizar-app immich:migrated x": false,
		"atualizar-app": false, "atualizar-app immich": false,
		"atualizar-app Immich:x": false, "atualizar-app a:b:c": false, "atualizar-linux x": false, "executar atualizar-linux": false,
		"desligar": false, "smart sda sat": false,
	} {
		if got := ActAllowed(cmd); got != want {
			t.Errorf("ActAllowed(%q) = %v, queria %v", cmd, got, want)
		}
	}
	for cmd, want := range map[string]bool{
		"velocidade sda": true, "velocidade nvme0n1": true, "ejetar sdb": true, "smart-teste sda sat curto": true, "smart-teste nvme0n1 auto longo": true,
		"velocidade": false, "velocidade sda1": false, "velocidade sda x": false, "ejetar": false, "ejetar /dev/sdb": false,
		"smart-teste sda sat": false, "smart-teste sda sat medio": false, "smart-teste sda xyz curto": false, "smart-teste sda1 sat curto": false,
		"estado sda": false, "desligar": false, "atualizar-linux": false, "": false,
	} {
		if got := DiskActAllowed(cmd); got != want {
			t.Errorf("DiskActAllowed(%q) = %v, queria %v", cmd, got, want)
		}
	}
	for cmd, want := range map[string]bool{
		"montar 1a2b-3c4d Disco4": true, "desmontar Disco4": true, "rotulo abcd Fotos": true,
		"montar abcd ../x": false, "montar ab;cd x": false, "desmontar a/b": false, "rotulo abcd nome;x": false,
		"formatar sdb S1 ext4 X": false, // formatar só pelo FormatAllowed (precisa da senha pela entrada padrão)
	} {
		if got := DiskActAllowed(cmd); got != want {
			t.Errorf("DiskActAllowed(%q) = %v, queria %v", cmd, got, want)
		}
	}
	for cmd, want := range map[string]bool{
		"formatar sdb S1 ext4 Fotos": true, "formatar nvme0n1 SN.1:2 exfat CURTO": true, "formatar sdb S1 exfat NOMEMUITOLONGO": false,
		"formatar sdb1 S1 ext4 X": false, "formatar sdb S1 fat32 X": false, "formatar sdb S1 ext4": false, "formatar sdb S;1 ext4 X": false,
	} {
		if got := FormatAllowed(cmd); got != want {
			t.Errorf("FormatAllowed(%q) = %v, queria %v", cmd, got, want)
		}
	}
}
