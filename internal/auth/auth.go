// Package auth cuida do usuário do BrunOS: cadastro na primeira execução,
// login com limite de tentativas e sessões por cookie.
//
// Segurança:
//   - Senhas viram PBKDF2-HMAC-SHA256 (600 000 iterações, sal aleatório de 16 bytes,
//     recomendação OWASP) — a senha em si nunca é gravada.
//   - auth.json e sessions.json ficam com permissão 0600 dentro de /data.
//   - O token de sessão (32 bytes aleatórios) só existe no cookie HttpOnly do navegador;
//     no disco fica apenas o SHA-256 dele, então ler sessions.json não permite entrar.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"brunos/internal/store"
)

const (
	pbkdf2Iterations = 600_000
	keyLen           = 32
	sessionTTL       = 30 * 24 * time.Hour
	minPasswordLen   = 8
)

var (
	ErrInvalidCredentials = errors.New("usuário ou senha incorretos")
	ErrSetupDone          = errors.New("o BrunOS já foi configurado")
	ErrWeakPassword       = errors.New("a senha precisa ter pelo menos 8 caracteres")
	ErrBadUsername        = errors.New("use de 3 a 32 letras, números, ponto, hífen ou sublinhado")
	ErrRateLimited        = errors.New("muitas tentativas; aguarde um pouco")
	usernameRe            = regexp.MustCompile(`^[a-zA-Z0-9._-]{3,32}$`)
)

type User struct {
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Salt        string    `json:"salt"` // base64
	Hash        string    `json:"hash"` // base64
	Iterations  int       `json:"iterations"`
	CreatedAt   time.Time `json:"createdAt"`
	PasswordAt  time.Time `json:"passwordChangedAt"`
}

