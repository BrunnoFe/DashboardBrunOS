package integrations

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"

	"brunos/internal/secret"
	"brunos/internal/store"
)

// Limites de frequência (decisão do Bruno: 10 s a 1 h).
const (
	MinInterval = 10
	MaxInterval = 3600
	maxItems    = 50
)

// AlertSetting é a escolha do Bruno para um Aviso pronto.
type AlertSetting struct {
	On    bool    `json:"on"`
	Value float64 `json:"value,omitempty"`
}

// Integration como fica no disco (Secret cifrado).
type Integration struct {
	ID       string                  `json:"id"`
	Kind     string                  `json:"kind"`
	Name     string                  `json:"name"`
	URL      string                  `json:"url"`
	User     string                  `json:"user,omitempty"`
	Secret   string                  `json:"secret,omitempty"`
	Interval int                     `json:"interval"`
	Alerts   map[string]AlertSetting `json:"alerts,omitempty"`
	Options  map[string]string       `json:"options,omitempty"` // campos extras (não secretos)
}

// Public é o que vai para o navegador: nunca o segredo.
type Public struct {
	ID        string                  `json:"id"`
	Kind      string                  `json:"kind"`
	Name      string                  `json:"name"`
	URL       string                  `json:"url"`
	User      string                  `json:"user,omitempty"`
	HasSecret bool                    `json:"hasSecret"`
	Interval  int                     `json:"interval"`
	Alerts    map[string]AlertSetting `json:"alerts"`
	Options   map[string]string       `json:"options"`
}

func (i Integration) Public() Public {
	a := i.Alerts
	if a == nil {
		a = map[string]AlertSetting{}
	}
	o := i.Options
	if o == nil {
		o = map[string]string{}
	}
	return Public{ID: i.ID, Kind: i.Kind, Name: i.Name, URL: i.URL, User: i.User, HasSecret: i.Secret != "", Interval: i.Interval, Alerts: a, Options: o}
}

// Input é o que chega da interface ao criar/editar. Secret nil = manter o atual; "" = apagar.
type Input struct {
	Kind     string                  `json:"kind"`
	Name     string                  `json:"name"`
	URL      string                  `json:"url"`
	User     string                  `json:"user"`
	Secret   *string                 `json:"secret"`
	Interval int                     `json:"interval"`
	Alerts   map[string]AlertSetting `json:"alerts"`
	Options  map[string]string       `json:"options"`
}

// ErrInput é um erro de validação com mensagem em pt-BR.
type ErrInput struct{ Msg string }

func (e *ErrInput) Error() string { return e.Msg }

var ErrNotFound = errors.New("integração não encontrada")

type file struct {
	Version      int           `json:"version"`
	Integrations []Integration `json:"integrations"`
}

// Store guarda as integrações em /data/integrations.json (0600, escrita atômica).
type Store struct {
	path string
	box  *secret.Box
	mu   sync.Mutex
	list []Integration
}

func NewStore(path string, box *secret.Box) *Store {
	s := &Store{path: path, box: box}
	var f file
	if _, err := store.ReadJSON(path, &f); err != nil {
		slog.Warn("integrações: arquivo ilegível, começando do zero", "erro", err)
	}
	s.list = f.Integrations
	return s
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	return store.WriteJSON(s.path, file{Version: 1, Integrations: s.list}, 0o600)
}

// List devolve uma cópia, por nome.
func (s *Store) List() []Integration {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Integration(nil), s.list...)
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func (s *Store) Get(id string) (Integration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.list {
		if it.ID == id {
			return it, true
		}
	}
	return Integration{}, false
}

// Config decifra a credencial para uma busca.
func (s *Store) Config(it Integration) (Config, error) {
	sec, err := s.box.Open(it.Secret)
	if err != nil {
		return Config{}, ferr("não foi possível ler a senha/token guardado (cadastre de novo)")
	}
	return Config{URL: it.URL, User: it.User, Secret: sec, Options: it.Options}, nil
}

// Validate confere a entrada e devolve a integração pronta (sem id e com o segredo anterior).
func (s *Store) Validate(in Input, prev *Integration) (Integration, error) {
	k := KindByID(in.Kind)
	if k == nil {
		return Integration{}, &ErrInput{"tipo de integração desconhecido"}
	}
	info := k.Info()
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = info.Name
	}
	if len([]rune(in.Name)) > 60 {
		return Integration{}, &ErrInput{"nome muito longo (máx. 60)"}
	}
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	if info.NoURL {
		in.URL = ""
	} else if u, err := url.Parse(in.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return Integration{}, &ErrInput{"endereço inválido: use http:// ou https:// seguido do IP ou nome e da porta"}
	}
	if len(in.User) > 200 {
		return Integration{}, &ErrInput{"usuário muito longo"}
	}
	if in.Interval == 0 {
		in.Interval = info.Interval
	}
	if in.Interval < MinInterval || in.Interval > MaxInterval {
		return Integration{}, &ErrInput{"a frequência deve ficar entre 10 segundos e 1 hora"}
	}
	it := Integration{Kind: in.Kind, Name: in.Name, URL: in.URL, User: strings.TrimSpace(in.User), Interval: in.Interval, Alerts: map[string]AlertSetting{}}
	if prev != nil {
		it.ID, it.Secret = prev.ID, prev.Secret
	}
	if in.Secret != nil {
		if len(*in.Secret) > 4096 {
			return Integration{}, &ErrInput{"senha/token muito longo"}
		}
		sealed, err := s.box.Seal(*in.Secret)
		if err != nil {
			return Integration{}, err
		}
		it.Secret = sealed
	}
	for _, f := range info.Fields {
		if f.Required && f.ID == "secret" && it.Secret == "" {
			return Integration{}, &ErrInput{f.Label + " é obrigatório"}
		}
		if f.ID == "url" || f.ID == "user" || f.ID == "secret" {
			continue
		}
		// campo extra (ex.: álbum): só os que o Tipo declara, texto curto
		v := strings.TrimSpace(in.Options[f.ID])
		if len(v) > 200 {
			return Integration{}, &ErrInput{f.Label + " muito longo"}
		}
		if v != "" {
			if it.Options == nil {
				it.Options = map[string]string{}
			}
			it.Options[f.ID] = v
		}
	}
	known := map[string]AlertDef{}
	for _, a := range info.Alerts {
		known[a.ID] = a
	}
	for id, a := range in.Alerts {
		def, ok := known[id]
		if !ok {
			continue
		}
		if def.Fixed {
			a.Value = 0
		}
		it.Alerts[id] = a
	}
	return it, nil
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Add cria uma integração nova.
func (s *Store) Add(in Input) (Integration, error) {
	it, err := s.Validate(in, nil)
	if err != nil {
		return it, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.list) >= maxItems {
		return it, &ErrInput{"limite de 50 integrações"}
	}
	it.ID = newID()
	s.list = append(s.list, it)
	return it, s.saveLocked()
}

// Update altera uma integração (o tipo não muda).
func (s *Store) Update(id string, in Input) (Integration, error) {
	prev, ok := s.Get(id)
	if !ok {
		return Integration{}, ErrNotFound
	}
	in.Kind = prev.Kind
	it, err := s.Validate(in, &prev)
	if err != nil {
		return it, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.list[i] = it
			return it, s.saveLocked()
		}
	}
	return Integration{}, ErrNotFound
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.list = append(s.list[:i], s.list[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}
