// Package terminal abre um terminal no servidor por SSH, para a janela Terminal do BrunOS.
//
// Segurança:
//   - O container do BrunOS continua sem privilégios: o terminal é uma conexão SSH comum
//     (com o seu usuário e senha ou chave), igual a abrir um terminal no seu computador.
//   - Senhas e chaves são usadas só para abrir a conexão e nunca são gravadas.
//   - A chave do servidor é conferida (TOFU): na primeira conexão você confirma a impressão
//     digital; ela fica em /data/known_hosts.json e, se mudar, a conexão é recusada.
//   - Só quem está logado no BrunOS usa as rotas; sessões sem ninguém olhando fecham sozinhas.
package terminal

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"brunos/internal/store"
)

const (
	maxSessions  = 4
	maxBuffered  = 512 << 10 // saída guardada enquanto ninguém lê (ex.: recarregou a página)
	idleTimeout  = 3 * time.Minute
	dialTimeout  = 10 * time.Second
	openPerMin   = 12
	knownVersion = 1
)

var hostRe = regexp.MustCompile(`^[a-zA-Z0-9._\-]{1,253}$|^\[?[0-9a-fA-F:.]{2,45}\]?$`)

// ── Erros que a interface trata ──────────────────────────────────

// UnknownHostError: primeira conexão a este servidor; peça para o usuário confirmar a impressão digital.
type UnknownHostError struct{ Addr, KeyType, Fingerprint string }

func (e *UnknownHostError) Error() string { return "servidor ainda não confirmado" }

// HostKeyMismatchError: a chave do servidor mudou desde a última vez.
type HostKeyMismatchError struct{ Addr, KeyType, Fingerprint, Known string }

func (e *HostKeyMismatchError) Error() string {
	return "a chave deste servidor mudou desde a última conexão; a conexão foi recusada"
}

var (
	ErrBadRequest  = errors.New("pedido inválido")
	ErrTooMany     = errors.New("muitas sessões abertas; feche alguma antes")
	ErrRateLimited = errors.New("muitas tentativas; espere um minuto")
	ErrNoSession   = errors.New("sessão de terminal não encontrada")
	ErrAuth        = errors.New("usuário, senha ou chave do SSH recusados pelo servidor")
	ErrBadKey      = errors.New("não consegui ler a chave privada (confira o texto e a senha da chave)")
)

// ── Servidores conhecidos ────────────────────────────────────────

type knownHost struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Key         string `json:"key"` // chave pública em base64 (formato do protocolo SSH)
	AddedAt     int64  `json:"addedAt"`
}

type knownFile struct {
	Version int                  `json:"version"`
	Hosts   map[string]knownHost `json:"hosts"`
}

// ── Gerenciador e sessões ────────────────────────────────────────

type Manager struct {
	KnownFile string

	mu       sync.Mutex
	sessions map[string]*Session
	opens    []time.Time
	stop     chan struct{}
}

type Session struct {
	ID      string
	Addr    string
	User    string
	Created time.Time

	client *ssh.Client
	sess   *ssh.Session
	stdin  io.WriteCloser

	mu       sync.Mutex
	buf      []byte
	notify   chan struct{}
	closed   bool
	exitMsg  string
	attachN  int // geração do leitor atual; um novo leitor substitui o anterior
	readers  int
	lastSeen time.Time
}

func NewManager(knownFile string) *Manager {
	m := &Manager{KnownFile: knownFile, sessions: map[string]*Session{}, stop: make(chan struct{})}
	go m.reaper()
	return m
}

type OpenRequest struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password"`
	Key        string `json:"key"`
	Passphrase string `json:"passphrase"`
	Cols       int    `json:"cols"`
	Rows       int    `json:"rows"`
	Trust      string `json:"trust"` // impressão digital confirmada pelo usuário
}

