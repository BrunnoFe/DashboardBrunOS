package hostcmd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"brunos/internal/sshknown"
)

// servidor SSH de teste: aceita só a chave autorizada e conta as sessões abertas.
// Cada sessão "executa" o comando forçado (como o OpenSSH faz com command=).
type fakeServer struct {
	host     string
	port     int
	hostKey  ssh.Signer
	sessions atomic.Int32
}

func startServer(t *testing.T, authorized func() ssh.PublicKey) *fakeServer {
	t.Helper()
	_, pk, _ := ed25519.GenerateKey(rand.Reader)
	hk, _ := ssh.NewSignerFromKey(pk)
	fs := &fakeServer{hostKey: hk}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if a := authorized(); a != nil && c.User() == DefaultUser && bytes.Equal(a.Marshal(), k.Marshal()) {
			return nil, nil
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
								r.Reply(true, nil)
								ch.Write([]byte("Shutdown scheduled\n"))
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
	r := New(filepath.Join(dir, "hostcmd_key"), sshknown.New(""))
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
	if !strings.HasPrefix(al, `restrict,command="sudo -n /usr/sbin/shutdown -h +1" ssh-ed25519 `) {
		t.Fatalf("linha do authorized_keys inesperada: %q", al)
	}
}

func TestTestNeverOpensSession(t *testing.T) {
	dir := t.TempDir()
	r := New(filepath.Join(dir, "hostcmd_key"), sshknown.New(filepath.Join(dir, "known_hosts.json")))
	signer, err := r.key()
	if err != nil {
		t.Fatal(err)
	}
	srv := startServer(t, func() ssh.PublicKey { return signer.PublicKey() })
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
	if _, err := r.Run(ctx, tg); !errors.As(err, &unk) {
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
	// 4. Run abre exatamente uma sessão e devolve a saída
	out, err := r.Run(ctx, tg)
	if err != nil || !strings.Contains(out, "Shutdown scheduled") {
		t.Fatalf("Run: %q %v", out, err)
	}
	if n := srv.sessions.Load(); n != 1 {
		t.Fatalf("Run deveria abrir 1 sessão, abriu %d", n)
	}
}

func TestWrongKeyRefused(t *testing.T) {
	dir := t.TempDir()
	r := New(filepath.Join(dir, "hostcmd_key"), sshknown.New(""))
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	otherSigner, _ := ssh.NewSignerFromKey(other)
	srv := startServer(t, func() ssh.PublicKey { return otherSigner.PublicKey() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := r.Test(ctx, Target{Host: srv.host, Port: srv.port}, ssh.FingerprintSHA256(srv.hostKey.PublicKey()))
	if err == nil || !strings.Contains(err.Error(), "authorized_keys") {
		t.Fatalf("chave não autorizada deveria dar erro explicando o authorized_keys, veio %v", err)
	}
	if _, err := r.Run(ctx, Target{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("sem host deveria dar ErrNotConfigured, veio %v", err)
	}
}
