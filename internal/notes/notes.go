// Package notes guarda o texto dos widgets de Notas em /data/notes.json.
//
// Leve de propósito: o arquivo só é lido ao pedir uma nota e gravado quando o Bruno digita (o
// navegador espera uma pausa antes de enviar). Quando uma nota muda, um aviso mínimo ("nota X
// mudou") vai pela conexão ao vivo que já existe (/api/stats/stream) e só os outros aparelhos
// buscam a nota de novo. Vale a última gravação.
package notes

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"

	"brunos/internal/store"
)

const (
	MaxChars = 20_000
	MaxNotes = 200
)

var (
	idRe       = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)
	ErrTooLong = errors.New("a nota passou de 20 mil caracteres")
	ErrTooMany = errors.New("notas demais (máx. 200)")
	ErrBadNote = errors.New("nota inválida")
)

type Note struct {
	Text string `json:"text"`
	At   int64  `json:"at"` // unix ms da última gravação
}

// Change é o aviso enviado pela conexão ao vivo.
type Change struct {
	ID string `json:"id"`
	At int64  `json:"at"`
}

type Store struct {
	path string
	mu   sync.Mutex
	subs map[chan Change]struct{}
}

func New(path string) *Store { return &Store{path: path, subs: map[chan Change]struct{}{}} }

func (s *Store) read() map[string]Note {
	m := map[string]Note{}
	if _, err := store.ReadJSON(s.path, &m); err != nil || m == nil {
		return map[string]Note{}
	}
	return m
}

func (s *Store) Get(id string) (Note, bool) {
	if !idRe.MatchString(id) {
		return Note{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.read()[id]
	return n, ok
}

func (s *Store) Put(id, text string) (Note, error) {
	if !idRe.MatchString(id) || !utf8.ValidString(text) {
		return Note{}, ErrBadNote
	}
	if utf8.RuneCountInString(text) > MaxChars {
		return Note{}, ErrTooLong
	}
	s.mu.Lock()
	m := s.read()
	if _, ok := m[id]; !ok && len(m) >= MaxNotes {
		s.mu.Unlock()
		return Note{}, ErrTooMany
	}
	n := Note{Text: text, At: time.Now().UnixMilli()}
	m[id] = n
	err := store.WriteJSON(s.path, m, 0o600)
	s.mu.Unlock()
	if err != nil {
		return Note{}, err
	}
	s.publish(Change{ID: id, At: n.At})
	return n, nil
}

func (s *Store) Delete(id string) error {
	if !idRe.MatchString(id) {
		return ErrBadNote
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.read()
	if _, ok := m[id]; !ok {
		return nil
	}
	delete(m, id)
	return store.WriteJSON(s.path, m, 0o600)
}

// Subscribe recebe os avisos de nota mudada (para a conexão ao vivo).
func (s *Store) Subscribe() (<-chan Change, func()) {
	ch := make(chan Change, 8)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

func (s *Store) publish(c Change) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- c:
		default: // conexão lenta: perde o aviso, a nota atualiza ao reabrir
		}
	}
}

// Register liga GET/PUT/DELETE /api/notes/{id}. wrap aplica autenticação e CSRF.
func (s *Store) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/notes/{id}", wrap(func(w http.ResponseWriter, r *http.Request) {
		n, _ := s.Get(r.PathValue("id")) // nota nova: texto vazio
		writeJSON(w, 200, n)
	}))
	mux.HandleFunc("PUT /api/notes/{id}", wrap(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 128<<10)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "nota inválida (máx. 20 mil caracteres)"})
			return
		}
		n, err := s.Put(r.PathValue("id"), body.Text)
		if err != nil {
			code := 400
			if !errors.Is(err, ErrTooLong) && !errors.Is(err, ErrTooMany) && !errors.Is(err, ErrBadNote) {
				code, err = 500, errors.New("não foi possível salvar a nota")
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, n)
	}))
	mux.HandleFunc("DELETE /api/notes/{id}", wrap(func(w http.ResponseWriter, r *http.Request) {
		if err := s.Delete(r.PathValue("id")); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
