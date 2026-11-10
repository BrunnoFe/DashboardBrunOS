// Package terminal abre um terminal no servidor por SSH, para a janela Terminal do BrunOS.
//
// Segurança:
//   - O container do BrunOS continua sem privilégios: o terminal é uma conexão SSH comum
//     (com o seu usuário e senha ou chave), igual a abrir um terminal no seu computador.
//   - Senhas e chaves são usadas só para abrir a conexão e nunca são gravadas.
//   - A chave do servidor é conferida (TOFU): na primeira conexão você confirma a impressão
//     digital; ela fica em /data/known_hosts.json e, se mudar, a conexão é recusada.
//   - Só quem está logado no BrunOS usa as rotas; sessões sem ninguém olhando fecham sozinhas
//     depois do tempo escolhido nas Configurações, a menos que algo ainda esteja rodando nelas
//     (conferido por um canal SSH extra, decisão 41) ou que a Sessão do BrunOS esteja bloqueada.
//   - A Chave do Terminal (internal/termkey) chega aqui já aberta, só para quem desbloqueou.
//
// Cada sessão SSH é uma Aba da janela Terminal (CONTEXT.md).
package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"brunos/internal/sshknown"
)

const (
	maxSessions = 4
	maxBuffered = 256 << 10 // saída ainda não entregue ao leitor
	// maxHistory: o fim da tela (~2.000 linhas) para mostrar de novo ao reabrir uma Aba minimizada
	maxHistory   = 256 << 10
	idleDefault  = 15 * time.Minute
	dialTimeout  = 10 * time.Second
	openPerMin   = 12
	checkEvery   = time.Minute      // Aba minimizada com algo rodando: confere a cada minuto
	detachGrace  = 10 * time.Second // recarregar a página não conta como minimizar
	busyDeadline = 8 * time.Second
)

// HostRe valida um nome de host ou IP (também usado pela configuração de energia).
var HostRe = regexp.MustCompile(`^[a-zA-Z0-9._\-]{1,253}$|^\[?[0-9a-fA-F:.]{2,45}\]?$`)

// ── Erros que a interface trata ──────────────────────────────────

// UnknownHostError: primeira conexão a este servidor; peça para o usuário confirmar a impressão digital.
type UnknownHostError = sshknown.UnknownHostError

// HostKeyMismatchError: a chave do servidor mudou desde a última vez.
type HostKeyMismatchError = sshknown.HostKeyMismatchError

var (
	ErrBadRequest  = errors.New("pedido inválido")
	ErrTooMany     = errors.New("muitas sessões abertas; feche alguma antes")
	ErrRateLimited = errors.New("muitas tentativas; espere um minuto")
	ErrNoSession   = errors.New("sessão de terminal não encontrada")
	ErrAuth        = sshknown.ErrAuth
	ErrBadKey      = errors.New("não consegui ler a chave privada (confira o texto e a senha da chave)")
	// ErrKeyLocked: a Chave do Terminal existe, mas está trancada para esta Sessão (pede a senha).
	ErrKeyLocked = errors.New("digite a senha do BrunOS para usar a Chave do Terminal")
	ErrNoKey     = errors.New("a Chave do Terminal ainda não foi criada")
)

// ── Gerenciador e sessões ────────────────────────────────────────

type Manager struct {
	KnownFile string
	Known     *sshknown.Store

	// Ganchos do main (todos opcionais).
	OwnerOf     func(r *http.Request) string                     // Sessão do BrunOS que abriu a Aba
	KeyFor      func(r *http.Request) (ssh.Signer, error)        // Chave do Terminal aberta
	Paused      func(owner string) bool                          // Sessão bloqueada: não encerra as Abas
	IdleTimeout func() time.Duration                             // 0 = nunca
	OnDone      func(addr, user string, tab int, names []string) // comando terminou numa Aba minimizada
	Locked      func(r *http.Request) bool                       // Sessão bloqueada: derruba o stream

	mu       sync.Mutex
	sessions map[string]*Session
	opens    []time.Time
	stop     chan struct{}
	// Busy é trocável nos testes (o padrão roda o ps por um canal SSH extra).
	busyFn func(s *Session) Busy
}

