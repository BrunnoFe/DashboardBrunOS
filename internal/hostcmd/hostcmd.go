// Package hostcmd executa ações que precisam de root no servidor (hoje: desligar) sem dar
// nenhum privilégio ao container. O BrunOS tem uma chave SSH própria (/data/hostcmd_key) e
// entra num usuário dedicado do servidor (brunos-power) cuja linha no authorized_keys força
// um único comando:
//
//	restrict,command="sudo -n /usr/sbin/shutdown -h +1" ssh-ed25519 AAAA... brunos-power
//
// Pegadinha: com command= forçado, QUALQUER sessão aberta com essa chave roda o comando.
// Por isso Test só autentica e fecha a conexão SEM abrir sessão; só Run abre uma sessão.
package hostcmd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"brunos/internal/sshknown"
	"brunos/internal/store"
)

const (
	DefaultUser = "brunos-power"
	// ShutdownCmd é o comando que o authorized_keys força (1 minuto de folga: dá para
	// cancelar no servidor com "sudo shutdown -c").
	ShutdownCmd = "sudo -n /usr/sbin/shutdown -h +1"
	dialTimeout = 10 * time.Second
	keyComment  = "brunos-power"
)

var ErrNotConfigured = errors.New("o desligamento ainda não foi configurado (falta o endereço do servidor)")

type Target struct {
	Host string
	Port int
	User string
}

func (t Target) addr() string { return sshknown.Addr(t.Host, t.Port) }

type Runner struct {
	KeyFile string
	Known   *sshknown.Store

	mu     sync.Mutex
	signer ssh.Signer
}

func New(keyFile string, known *sshknown.Store) *Runner {
	return &Runner{KeyFile: keyFile, Known: known}
}

// key lê a chave ou cria uma nova (ED25519, gravada com permissão 0600).
func (r *Runner) key() (ssh.Signer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.signer != nil {
		return r.signer, nil
	}
	if b, err := os.ReadFile(r.KeyFile); err == nil {
		s, err := ssh.ParsePrivateKey(b)
		if err != nil {
			return nil, fmt.Errorf("a chave em %s está ilegível: %w", r.KeyFile, err)
		}
		r.signer = s
		return s, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, keyComment)
	if err != nil {
		return nil, err
	}
	if err := store.WriteFile(r.KeyFile, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	r.signer = s
	return s, nil
}

// HasKey diz se a chave já foi criada (sem criá-la).
func (r *Runner) HasKey() bool {
	_, err := os.Stat(r.KeyFile)
	return err == nil
}

// PublicKey devolve a chave pública no formato do authorized_keys (sem quebra de linha)
// e a impressão digital. Cria a chave se ainda não existir.
func (r *Runner) PublicKey() (line, fingerprint string, err error) {
	s, err := r.key()
	if err != nil {
		return "", "", err
	}
	pub := s.PublicKey()
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " " + keyComment, ssh.FingerprintSHA256(pub), nil
}

// AuthorizedLine é a linha completa para o authorized_keys do usuário dedicado.
func (r *Runner) AuthorizedLine() (string, error) {
	line, _, err := r.PublicKey()
	if err != nil {
		return "", err
	}
	return `restrict,command="` + ShutdownCmd + `" ` + line, nil
}

func (r *Runner) dial(ctx context.Context, t Target, trust string) (*ssh.Client, error) {
	if strings.TrimSpace(t.Host) == "" {
		return nil, ErrNotConfigured
	}
	if t.User == "" {
		t.User = DefaultUser
	}
	signer, err := r.key()
	if err != nil {
		return nil, err
	}
	addr := t.addr()
	known, _ := r.Known.Load()
	keyOK := false
	check := r.Known.Callback(addr, trust, known)
	cfg := &ssh.ClientConfig{
		User:    t.User,
		Auth:    []ssh.AuthMethod{ssh.PublicKeys(signer)},
		Timeout: dialTimeout,
		HostKeyCallback: func(h string, a net.Addr, k ssh.PublicKey) error {
			err := check(h, a, k)
			keyOK = err == nil
			return err
		},
		HostKeyAlgorithms: sshknown.Algorithms(known, addr),
		ClientVersion:     "SSH-2.0-BrunOS",
	}
	d := net.Dialer{Timeout: dialTimeout}
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, sshknown.FriendlyDialError(err, addr)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = nc.SetDeadline(dl)
	}
	c, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		nc.Close()
		if keyOK && !sshknown.IsNetErr(err) {
			return nil, fmt.Errorf("o servidor recusou a chave do BrunOS para o usuário %s: confira o authorized_keys", t.User)
		}
		return nil, sshknown.FriendlyDialError(err, addr)
	}
	_ = nc.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// Test confere a conexão e a chave: conecta, autentica e fecha SEM abrir sessão
// (abrir sessão executaria o comando forçado, ou seja, desligaria o servidor).
// trust é a impressão digital do servidor confirmada pelo usuário na primeira vez.
func (r *Runner) Test(ctx context.Context, t Target, trust string) error {
	c, err := r.dial(ctx, t, trust)
	if err != nil {
		return err
	}
	return c.Close()
}

// Run abre uma sessão e executa o comando forçado. Só aceita servidor já confirmado.
func (r *Runner) Run(ctx context.Context, t Target) (string, error) {
	c, err := r.dial(ctx, t, "")
	if err != nil {
		return "", err
	}
	defer c.Close()
	sess, err := c.NewSession()
	if err != nil {
		return "", fmt.Errorf("o servidor recusou abrir a sessão: %w", err)
	}
	defer sess.Close()
	// buffers separados: as duas cópias rodam ao mesmo tempo
	var stdout, stderr bytes.Buffer
	sess.Stdout, sess.Stderr = &stdout, &stderr
	done := make(chan error, 1)
	go func() { done <- sess.Run("brunos-power") }() // o texto é ignorado: o authorized_keys decide o comando
	select {
	case err = <-done:
	case <-ctx.Done():
		err = ctx.Err()
	}
	text := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	if len(text) > 2000 {
		text = text[:2000]
	}
	if err != nil {
		var exit *ssh.ExitError
		if errors.As(err, &exit) {
			return text, fmt.Errorf("o comando saiu com código %d: %s", exit.ExitStatus(), text)
		}
		return text, err
	}
	return text, nil
}
