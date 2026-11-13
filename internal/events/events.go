// Package events é a central de notificações do BrunOS (o sino da tela inicial).
//
// Cada parte do sistema chama Emit com um aviso. Avisos com Key (ex.: "disk-unmounted:Disco1")
// não se repetem enquanto o problema continuar: só voltam a aparecer depois de Resolve(key).
// Tudo fica em /data/events.json (no máximo 500 avisos e 30 dias).
package events

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sort"
	"sync"
	"time"

	"brunos/internal/store"
)

const (
	maxEvents = 500
	maxAge    = 30 * 24 * time.Hour
)

// Níveis de importância.
const (
	Info    = "info"
	Aviso   = "aviso"
	Critico = "critico"
)

type Event struct {
	ID    string         `json:"id"`
	Time  int64          `json:"time"`
	Kind  string         `json:"kind"`  // ex.: power-outage, power-restored, disk-unmounted
	Level string         `json:"level"` // info | aviso | critico
	Title string         `json:"title"`
	Body  string         `json:"body,omitempty"`
	Read  bool           `json:"read"`
	Key   string         `json:"key,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

type file struct {
	Version int      `json:"version"`
	Events  []Event  `json:"events"`
	Active  []string `json:"active"` // problemas ainda abertos (não avisar de novo)
}

type Store struct {
	File string
	Now  func() time.Time

	mu     sync.Mutex
	list   []Event // mais antigo primeiro
	active map[string]bool
	subs   map[chan Event]struct{}
}

// New lê o arquivo (se existir). file vazio = só em memória (testes).
func New(path string) *Store {
	s := &Store{File: path, Now: time.Now, active: map[string]bool{}, subs: map[chan Event]struct{}{}}
	if path != "" {
		var f file
		if _, err := store.ReadJSON(path, &f); err != nil {
			slog.Warn("eventos: arquivo ilegível, começando do zero", "erro", err)
		}
		s.list = f.Events
		for _, k := range f.Active {
			s.active[k] = true
		}
	}
	return s
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Emit registra um aviso. Devolve false se for um problema (Key) que já está aberto.
func (s *Store) Emit(e Event) bool {
	s.mu.Lock()
	if e.Key != "" && s.active[e.Key] {
		s.mu.Unlock()
		return false
	}
	if e.Key != "" {
		s.active[e.Key] = true
	}
	ev := s.addLocked(e)
	s.mu.Unlock()
	s.publish(ev)
	return true
}

// Resolve marca o problema Key como resolvido. Se ele estava aberto e e != nil, registra e
// como aviso de "voltou ao normal". Devolve true se o problema estava aberto.
func (s *Store) Resolve(key string, e *Event) bool {
	s.mu.Lock()
	if !s.active[key] {
		s.mu.Unlock()
		return false
	}
	delete(s.active, key)
	var ev Event
	if e != nil {
		c := *e
		c.Key = ""
		ev = s.addLocked(c)
	} else {
		s.saveLocked()
	}
	s.mu.Unlock()
	if e != nil {
		s.publish(ev)
	}
	return true
}

// IsActive diz se o problema Key está aberto.
func (s *Store) IsActive(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[key]
}

func (s *Store) addLocked(e Event) Event {
	now := s.Now()
	e.ID, e.Time, e.Read = newID(), now.Unix(), false
	if e.Level == "" {
		e.Level = Info
	}
	s.list = append(s.list, e)
	s.pruneLocked(now)
	s.saveLocked()
	return e
}

func (s *Store) pruneLocked(now time.Time) {
	cut := now.Add(-maxAge).Unix()
	i := 0
	for i < len(s.list) && s.list[i].Time < cut {
		i++
	}
	if n := len(s.list) - i; n > maxEvents {
		i = len(s.list) - maxEvents
	}
	if i > 0 {
		s.list = append([]Event(nil), s.list[i:]...)
	}
}

func (s *Store) saveLocked() {
	if s.File == "" {
		return
	}
	act := make([]string, 0, len(s.active))
	for k := range s.active {
		act = append(act, k)
	}
	sort.Strings(act)
	if err := store.WriteJSON(s.File, file{Version: 1, Events: s.list, Active: act}, 0o600); err != nil {
		slog.Error("eventos: não foi possível gravar", "erro", err)
	}
}

// List devolve os avisos do mais novo para o mais antigo e quantos estão sem ler.
func (s *Store) List() ([]Event, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.list))
	unread := 0
	for i, e := range s.list {
		out[len(s.list)-1-i] = e
		if !e.Read {
			unread++
		}
	}
	return out, unread
}

// MarkRead marca como lidos os avisos indicados (nenhum id = todos).
func (s *Store) MarkRead(ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	changed := false
	for i := range s.list {
		if !s.list[i].Read && (len(ids) == 0 || want[s.list[i].ID]) {
			s.list[i].Read, changed = true, true
		}
	}
	if changed {
		s.saveLocked()
	}
}

// Clear apaga a lista (os problemas abertos continuam lembrados, para não avisar de novo).
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list = nil
	s.saveLocked()
}

// Subscribe recebe cada aviso novo (usado pelo stream da tela inicial).
func (s *Store) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 8)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

func (s *Store) publish(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- e:
		default: // aba lenta: ela relê a lista quando precisar
		}
	}
}