type Session struct {
	ID      string
	Addr    string
	User    string
	Owner   string
	Created time.Time

	client *ssh.Client
	sess   *ssh.Session
	stdin  io.WriteCloser

	mu       sync.Mutex
	buf      []byte
	hist     []byte
	notify   chan struct{}
	closed   bool
	exitMsg  string
	attachN  int // geração do leitor atual; um novo leitor substitui o anterior
	readers  int
	lastSeen time.Time

	// conferência "ainda está rodando?" (só sem ninguém olhando)
	checking  bool
	checked   bool // já conferiu desde que ficou sem leitor
	lastCheck time.Time
	running   []string // o que estava rodando na última conferência
}

func NewManager(knownFile string) *Manager { return NewManagerWith(sshknown.New(knownFile)) }

// NewManagerWith usa um arquivo de servidores conhecidos compartilhado com outras partes do BrunOS.
func NewManagerWith(known *sshknown.Store) *Manager {
	m := &Manager{KnownFile: known.File, Known: known, sessions: map[string]*Session{}, stop: make(chan struct{})}
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
	Trust      string `json:"trust"`  // impressão digital confirmada pelo usuário
	UseKey     bool   `json:"useKey"` // entrar com a Chave do Terminal

	Signer ssh.Signer `json:"-"` // a Chave do Terminal aberta (preenchida pelo handler)
	Owner  string     `json:"-"`
}

func (m *Manager) Open(req OpenRequest) (*Session, error) {
	req.Host = strings.TrimSpace(req.Host)
	req.User = strings.TrimSpace(req.User)
	if req.Port == 0 {
		req.Port = 22
	}
	if !HostRe.MatchString(req.Host) || req.Port < 1 || req.Port > 65535 || req.User == "" || len(req.User) > 64 ||
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

	addr := sshknown.Addr(req.Host, req.Port)
	var auths []ssh.AuthMethod
	if req.Signer != nil {
		auths = append(auths, ssh.PublicKeys(req.Signer))
	}
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

	known, _ := m.Known.Load()
	keyOK := false // a chave do servidor foi aceita: um erro depois disso é da autenticação
	check := m.Known.Callback(addr, req.Trust, known)
	cfg := &ssh.ClientConfig{
		User:    req.User,
		Auth:    auths,
		Timeout: dialTimeout,
		HostKeyCallback: func(h string, a net.Addr, k ssh.PublicKey) error {
			err := check(h, a, k)
			keyOK = err == nil
			return err
		},
		HostKeyAlgorithms: sshknown.Algorithms(known, addr),
		ClientVersion:     "SSH-2.0-BrunOS",
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		slog.Info("terminal: detalhe do erro do ssh", "destino", addr, "erro", err)
		if keyOK && !sshknown.IsNetErr(err) {
			return nil, ErrAuth
		}
		return nil, sshknown.FriendlyDialError(err, addr)
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
	s := &Session{ID: newID(), Addr: addr, User: req.User, Owner: req.Owner, Created: now, client: client, sess: sess, stdin: stdin,
		notify: make(chan struct{}, 1), lastSeen: now, checked: true}
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

// CloseOwner encerra as Abas abertas por uma Sessão do BrunOS (ao sair).
func (m *Manager) CloseOwner(owner string) {
	m.mu.Lock()
	var drop []*Session
	for id, s := range m.sessions {
		if s.Owner == owner {
			delete(m.sessions, id)
			drop = append(drop, s)
		}
	}
	m.mu.Unlock()
	for _, s := range drop {
		s.kill("sessão encerrada")
	}
}

type SessionInfo struct {
	ID      string   `json:"id"`
	Addr    string   `json:"addr"`
	User    string   `json:"user"`
	Created int64    `json:"created"`
	Running []string `json:"running,omitempty"` // o que estava rodando na última conferência
}

// List devolve as Abas abertas, da mais antiga para a mais nova (a ordem das Abas na janela).
func (m *Manager) List() []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []SessionInfo{}
	for _, s := range m.sessions {
		s.mu.Lock()
		out = append(out, SessionInfo{ID: s.ID, Addr: s.Addr, User: s.User, Created: s.Created.UnixMilli(), Running: s.running})
		s.mu.Unlock()
	}
	sortInfos(out)
	return out
}

func sortInfos(out []SessionInfo) {
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Created < out[j-1].Created; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
}

func (m *Manager) idle() time.Duration {
	if m.IdleTimeout == nil {
		return idleDefault
	}
	return m.IdleTimeout()
}

// tabNumber é a posição (1, 2...) da Aba entre as abertas, para os avisos ("aba 2").
func (m *Manager) tabNumber(id string) int {
	for i, x := range m.List() {
		if x.ID == id {
			return i + 1
		}
	}
	return 0
}

// reaper fecha Abas terminadas e, sem ninguém olhando, confere se ainda há algo rodando: encerra
// quando o tempo sem uso passou e nada roda; avisa quando um comando de uma Aba minimizada termina.
func (m *Manager) reaper() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
		}
		m.sweep(time.Now())
	}
}