func (m *Manager) Open(req OpenRequest) (*Session, error) {
	req.Host = strings.TrimSpace(req.Host)
	req.User = strings.TrimSpace(req.User)
	if req.Port == 0 {
		req.Port = 22
	}
	if !hostRe.MatchString(req.Host) || req.Port < 1 || req.Port > 65535 || req.User == "" || len(req.User) > 64 ||
		strings.ContainsAny(req.User, " \t\r\n:@/") || len(req.Password) > 1024 || len(req.Key) > 32<<10 {
		return nil, ErrBadRequest
	}
	req.Cols, req.Rows = clampSize(req.Cols, req.Rows)

	m.mu.Lock()
	now := time.Now()
	recent := m.opens[:0]
	for _, t := range m.opens {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	m.opens = recent
	if len(m.opens) >= openPerMin {
		m.mu.Unlock()
		return nil, ErrRateLimited
	}
	m.opens = append(m.opens, now)
	if len(m.sessions) >= maxSessions {
		m.mu.Unlock()
		return nil, ErrTooMany
	}
	m.mu.Unlock()

	addr := net.JoinHostPort(strings.Trim(req.Host, "[]"), strconv.Itoa(req.Port))
	var auths []ssh.AuthMethod
	if strings.TrimSpace(req.Key) != "" {
		var signer ssh.Signer
		var err error
		if req.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(req.Key), []byte(req.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(req.Key))
		}
		if err != nil {
			return nil, ErrBadKey
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}
	if req.Password != "" {
		pw := req.Password
		auths = append(auths, ssh.Password(pw), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			ans := make([]string, len(qs))
			for i := range ans {
				ans[i] = pw
			}
			return ans, nil
		}))
	}
	if len(auths) == 0 {
		return nil, ErrBadRequest
	}

	known, _ := m.loadKnown()
	keyOK := false // a chave do servidor foi aceita: um erro depois disso é da autenticação
	check := m.hostKeyCallback(addr, req.Trust, known)
	cfg := &ssh.ClientConfig{
		User:    req.User,
		Auth:    auths,
		Timeout: dialTimeout,
		HostKeyCallback: func(h string, a net.Addr, k ssh.PublicKey) error {
			err := check(h, a, k)
			keyOK = err == nil
			return err
		},
		ClientVersion: "SSH-2.0-BrunOS",
	}
	if k, ok := known.Hosts[addr]; ok {
		cfg.HostKeyAlgorithms = algorithmsFor(k.Type)
	} else {
		// mesma preferência do OpenSSH: assim a impressão digital é a que você já conhece (ED25519)
		cfg.HostKeyAlgorithms = []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		slog.Info("terminal: detalhe do erro do ssh", "destino", addr, "erro", err)
		if keyOK && !isNetErr(err) {
			return nil, ErrAuth
		}
		return nil, friendlyDialError(err, addr)
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("o servidor recusou abrir a sessão")
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	stderr, _ := sess.StderrPipe()
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200}
	if err := sess.RequestPty("xterm-256color", req.Rows, req.Cols, modes); err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("o servidor não abriu um terminal (pty)")
	}
	if err := sess.Shell(); err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("o servidor não abriu o shell")
	}
	s := &Session{ID: newID(), Addr: addr, User: req.User, Created: now, client: client, sess: sess, stdin: stdin,
		notify: make(chan struct{}, 1), lastSeen: now}
	go s.pump(stdout)
	go s.pump(stderr)
	go func() {
		err := sess.Wait()
		msg := "sessão encerrada"
		var exit *ssh.ExitError
		if errors.As(err, &exit) {
			msg = fmt.Sprintf("o shell saiu com código %d", exit.ExitStatus())
		} else if err != nil && !errors.Is(err, io.EOF) {
			msg = "conexão perdida"
		}
		s.finish(msg)
		client.Close()
	}()

	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) Get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNoSession
	}
	return s, nil
}

func (m *Manager) Close(id string) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if ok {
		s.kill("sessão encerrada")
	}
}

// CloseAll encerra todas as sessões (ao sair do BrunOS ou trocar a senha).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := m.sessions
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	for _, s := range all {
		s.kill("sessão encerrada")
	}
}

type SessionInfo struct {
	ID      string `json:"id"`
	Addr    string `json:"addr"`
	User    string `json:"user"`
	Created int64  `json:"created"`
}

func (m *Manager) List() []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []SessionInfo{}
	for _, s := range m.sessions {
		out = append(out, SessionInfo{ID: s.ID, Addr: s.Addr, User: s.User, Created: s.Created.Unix()})
	}
	return out
}

// reaper fecha sessões terminadas ou sem ninguém olhando há mais de idleTimeout.
func (m *Manager) reaper() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
		}
		var drop []*Session
		m.mu.Lock()
		for id, s := range m.sessions {
			s.mu.Lock()
			idle := s.readers == 0 && time.Since(s.lastSeen) > idleTimeout
			done := s.closed && s.readers == 0 && time.Since(s.lastSeen) > 30*time.Second
			s.mu.Unlock()
			if idle || done {
				delete(m.sessions, id)
				drop = append(drop, s)
			}
		}
		m.mu.Unlock()
		for _, s := range drop {
			s.kill("sessão encerrada por inatividade")
		}
	}
}

