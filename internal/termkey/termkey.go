// Package termkey guarda a Chave do Terminal (CONTEXT.md): uma chave SSH ED25519 criada pelo
// próprio BrunOS só para o Terminal, trancada pela senha do BrunOS (decisão 40).
//
// Segurança:
//   - No disco (/data/terminal_key.json, 0600) fica só a chave privada cifrada com AES-256-GCM. A
//     chave do AES vem da senha do BrunOS por PBKDF2-HMAC-SHA256 (600 000 iterações, sal próprio,
//     diferente do sal do login). Quem copiar o /data sem saber a senha não consegue usar a chave.
//   - A chave aberta (o "Signer") só existe na memória, ligada a uma Sessão desbloqueada. Ela some
//     no Bloqueio com senha, ao sair e quando o BrunOS reinicia; aí o Terminal pede a senha de novo.
//   - Esquecer a senha do BrunOS perde a chave: basta criar outra e autorizar de novo.
package termkey

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"brunos/internal/store"
)

const (
	iterations = 600_000
	Comment    = "brunos-terminal"
)

var (
	ErrNoKey         = errors.New("a Chave do Terminal ainda não foi criada")
	ErrWrongPassword = errors.New("senha do BrunOS incorreta")
	ErrExists        = errors.New("a Chave do Terminal já existe (apague antes de criar outra)")
)

type file struct {
	Version   int       `json:"v"`
	Salt      string    `json:"salt"`
	Iter      int       `json:"iter"`
	Nonce     string    `json:"nonce"`
	Box       string    `json:"box"` // chave privada (PEM OpenSSH) cifrada
	Public    string    `json:"pub"` // linha do authorized_keys
	CreatedAt time.Time `json:"createdAt"`
}

// Vault é o cofre da Chave do Terminal.
type Vault struct {
	path string

	mu    sync.Mutex
	open  map[string]ssh.Signer // por ID de Sessão
	cache *file
}

func New(path string) *Vault { return &Vault{path: path, open: map[string]ssh.Signer{}} }

func (v *Vault) load() (*file, error) {
	if v.cache != nil {
		return v.cache, nil
	}
	var f file
	ok, err := store.ReadJSON(v.path, &f)
	if err != nil {
		return nil, err
	}
	if !ok || f.Box == "" {
		return nil, ErrNoKey
	}
	v.cache = &f
	return &f, nil
}