func (m *Manager) sweep(now time.Time) {
	timeout := m.idle()
	var drop, check []*Session
	m.mu.Lock()
	for id, s := range m.sessions {
		paused := m.Paused != nil && m.Paused(s.Owner)
		s.mu.Lock()
		done := s.closed && s.readers == 0 && now.Sub(s.lastSeen) > 30*time.Second
		unwatched := s.readers == 0 && !s.closed && now.Sub(s.lastSeen) > detachGrace
		expired := unwatched && timeout > 0 && now.Sub(s.lastSeen) > timeout && !paused
		needCheck := unwatched && !s.checking &&
			(!s.checked || // acabou de ficar sem ninguém: descobre se tem algo rodando
				(len(s.running) > 0 && now.Sub(s.lastCheck) >= checkEvery) || // rodando: até terminar
				(expired && now.Sub(s.lastCheck) >= checkEvery)) // tempo esgotado: confere antes de fechar
		if needCheck {
			s.checking = true
		}
		s.mu.Unlock()
		if done {
			delete(m.sessions, id)
			drop = append(drop, s)
		} else if needCheck {
			check = append(check, s)
		}
	}
	m.mu.Unlock()
	for _, s := range drop {
		s.kill("sessão encerrada")
	}
	for _, s := range check {
		go m.check(s, timeout)
	}
}

