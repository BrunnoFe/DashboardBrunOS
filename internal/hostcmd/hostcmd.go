// Package hostcmd executa ações que precisam de root no servidor (desligar e limite de carga
// da bateria) sem dar nenhum privilégio ao container. O BrunOS tem uma chave SSH própria e
// entra num usuário dedicado do servidor (brunos-power) cuja linha no authorized_keys força
// um único programa, o script brunos-host (HostScript), que só aceita alguns pedidos:
//
//	restrict,command="/usr/local/sbin/brunos-host" ssh-ed25519 AAAA... brunos-power
//
// O script lê o pedido (SSH_ORIGINAL_COMMAND), recusa tudo o que não for "desligar",
// "limite 50..100", "protecao on|off" ou "versao", e chama a si mesmo pelo sudo (o sudoers
// libera só ele), conferindo o pedido de novo já como root.
//
// Por que systemctl e não "shutdown -h +1" para desligar: desde o systemd 257 (o Ubuntu 26.04
// usa o 259) uma trava de desligamento ("block inhibitor") vale até para o root. O
// unattended-upgrades segura uma o tempo todo, e o "shutdown -h +1" era recusado.
// --check-inhibitors=no passa por cima da trava: numa queda de energia, desligar é mais seguro
// que esperar a bateria acabar.
//
// Duas chaves: até a 0.8.0 o authorized_keys forçava "sudo -n /usr/sbin/shutdown -h +1" com a
// chave antiga (/data/hostcmd_key). Com command= forçado, QUALQUER sessão roda o comando: mandar
// "limite 80" para um servidor ainda não atualizado o desligaria. Por isso a configuração nova
// usa outra chave (/data/hostcmd_key_v2): o servidor não atualizado recusa a chave nova já na
// autenticação, sem executar nada, e o teste de conexão (que só autentica) prova que o servidor
// foi atualizado. A chave antiga só é usada para desligar, enquanto o servidor não é atualizado.
//
// Test só autentica e fecha a conexão SEM abrir sessão; só Run abre uma sessão.
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
	// ScriptPath é onde os comandos de configuração instalam o script no servidor.
	ScriptPath = "/usr/local/sbin/brunos-host"
	// SetupVersion sobe quando o que se cola no servidor muda.
	// 1 = shutdown -h +1 com a chave antiga (até a 0.8.0); 2 = script brunos-host com a chave nova.
	SetupVersion = 2
	dialTimeout  = 10 * time.Second
	keyComment   = "brunos-power"
)

// Pedidos aceitos pelo script do servidor.
const (
	CmdShutdown = "desligar"
	CmdVersion  = "versao"
)

// CmdLimit e CmdProtect montam os pedidos do limite de carga (o script confere de novo).
func CmdLimit(pct int) string { return fmt.Sprintf("limite %d", pct) }
func CmdProtect(on bool) string {
	if on {
		return "protecao on"
	}
	return "protecao off"
}

// HostScript é o script instalado em ScriptPath. Indentado com espaços (não tabs): colado
// num terminal, um tab poderia virar autocompletar.
const HostScript = `#!/bin/sh
# brunos-host 2: o único programa que a chave do BrunOS pode rodar neste servidor.
# Aceita só: desligar | limite <50-100> | protecao <on|off> | versao. Recusa todo o resto.
# Instalado pelos comandos de Configurações -> Energia do BrunOS.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
if [ "$(id -u)" -ne 0 ]; then
  # chamado pelo SSH (usuário brunos-power): o pedido vem em SSH_ORIGINAL_COMMAND
  set -f
  set -- ${SSH_ORIGINAL_COMMAND:-}
  set +f
fi
falha() { echo "$1" >&2; exit "${2:-2}"; }
if [ $# -lt 1 ] || [ $# -gt 2 ]; then falha "pedido inválido"; fi
acao=$1
valor=${2:-}
case "$acao" in
  versao) [ $# -eq 1 ] || falha "pedido inválido"; echo "brunos-host 2"; exit 0 ;;
  desligar) [ $# -eq 1 ] || falha "pedido inválido" ;;
  limite) case "$valor" in [5-9][0-9]|100) ;; *) falha "limite inválido (use de 50 a 100)" ;; esac ;;
  protecao) case "$valor" in on|off) ;; *) falha "valor inválido (use on ou off)" ;; esac ;;
  *) falha "pedido inválido" ;;
esac
if [ "$(id -u)" -ne 0 ]; then
  if [ -n "$valor" ]; then exec sudo -n /usr/local/sbin/brunos-host "$acao" "$valor"; fi
  exec sudo -n /usr/local/sbin/brunos-host "$acao"
fi
# daqui para baixo roda como root (pelo sudo), com o pedido já conferido duas vezes
baterias() {
  for b in /sys/class/power_supply/*; do
    if [ "$(cat "$b/type" 2>/dev/null)" = Battery ]; then echo "$b"; fi
  done
}
feito=0
case "$acao" in
  desligar)
    exec systemctl poweroff --check-inhibitors=no --when=+1min ;;
  limite)
    for b in $(baterias); do
      fim=$b/charge_control_end_threshold
      ini=$b/charge_control_start_threshold
      [ -e "$fim" ] || continue
      if [ -e "$ini" ]; then
        # a carga volta a subir 5 pontos abaixo do limite; a ordem evita início >= fim
        if [ "$valor" -ge "$(cat "$fim")" ]; then
          echo "$valor" >"$fim" || falha "o notebook recusou o limite de $valor%" 1
          echo $((valor - 5)) >"$ini" 2>/dev/null || true
        else
          echo $((valor - 5)) >"$ini" 2>/dev/null || true
          echo "$valor" >"$fim" || falha "o notebook recusou o limite de $valor%" 1
        fi
      else
        echo "$valor" >"$fim" || falha "o notebook recusou o limite de $valor%" 1
      fi
      feito=1
      echo "limite $(cat "$fim")"
    done
    [ "$feito" = 1 ] || falha "este notebook não tem limite de carga" 3 ;;
  protecao)
    if [ "$valor" = on ]; then tipo=Long_Life; num=1; else tipo=Standard; num=0; fi
    for b in $(baterias); do
      t=$b/charge_types
      [ -e "$t" ] && grep -q Long_Life "$t" || continue
      echo "$tipo" >"$t" || falha "o notebook recusou a proteção de carga" 1
      feito=1
    done
    if [ "$feito" = 0 ]; then
      for c in /sys/bus/platform/drivers/ideapad_acpi/*/conservation_mode; do
        [ -e "$c" ] || continue
        echo "$num" >"$c" || falha "o notebook recusou a proteção de carga" 1
        feito=1
      done
    fi
    [ "$feito" = 1 ] || falha "este notebook não tem proteção de carga" 3
    echo "protecao $valor" ;;
esac
`