// Info é o que a interface mostra (nada secreto).
type Info struct {
	Exists      bool      `json:"exists"`
	Public      string    `json:"publicKey,omitempty"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	CreatedAt   time.Time `json:"createdAt,omitzero"`
	Unlocked    bool      `json:"unlocked"` // aberta para esta Sessão
}

func (v *Vault) Info(sessionID string) Info {
	v.mu.Lock()
	defer v.mu.Unlock()
	f, err := v.load()
	if err != nil {
		return Info{}
	}
	in := Info{Exists: true, Public: f.Public, CreatedAt: f.CreatedAt, Unlocked: v.open[sessionID] != nil}
	if pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(f.Public)); err == nil {
		in.Fingerprint = ssh.FingerprintSHA256(pk)
	}
	return in
}

// Exists diz se há uma chave guardada.
func (v *Vault) Exists() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	_, err := v.load()
	return err == nil
}

func derive(password string, salt []byte, iter int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, iter, 32)
}

func seal(password string, pemBytes []byte) (salt, nonce, box []byte, err error) {
	salt = make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return
	}
	k, err := derive(password, salt, iterations)
	if err != nil {
		return
	}
	gcm, err := newGCM(k)
	if err != nil {
		return
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return
	}
	box = gcm.Seal(nil, nonce, pemBytes, []byte(Comment))
	return
}

func newGCM(k []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func (f *file) openPEM(password string) ([]byte, error) {
	salt, e1 := base64.StdEncoding.DecodeString(f.Salt)
	nonce, e2 := base64.StdEncoding.DecodeString(f.Nonce)
	box, e3 := base64.StdEncoding.DecodeString(f.Box)
	if e1 != nil || e2 != nil || e3 != nil || f.Iter <= 0 {
		return nil, errors.New("arquivo da Chave do Terminal corrompido")
	}
	k, err := derive(password, salt, f.Iter)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(k)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("arquivo da Chave do Terminal corrompido")
	}
	plain, err := gcm.Open(nil, nonce, box, []byte(Comment))
	if err != nil {
		return nil, ErrWrongPassword
	}
	return plain, nil
}

func (v *Vault) write(f *file) error {
	if err := store.WriteJSON(v.path, f, 0o600); err != nil {
		return err
	}
	v.cache = f
	return nil
}

// Create gera a chave, guarda cifrada com a senha e já a deixa aberta para a Sessão.
func (v *Vault) Create(sessionID, password string) (Info, error) {
	v.mu.Lock()
	if _, err := v.load(); err == nil {
		v.mu.Unlock()
		return Info{}, ErrExists
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		v.mu.Unlock()
		return Info{}, err
	}
	blk, err := ssh.MarshalPrivateKey(priv, Comment)
	if err != nil {
		v.mu.Unlock()
		return Info{}, err
	}
	pemBytes := pem.EncodeToMemory(blk)
	salt, nonce, box, err := seal(password, pemBytes)
	if err != nil {
		v.mu.Unlock()
		return Info{}, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		v.mu.Unlock()
		return Info{}, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		v.mu.Unlock()
		return Info{}, err
	}
	f := &file{Version: 1, Salt: b64(salt), Iter: iterations, Nonce: b64(nonce), Box: b64(box),
		Public: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " " + Comment, CreatedAt: time.Now().UTC()}
	if err := v.write(f); err != nil {
		v.mu.Unlock()
		return Info{}, err
	}
	v.open = map[string]ssh.Signer{sessionID: signer}
	v.mu.Unlock()
	return v.Info(sessionID), nil
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// Unlock abre a chave para a Sessão com a senha do BrunOS. Sem chave guardada, não faz nada.
func (v *Vault) Unlock(sessionID, password string) error {
	v.mu.Lock()
	f, err := v.load()
	v.mu.Unlock()
	if err != nil {
		return err
	}
	plain, err := f.openPEM(password) // lento (PBKDF2): fora do mutex
	if err != nil {
		return err
	}
	signer, err := ssh.ParsePrivateKey(plain)
	if err != nil {
		return errors.New("arquivo da Chave do Terminal corrompido")
	}
	v.mu.Lock()
	v.open[sessionID] = signer
	v.mu.Unlock()
	return nil
}

// Signer devolve a chave aberta para a Sessão (nil se trancada ou inexistente).
func (v *Vault) Signer(sessionID string) ssh.Signer {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.open[sessionID]
}

// Forget tranca a chave para uma Sessão (Bloqueio com senha, sair).
func (v *Vault) Forget(sessionID string) {
	v.mu.Lock()
	delete(v.open, sessionID)
	v.mu.Unlock()
}

// Keep mantém aberta só para as Sessões dadas (ao trocar a senha, as outras Sessões acabam).
func (v *Vault) Keep(sessionIDs ...string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	keep := map[string]bool{}
	for _, id := range sessionIDs {
		keep[id] = true
	}
	for id := range v.open {
		if !keep[id] {
			delete(v.open, id)
		}
	}
}

// Rekey cifra a chave de novo com a senha nova (ao trocar a senha do BrunOS).
func (v *Vault) Rekey(oldPassword, newPassword string) error {
	v.mu.Lock()
	f, err := v.load()
	v.mu.Unlock()
	if errors.Is(err, ErrNoKey) {
		return nil
	}
	if err != nil {
		return err
	}
	plain, err := f.openPEM(oldPassword)
	if err != nil {
		return err
	}
	salt, nonce, box, err := seal(newPassword, plain)
	if err != nil {
		return err
	}
	nf := *f
	nf.Salt, nf.Iter, nf.Nonce, nf.Box = b64(salt), iterations, b64(nonce), b64(box)
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.write(&nf)
}

// Delete apaga a chave do disco e da memória.
func (v *Vault) Delete() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.open = map[string]ssh.Signer{}
	v.cache = nil
	if err := os.Remove(v.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
