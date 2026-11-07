// Package sshknown guarda as chaves dos servidores SSH já confirmados (TOFU) em
// /data/known_hosts.json. É usado pelo terminal e pelos comandos no host (desligamento).
//
// Na primeira conexão o usuário confirma a impressão digital; depois disso, se a chave
// do servidor mudar, a conexão é recusada até alguém "esquecer" a chave antiga.
package sshknown

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"brunos/internal/store"
)

const fileVersion = 1

// UnknownHostError: primeira conexão a este servidor; peça para o usuário confirmar a impressão digital.
type UnknownHostError struct{ Addr, KeyType, Fingerprint string }

func (e *UnknownHostError) Error() string { return "servidor ainda não confirmado" }

// HostKeyMismatchError: a chave do servidor mudou desde a última vez.
type HostKeyMismatchError struct{ Addr, KeyType, Fingerprint, Known string }

func (e *HostKeyMismatchError) Error() string {
	return "a chave deste servidor mudou desde a última conexão; a conexão foi recusada"
}

type Host struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Key         string `json:"key"` // chave pública em base64 (formato do protocolo SSH)
	AddedAt     int64  `json:"addedAt"`
}

type File struct {
	Version int             `json:"version"`
	Hosts   map[string]Host `json:"hosts"`
}

// Store lê e grava o arquivo. Use uma única instância por arquivo (o mutex protege as gravações).
type Store struct {
	File string // vazio = só em memória (testes)
	mu   sync.Mutex
}

func New(file string) *Store { return &Store{File: file} }

// Addr monta "host:porta" do jeito que fica gravado.
func Addr(host string, port int) string {
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
}

func (s *Store) Load() (File, error) {
	k := File{Version: fileVersion, Hosts: map[string]Host{}}
	if s == nil || s.File == "" {
		return k, nil
	}
	_, err := store.ReadJSON(s.File, &k)
	if k.Hosts == nil {
		k.Hosts = map[string]Host{}
	}
	return k, err
}

// Callback confere a chave do servidor. trust é a impressão digital que o usuário confirmou
// (vazio = só aceita servidor já conhecido). Uma chave nova confirmada é gravada.
func (s *Store) Callback(addr, trust string, known File) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		fp := ssh.FingerprintSHA256(key)
		raw := base64.StdEncoding.EncodeToString(key.Marshal())
		if k, ok := known.Hosts[addr]; ok {
			if k.Key == raw {
				return nil
			}
			return &HostKeyMismatchError{Addr: addr, KeyType: key.Type(), Fingerprint: fp, Known: k.Fingerprint}
		}
		if trust == "" || trust != fp {
			return &UnknownHostError{Addr: addr, KeyType: key.Type(), Fingerprint: fp}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		cur, _ := s.Load()
		cur.Hosts[addr] = Host{Type: key.Type(), Fingerprint: fp, Key: raw, AddedAt: time.Now().Unix()}
		if s.File != "" {
			return store.WriteJSON(s.File, cur, 0o600)
		}
		return nil
	}
}

// Forget apaga a chave guardada de um servidor (quando você reinstalou o sistema, por exemplo).
func (s *Store) Forget(addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, _ := s.Load()
	delete(cur.Hosts, addr)
	if s.File == "" {
		return nil
	}
	return store.WriteJSON(s.File, cur, 0o600)
}

// Algorithms devolve os algoritmos de chave a pedir ao servidor: o tipo já conhecido ou,
// na primeira vez, a mesma preferência do OpenSSH (assim a impressão digital é a que você já conhece).
func Algorithms(known File, addr string) []string {
	if k, ok := known.Hosts[addr]; ok {
		switch k.Type {
		case ssh.KeyAlgoRSA:
			return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
		case "":
		default:
			return []string{k.Type}
		}
	}
	return []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
}

// FriendlyDialError traduz erros de conexão para mensagens em português.
func FriendlyDialError(err error, addr string) error {
	var unk *UnknownHostError
	var mis *HostKeyMismatchError
	if errors.As(err, &unk) {
		return unk
	}
	if errors.As(err, &mis) {
		return mis
	}
	msg := err.Error()
	var ne net.Error
	switch {
	case strings.Contains(msg, "unable to authenticate"), strings.Contains(msg, "no supported methods remain"):
		return ErrAuth
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("%s recusou a conexão: o SSH está ligado nessa porta?", addr)
	case errors.As(err, &ne) && ne.Timeout(), strings.Contains(msg, "i/o timeout"):
		return fmt.Errorf("%s não respondeu a tempo", addr)
	case strings.Contains(msg, "no such host"):
		return fmt.Errorf("não encontrei o endereço %s", addr)
	case strings.Contains(msg, "no route to host"), strings.Contains(msg, "network is unreachable"):
		return fmt.Errorf("sem rota até %s", addr)
	}
	return fmt.Errorf("não deu para conectar em %s", addr)
}

// ErrAuth: o servidor recusou o usuário, a senha ou a chave.
var ErrAuth = errors.New("usuário, senha ou chave do SSH recusados pelo servidor")

// IsNetErr separa falhas de rede (servidor fora, conexão caiu) das recusas de login.
func IsNetErr(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := err.Error()
	for _, s := range []string{"connection refused", "no route to host", "network is unreachable", "no such host", "i/o timeout"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