// check roda a conferência de uma Aba sem ninguém olhando e decide o que fazer.
func (m *Manager) check(s *Session, timeout time.Duration) {
	fn := m.busyFn
	if fn == nil {
		fn = (*Session).Busy
	}
	b := fn(s)
	now := time.Now()
	paused := m.Paused != nil && m.Paused(s.Owner)
	s.mu.Lock()
	s.checking, s.checked, s.lastCheck = false, true, now
	prev := s.running
	if b.Known {
		s.running = b.Names
	}
	watching := s.readers > 0
	expired := !watching && timeout > 0 && now.Sub(s.lastSeen) > timeout && !paused
	s.mu.Unlock()
	if b.Known && len(prev) > 0 && len(b.Names) == 0 && !watching && m.OnDone != nil {
		m.OnDone(s.Addr, s.User, m.tabNumber(s.ID), prev)
	}
	// tempo esgotado: fecha se nada roda (ou se não deu para saber, como antes)
	if expired && (!b.Known || len(b.Names) == 0) {
		m.mu.Lock()
		_, ok := m.sessions[s.ID]
		delete(m.sessions, s.ID)
		m.mu.Unlock()
		if ok {
			slog.Info("terminal: aba encerrada por inatividade", "destino", s.Addr)
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
			s.buf = capTail(append(s.buf, b[:n]...), maxBuffered)
			s.hist = capTail(append(s.hist, b[:n]...), maxHistory)
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

// Resize muda o tamanho do terminal. Também vale como sinal de vida: com a janela Terminal aberta,
// as Abas que não estão à frente mandam um a cada minuto e não contam como "sem ninguém olhando".
func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	s.lastSeen = time.Now()
	s.mu.Unlock()
	cols, rows = clampSize(cols, rows)
	return s.sess.WindowChange(rows, cols)
}

// capTail guarda só o fim de b (até max bytes), começando numa linha nova quando dá.
func capTail(b []byte, max int) []byte {
	if len(b) <= max {
		return b
	}
	cut := b[len(b)-max:]
	if i := strings.IndexByte(string(cut[:min(len(cut), 4096)]), '\n'); i >= 0 {
		cut = cut[i+1:]
	}
	return append([]byte(nil), cut...)
}

// Attach registra um leitor. O anterior (outra janela, conexão antiga) é dispensado. Com replay,
// devolve o fim da tela (Aba reaberta depois de minimizar) e o que estava pendente já vai junto.
func (s *Session) Attach(replay bool) (gen int, hist []byte, detach func()) {
	s.mu.Lock()
	s.attachN++
	s.readers++
	gen = s.attachN
	if replay {
		hist = append([]byte(nil), s.hist...)
		s.buf = nil
	}
	s.mu.Unlock()
	s.signal()
	return gen, hist, func() {
		s.mu.Lock()
		s.readers--
		s.lastSeen = time.Now()
		if s.readers == 0 {
			s.checked = false // ficou sem ninguém: a próxima varredura confere se algo roda
		}
		s.mu.Unlock()
	}
}

// ── "Ainda está rodando?" (decisão 41) ───────────────────────────

// Busy é o resultado da conferência. Known = false quando não deu para saber (não é Linux, sem ps).
type Busy struct {
	Known bool     `json:"known"`
	Names []string `json:"names"`
}

// busyScript roda num canal SSH extra da mesma conexão. O pai deste comando é o processo do sshd
// da Aba; o shell interativo é o irmão que tem terminal (tty). Se o grupo em primeiro plano desse
// terminal (tpgid) não for o próprio shell, há um comando rodando (F); filhos do shell fora desse
// grupo são programas em segundo plano (B).
const busyScript = `p=$(ps -o ppid= -p $$ | tr -d ' '); [ -n "$p" ] || exit 3; echo OK; ` +
	`ps -o pid=,tpgid=,tty= --ppid "$p" | while read pid tp tty; do [ "$tty" = "?" ] && continue; echo "S $pid $tp"; ` +
	`ps -e -o pgid=,ppid=,pid=,comm= | awk -v g="$tp" -v s="$pid" '($1==g && $3!=s){print "F " $4} ($2==s && $1!=g){print "B " $4}'; done`

// shells não contam como "rodando" (ex.: um sudo -i parado no prompt).
var shells = map[string]bool{"bash": true, "sh": true, "zsh": true, "fish": true, "dash": true, "ash": true, "ksh": true,
	"sudo": true, "su": true, "login": true, "ps": true, "awk": true}

func parseBusy(out string) Busy {
	ok, sawShell := false, false
	seen := map[string]bool{}
	names := []string{}
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case ln == "OK":
			ok = true
		case strings.HasPrefix(ln, "S "):
			sawShell = true
		case strings.HasPrefix(ln, "F ") || strings.HasPrefix(ln, "B "):
			n := strings.TrimPrefix(strings.TrimSpace(ln[2:]), "-")
			if n == "" || shells[n] || seen[n] {
				continue
			}
			seen[n] = true
			names = append(names, n)
		}
	}
	if !ok || !sawShell {
		return Busy{}
	}
	return Busy{Known: true, Names: names}
}

// Busy confere, por um canal SSH extra, se há algo rodando no shell da Aba.
func (s *Session) Busy() Busy {
	ch, err := s.client.NewSession()
	if err != nil {
		return Busy{}
	}
	defer ch.Close()
	type res struct {
		out []byte
	}
	done := make(chan res, 1)
	go func() {
		out, _ := ch.Output(busyScript) // erro de saída ≠ 0 não importa: vale o que foi impresso
		done <- res{out}
	}()
	select {
	case r := <-done:
		return parseBusy(string(r.out))
	case <-time.After(busyDeadline):
		return Busy{}
	}
}

// CheckBusy é a conferência pedida pela janela (antes de fechar com o "×").
func (m *Manager) CheckBusy(id string) (Busy, error) {
	s, err := m.Get(id)
	if err != nil {
		return Busy{}, err
	}
	fn := m.busyFn
	if fn == nil {
		fn = (*Session).Busy
	}
	b := fn(s)
	if b.Known {
		s.mu.Lock()
		s.running = b.Names
		s.mu.Unlock()
	}
	return b, nil
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

func (m *Manager) loadKnown() (sshknown.File, error) { return m.Known.Load() }

// Forget apaga a chave guardada de um servidor (quando você reinstalou o sistema, por exemplo).
func (m *Manager) Forget(host string, port int) error {
	return m.Known.Forget(sshknown.Addr(host, port))
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