var (
	ErrNotConfigured = errors.New("o desligamento ainda não foi configurado (falta o endereço do servidor)")
	ErrNoLegacyKey   = errors.New("o servidor ainda não foi atualizado e a chave antiga do BrunOS não existe mais")
)

type Target struct {
	Host string
	Port int
	User string
}

func (t Target) addr() string { return sshknown.Addr(t.Host, t.Port) }

type Runner struct {
	KeyFile    string // chave atual (criada sob demanda)
	LegacyFile string // chave da configuração antiga (só lida, nunca criada)
	Known      *sshknown.Store

	mu     sync.Mutex
	signer ssh.Signer
}

func New(keyFile, legacyFile string, known *sshknown.Store) *Runner {
	return &Runner{KeyFile: keyFile, LegacyFile: legacyFile, Known: known}
}

// key lê a chave atual ou cria uma nova (ED25519, gravada com permissão 0600).
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

// legacyKey lê a chave da configuração antiga (até a 0.8.0), se ainda existir.
func (r *Runner) legacyKey() (ssh.Signer, error) {
	if r.LegacyFile == "" {
		return nil, ErrNoLegacyKey
	}
	b, err := os.ReadFile(r.LegacyFile)
	if err != nil {
		return nil, ErrNoLegacyKey
	}
	return ssh.ParsePrivateKey(b)
}

// HasKey diz se a chave atual já foi criada (sem criá-la).
func (r *Runner) HasKey() bool {
	_, err := os.Stat(r.KeyFile)
	return err == nil
}

// PublicKey devolve a chave pública atual no formato do authorized_keys (sem quebra de linha)
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
	return `restrict,command="` + ScriptPath + `" ` + line, nil
}

func (r *Runner) dial(ctx context.Context, t Target, trust string, signer ssh.Signer) (*ssh.Client, error) {
	if strings.TrimSpace(t.Host) == "" {
		return nil, ErrNotConfigured
	}
	if t.User == "" {
		t.User = DefaultUser
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
			return nil, fmt.Errorf("o servidor recusou a chave do BrunOS para o usuário %s: cole os comandos de Configurações → Energia no servidor", t.User)
		}
		return nil, sshknown.FriendlyDialError(err, addr)
	}
	_ = nc.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// Test confere a conexão e a chave atual: conecta, autentica e fecha SEM abrir sessão
// (abrir sessão executaria o programa forçado). Passar prova que o servidor tem a linha da
// chave atual, ou seja, a configuração da versão SetupVersion.
// trust é a impressão digital do servidor confirmada pelo usuário na primeira vez.
func (r *Runner) Test(ctx context.Context, t Target, trust string) error {
	signer, err := r.key()
	if err != nil {
		return err
	}
	c, err := r.dial(ctx, t, trust, signer)
	if err != nil {
		return err
	}
	return c.Close()
}

// Run abre uma sessão e faz um pedido ao script do servidor (CmdShutdown, CmdLimit...).
// Só aceita servidor já confirmado. legacy = servidor ainda com a configuração antiga: usa a
// chave antiga e só aceita CmdShutdown (lá, qualquer sessão desliga).
func (r *Runner) Run(ctx context.Context, t Target, cmd string, legacy bool) (string, error) {
	var signer ssh.Signer
	var err error
	if legacy {
		if cmd != CmdShutdown {
			return "", errors.New("atualize a configuração do servidor em Configurações → Energia")
		}
		signer, err = r.legacyKey()
	} else {
		signer, err = r.key()
	}
	if err != nil {
		return "", err
	}
	c, err := r.dial(ctx, t, "", signer)
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
	go func() { done <- sess.Run(cmd) }() // vira o SSH_ORIGINAL_COMMAND que o script confere
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