// ── Sessão ───────────────────────────────────────────────────────

func (s *Session) pump(r io.Reader) {
	if r == nil {
		return
	}
	b := make([]byte, 32<<10)
	for {
		n, err := r.Read(b)
		if n > 0 {
			s.mu.Lock()
			s.buf = append(s.buf, b[:n]...)
			if len(s.buf) > maxBuffered {
				s.buf = append([]byte(nil), s.buf[len(s.buf)-maxBuffered:]...)
			}
			s.mu.Unlock()
			s.signal()
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) signal() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *Session) finish(msg string) {
	s.mu.Lock()
	if !s.closed {
		s.closed, s.exitMsg = true, msg
	}
	s.mu.Unlock()
	s.signal()
}

func (s *Session) kill(msg string) {
	s.finish(msg)
	s.sess.Close()
	s.client.Close()
}

func (s *Session) Write(data string) error {
	s.mu.Lock()
	closed := s.closed
	s.lastSeen = time.Now()
	s.mu.Unlock()
	if closed {
		return ErrNoSession
	}
	_, err := io.WriteString(s.stdin, data)
	return err
}

func (s *Session) Resize(cols, rows int) error {
	cols, rows = clampSize(cols, rows)
	return s.sess.WindowChange(rows, cols)
}

// Attach registra um leitor. O anterior (outra aba, conexão antiga) é dispensado.
func (s *Session) Attach() (gen int, detach func()) {
	s.mu.Lock()
	s.attachN++
	s.readers++
	gen = s.attachN
	s.mu.Unlock()
	s.signal()
	return gen, func() {
		s.mu.Lock()
		s.readers--
		s.lastSeen = time.Now()
		s.mu.Unlock()
	}
}

// Next devolve a saída pendente. stale = outro leitor assumiu; done = o shell terminou.
func (s *Session) Next(gen int) (out []byte, done bool, msg string, stale bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.attachN {
		return nil, false, "", true
	}
	out, s.buf = s.buf, nil
	s.lastSeen = time.Now()
	return out, s.closed && len(out) == 0, s.exitMsg, false
}

func (s *Session) Notify() <-chan struct{} { return s.notify }

// ── Chave do servidor (TOFU) ─────────────────────────────────────

func (m *Manager) loadKnown() (knownFile, error) {
	k := knownFile{Version: knownVersion, Hosts: map[string]knownHost{}}
	if m.KnownFile == "" {
		return k, nil
	}
	_, err := store.ReadJSON(m.KnownFile, &k)
	if k.Hosts == nil {
		k.Hosts = map[string]knownHost{}
	}
	return k, err
}

func (m *Manager) hostKeyCallback(addr, trust string, known knownFile) ssh.HostKeyCallback {
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
		m.mu.Lock()
		defer m.mu.Unlock()
		cur, _ := m.loadKnown()
		cur.Hosts[addr] = knownHost{Type: key.Type(), Fingerprint: fp, Key: raw, AddedAt: time.Now().Unix()}
		if m.KnownFile != "" {
			return store.WriteJSON(m.KnownFile, cur, 0o600)
		}
		return nil
	}
}

// Forget apaga a chave guardada de um servidor (quando você reinstalou o sistema, por exemplo).
func (m *Manager) Forget(host string, port int) error {
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, _ := m.loadKnown()
	delete(cur.Hosts, addr)
	if m.KnownFile == "" {
		return nil
	}
	return store.WriteJSON(m.KnownFile, cur, 0o600)
}

func algorithmsFor(keyType string) []string {
	switch keyType {
	case ssh.KeyAlgoRSA:
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	case "":
		return nil
	default:
		return []string{keyType}
	}
}

func friendlyDialError(err error, addr string) error {
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

// isNetErr separa falhas de rede (servidor fora, conexão caiu) das recusas de login.
func isNetErr(err error) bool {
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

func clampSize(cols, rows int) (int, int) {
	if cols < 20 || cols > 500 {
		cols = 100
	}
	if rows < 5 || rows > 300 {
		rows = 30
	}
	return cols, rows
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