type Session struct {
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"userAgent"`
	// Locked: Bloqueio com senha (CONTEXT.md). Gravado em disco: recarregar a página ou reiniciar o
	// BrunOS não desbloqueia.
	Locked bool `json:"locked,omitempty"`
	// ID identifica a Sessão sem revelar o token (é o SHA-256 dele, a mesma chave do sessions.json).
	ID string `json:"-"`
}

type authFile struct {
	Version int    `json:"version"`
	Users   []User `json:"users"`
}

type Manager struct {
	mu           sync.Mutex
	authPath     string
	sessionsPath string
	users        []User
	sessions     map[string]*Session // chave: sha256(token) em hex
	limiter      *limiter
	dummy        User // gasta o mesmo tempo quando o usuário não existe

	// OnBlock é chamado quando um IP erra a senha tantas vezes que fica bloqueado.
	OnBlock func(ip string, lock time.Duration)
}

func New(dataDir string) (*Manager, error) {
	m := &Manager{
		authPath:     filepath.Join(dataDir, "auth.json"),
		sessionsPath: filepath.Join(dataDir, "sessions.json"),
		sessions:     map[string]*Session{},
		limiter:      newLimiter(),
	}
	var af authFile
	if _, err := store.ReadJSON(m.authPath, &af); err != nil {
		return nil, err
	}
	m.users = af.Users
	if _, err := store.ReadJSON(m.sessionsPath, &m.sessions); err != nil || m.sessions == nil {
		m.sessions = map[string]*Session{} // arquivo corrompido: todos entram de novo
	}
	var err error
	if m.dummy, err = newUser("dummy", "dummy", "dummy-password-for-timing"); err != nil {
		return nil, err
	}
	m.pruneLocked()
	return m, nil
}

func newUser(username, display, password string) (User, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return User{}, err
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, keyLen)
	if err != nil {
		return User{}, err
	}
	now := time.Now().UTC()
	return User{
		Username: username, DisplayName: display,
		Salt: base64.StdEncoding.EncodeToString(salt), Hash: base64.StdEncoding.EncodeToString(hash),
		Iterations: pbkdf2Iterations, CreatedAt: now, PasswordAt: now,
	}, nil
}

func (u User) check(password string) bool {
	salt, err1 := base64.StdEncoding.DecodeString(u.Salt)
	want, err2 := base64.StdEncoding.DecodeString(u.Hash)
	if err1 != nil || err2 != nil || u.Iterations <= 0 || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, u.Iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func validate(username, password string) error {
	if !usernameRe.MatchString(username) {
		return ErrBadUsername
	}
	if utf8.RuneCountInString(password) < minPasswordLen || len(password) > 1024 {
		return ErrWeakPassword
	}
	return nil
}

func (m *Manager) SetupRequired() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users) == 0
}

// DisplayName devolve o nome de exibição do primeiro usuário (para a tela de login).
func (m *Manager) DisplayName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) == 0 {
		return ""
	}
	return m.users[0].DisplayName
}

func (m *Manager) saveUsersLocked() error {
	return store.WriteJSON(m.authPath, authFile{Version: 1, Users: m.users}, 0o600)
}

func (m *Manager) saveSessionsLocked() error {
	return store.WriteJSON(m.sessionsPath, m.sessions, 0o600)
}

// Setup cria o primeiro usuário. Só funciona enquanto não houver nenhum.
func (m *Manager) Setup(username, display, password, ip, ua string) (string, error) {
	username = strings.TrimSpace(username)
	display = strings.TrimSpace(display)
	if display == "" {
		display = username
	}
	if utf8.RuneCountInString(display) > 40 {
		display = string([]rune(display)[:40])
	}
	if err := validate(username, password); err != nil {
		return "", err
	}
	u, err := newUser(username, display, password)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) > 0 {
		return "", ErrSetupDone
	}
	m.users = []User{u}
	if err := m.saveUsersLocked(); err != nil {
		m.users = nil
		return "", err
	}
	return m.newSessionLocked(username, ip, ua)
}

// Login confere a senha (com limite de tentativas por IP) e abre uma sessão.
func (m *Manager) Login(username, password, ip, ua string) (string, error) {
	if m.limiter.blocked(ip) > 0 {
		return "", ErrRateLimited
	}
	m.mu.Lock()
	var user *User
	for i := range m.users {
		if strings.EqualFold(m.users[i].Username, strings.TrimSpace(username)) {
			u := m.users[i]
			user = &u
			break
		}
	}
	m.mu.Unlock()

	ok := false
	if user != nil {
		ok = user.check(password)
	} else {
		m.dummy.check(password) // mesmo custo de tempo: não revela se o usuário existe
	}
	if !ok {
		if lock := m.limiter.fail(ip); lock > 0 && m.OnBlock != nil {
			m.OnBlock(ip, lock)
		}
		return "", ErrInvalidCredentials
	}
	m.limiter.success(ip)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.newSessionLocked(user.Username, ip, ua)
}

func (m *Manager) newSessionLocked(username, ip, ua string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if len(ua) > 200 {
		ua = ua[:200]
	}
	now := time.Now().UTC()
	m.sessions[hashToken(token)] = &Session{Username: username, CreatedAt: now, ExpiresAt: now.Add(sessionTTL), IP: ip, UserAgent: ua}
	m.pruneLocked()
	return token, m.saveSessionsLocked()
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Validate devolve a sessão do token, renovando a validade de forma deslizante.
func (m *Manager) Validate(token string) (*Session, bool) {
	if token == "" || len(token) > 100 {
		return nil, false
	}
	key := hashToken(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	if !ok {
		return nil, false
	}
	now := time.Now().UTC()
	if now.After(s.ExpiresAt) {
		delete(m.sessions, key)
		_ = m.saveSessionsLocked()
		return nil, false
	}
	// renova no máximo uma vez por dia para não gravar em disco a cada requisição
	if s.ExpiresAt.Sub(now) < sessionTTL-24*time.Hour {
		s.ExpiresAt = now.Add(sessionTTL)
		_ = m.saveSessionsLocked()
	}
	cp := *s
	cp.ID = key
	return &cp, true
}

// SessionID devolve o ID da Sessão de um token (sem conferir validade).
func SessionID(token string) string { return hashToken(token) }

// Lock bloqueia a Sessão (modo com senha): até Unlock, o servidor só aceita desbloquear ou sair.
func (m *Manager) Lock(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrInvalidCredentials
	}
	if s.Locked {
		return nil
	}
	s.Locked = true
	return m.saveSessionsLocked()
}

// IsLocked diz se a Sessão está bloqueada (sessão que não existe conta como bloqueada).
func (m *Manager) IsLocked(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return !ok || s.Locked
}

// SessionLocked diz se a Sessão existe E está bloqueada (para não encerrar as Abas dela).
func (m *Manager) SessionLocked(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return ok && s.Locked
}

// ErrSessionEnded: errou a senha do desbloqueio vezes demais; a Sessão foi encerrada.
var ErrSessionEnded = errors.New("muitas tentativas erradas: entre de novo com usuário e senha")

// Verify confere a senha do usuário com o mesmo limite de tentativas do login.
func (m *Manager) Verify(username, password, ip string) error {
	if m.limiter.blocked(ip) > 0 {
		return ErrRateLimited
	}
	u, ok := m.User(username)
	if !ok || !u.check(password) {
		if lock := m.limiter.fail(ip); lock > 0 {
			if m.OnBlock != nil {
				m.OnBlock(ip, lock)
			}
			return ErrRateLimited
		}
		return ErrInvalidCredentials
	}
	m.limiter.success(ip)
	return nil
}

// Unlock confere a senha e desbloqueia a Sessão. Se o IP ficar bloqueado por errar demais, a
// Sessão é encerrada (quem estiver tentando adivinhar perde o acesso de vez).
func (m *Manager) Unlock(id, password, ip string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	var user string
	if ok {
		user = s.Username
	}
	m.mu.Unlock()
	if !ok {
		return ErrInvalidCredentials
	}
	if err := m.Verify(user, password, ip); err != nil {
		if errors.Is(err, ErrRateLimited) {
			m.mu.Lock()
			delete(m.sessions, id)
			_ = m.saveSessionsLocked()
			m.mu.Unlock()
			return ErrSessionEnded
		}
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok && s.Locked {
		s.Locked = false
		return m.saveSessionsLocked()
	}
	return nil
}

func (m *Manager) Logout(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, hashToken(token))
	_ = m.saveSessionsLocked()
}

// ChangePassword troca a senha e encerra todas as outras sessões.
func (m *Manager) ChangePassword(username, current, next, keepToken string) error {
	if err := validate(username, next); err != nil {
		return err
	}
	u, ok := m.User(username)
	if !ok || !u.check(current) {
		return ErrInvalidCredentials
	}
	nu, err := newUser(u.Username, u.DisplayName, next)
	if err != nil {
		return err
	}
	nu.CreatedAt = u.CreatedAt
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.users {
		if m.users[i].Username == username {
			m.users[i] = nu
		}
	}
	if err := m.saveUsersLocked(); err != nil {
		return err
	}
	keep := hashToken(keepToken)
	for k, s := range m.sessions {
		if s.Username == username && k != keep {
			delete(m.sessions, k)
		}
	}
	return m.saveSessionsLocked()
}

func (m *Manager) SetDisplayName(username, display string) error {
	display = strings.TrimSpace(display)
	if display == "" || utf8.RuneCountInString(display) > 40 {
		return errors.New("nome inválido")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.users {
		if m.users[i].Username == username {
			m.users[i].DisplayName = display
			return m.saveUsersLocked()
		}
	}
	return ErrInvalidCredentials
}

func (m *Manager) User(username string) (User, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Username == username {
			return u, true
		}
	}
	return User{}, false
}

// Sessions devolve uma cópia das sessões abertas (sem os tokens).
func (m *Manager) Sessions() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, *s)
	}
	return out
}

func (m *Manager) pruneLocked() {
	now := time.Now().UTC()
	for k, s := range m.sessions {
		if now.After(s.ExpiresAt) {
			delete(m.sessions, k)
		}
	}
}
