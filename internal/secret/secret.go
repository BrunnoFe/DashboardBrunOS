// Package secret cifra pequenos segredos (tokens e senhas das integrações) com AES-256-GCM.
//
// A chave é aleatória e fica em /data/secret.key (0600), separada dos dados cifrados. Isso protege
// contra vazar só o arquivo das integrações (uma cópia ou um backup parcial). Ver decisão 33.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"brunos/internal/store"
)

const keySize = 32

// Box cifra e decifra com uma chave fixa.
type Box struct{ aead cipher.AEAD }

// Load lê a chave de path ou cria uma nova (0600) se o arquivo não existir.
func Load(path string) (*Box, error) {
	key, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		key = make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := store.WriteFile(path, key, 0o600); err != nil {
			return nil, fmt.Errorf("gravar chave: %w", err)
		}
	case err != nil:
		return nil, err
	case len(key) != keySize:
		return nil, fmt.Errorf("chave em %s tem %d bytes (esperado %d)", path, len(key), keySize)
	}
	return New(key)
}

// New cria um Box com a chave dada (32 bytes).
func New(key []byte) (*Box, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("chave precisa ter %d bytes", keySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal cifra texto e devolve base64 (nonce + texto cifrado). Texto vazio vira "".
func (b *Box) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// ErrDecrypt indica chave errada ou dado corrompido.
var ErrDecrypt = errors.New("não foi possível decifrar (chave diferente ou dado corrompido)")

// Open decifra o que Seal produziu. "" vira "".
func (b *Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", ErrDecrypt
	}
	n := b.aead.NonceSize()
	if len(raw) < n+b.aead.Overhead() {
		return "", ErrDecrypt
	}
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}
