// Package integrations liga o BrunOS aos apps da casa (AdGuard, Speedtest Tracker...).
//
// Cada Tipo de integração traduz a API de um app em Métricas com nome em pt-BR. Uma Integração é
// um serviço daquele tipo (endereço + credencial cifrada). O Hub só busca o que algum Widget usa, e
// só enquanto alguém está com o BrunOS aberto (decisão 32); o que estiver num gráfico sem histórico
// próprio, ou tiver um Aviso pronto ligado, é coletado em segundo plano a cada 5 min.
// Termos: CONTEXT.md.
package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"time"
)

// Tipos de valor de uma Métrica.
const (
	Num    = "num"    // número
	Text   = "text"   // texto curto
	List   = "list"   // lista de itens (rótulo + valor)
	Series = "series" // série no tempo
	Media  = "media"  // lista de itens com imagem (fotos do Immich, capa do Plex) para o formato Mídia
)

// Range é uma faixa de cor padrão: até Max (inclusive) usa Level (ok | aviso | critico).
// O último item pode ter Max nulo (= sem limite).
type Range struct {
	Max   *float64 `json:"max"`
	Level string   `json:"level"`
}

// MetricDef descreve uma Métrica que um Tipo oferece.
type MetricDef struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Kind   string  `json:"kind"`
	Unit   string  `json:"unit,omitempty"` // "", "%", "ms", "Mb/s", "B", "s"
	Icon   string  `json:"icon,omitempty"`
	Ranges []Range `json:"ranges,omitempty"`
	// Série com o histórico deste número que o próprio app guarda (para gráficos e mini-gráficos).
	History string `json:"history,omitempty"`
	// Mídia: "photo" (foto cobrindo o widget) ou "poster" (capa em pé ao lado do texto).
	Look string `json:"look,omitempty"`
}

// Field é um campo do formulário da integração.
type Field struct {
	ID          string `json:"id"` // url | user | secret
	Label       string `json:"label"`
	Type        string `json:"type"` // url | text | secret
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// AlertDef é um Aviso pronto: "Métrica Op Valor" (ex.: download < 100 Mb/s).
// Op "down" = a integração parou de responder.
type AlertDef struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Desc    string  `json:"desc,omitempty"`
	Metric  string  `json:"metric,omitempty"`
	Op      string  `json:"op"` // < | > | down
	Default float64 `json:"default,omitempty"`
	Unit    string  `json:"unit,omitempty"`
	Level   string  `json:"level"`           // info | aviso | critico
	Fixed   bool    `json:"fixed,omitempty"` // sem número ajustável (só liga/desliga)
	// Op "new": avisa uma vez por item (Item.ID) que aparece na lista Metric; Hint vai no aviso.
	Hint string `json:"-"`
}

// KindInfo é o que a interface precisa saber de um Tipo.
type KindInfo struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Desc     string      `json:"desc"`
	Fields   []Field     `json:"fields"`
	Interval int         `json:"interval"` // segundos (padrão)
	Metrics  []MetricDef `json:"metrics"`
	Alerts   []AlertDef  `json:"alerts"`
	Runtipi  []string    `json:"-"` // ids de app no Runtipi que são deste tipo (sugestão)
	// NoURL: o tipo não fala com o app por HTTP (Tailscale vai pelo SSH do servidor).
	NoURL bool   `json:"noUrl,omitempty"`
	Note  string `json:"note,omitempty"` // explicação extra no formulário
}

// Item de uma lista. ID identifica o item entre buscas (ex.: MAC de um aparelho, para o aviso de
// aparelho novo); Img é a referência de uma imagem servida por /api/integrations/{id}/image.
type Item struct {
	Label string   `json:"k"`
	Text  string   `json:"v,omitempty"`
	Num   *float64 `json:"n,omitempty"`
	ID    string   `json:"id,omitempty"`
	Img   string   `json:"img,omitempty"`
}

// SeriesData é uma série com passo fixo; Points usa NaN para "sem dado" (vira null no JSON).
type SeriesData struct {
	Step   int64     `json:"step"`  // segundos entre pontos
	Start  int64     `json:"start"` // unix do primeiro ponto
	Points []float64 `json:"-"`
}

func (s SeriesData) MarshalJSON() ([]byte, error) {
	pts := make([]*float64, len(s.Points))
	for i, v := range s.Points {
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			v := v
			pts[i] = &v
		}
	}
	return json.Marshal(struct {
		Step   int64      `json:"step"`
		Start  int64      `json:"start"`
		Points []*float64 `json:"points"`
	}{s.Step, s.Start, pts})
}

func (s *SeriesData) UnmarshalJSON(b []byte) error {
	var raw struct {
		Step   int64      `json:"step"`
		Start  int64      `json:"start"`
		Points []*float64 `json:"points"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	s.Step, s.Start, s.Points = raw.Step, raw.Start, make([]float64, len(raw.Points))
	for i, p := range raw.Points {
		if p == nil {
			s.Points[i] = math.NaN()
		} else {
			s.Points[i] = *p
		}
	}
	return nil
}

// Value é o valor atual de uma Métrica (um dos campos preenchido).
type Value struct {
	Num    *float64    `json:"n,omitempty"`
	Text   *string     `json:"t,omitempty"`
	List   []Item      `json:"l,omitempty"`
	Series *SeriesData `json:"s,omitempty"`
}

func N(v float64) Value {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Value{}
	}
	return Value{Num: &v}
}
func T(s string) Value { return Value{Text: &s} }
func fp(v float64) *float64 {
	return &v
}

// Config é o que um Tipo recebe para buscar: endereço e credenciais já decifradas.
type Config struct {
	URL    string
	User   string
	Secret string
	// Prev são os valores da busca anterior (para não baixar de novo o que não mudou).
	// Métricas que começam com "_" são internas: ficam no cache, mas não vão para a interface.
	Prev map[string]Value
	// Options são os campos extras do formulário (ex.: álbum do Immich).
	Options map[string]string
}

// ImageKind é um Tipo que serve imagens (fotos, capas). ref vem de Item.Img e é conferido pelo Tipo.
type ImageKind interface {
	Image(ctx context.Context, c *http.Client, cfg Config, ref string) (io.ReadCloser, error)
}

// HostQuery faz um pedido de leitura ao script do servidor (pela conexão SSH do desligamento
// seguro). Definido pelo main; nil = indisponível.
var HostQuery func(ctx context.Context, cmd string) ([]byte, error)

// Kind é um Tipo de integração.
type Kind interface {
	Info() KindInfo
	Fetch(ctx context.Context, c *http.Client, cfg Config) (map[string]Value, error)
}

var kinds = map[string]Kind{}

func register(k Kind) { kinds[k.Info().ID] = k }

// KindByID devolve o Tipo (nil se não existir).
func KindByID(id string) Kind { return kinds[id] }

// Kinds lista os Tipos em ordem alfabética do nome.
func Kinds() []KindInfo {
	out := make([]KindInfo, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, k.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ── ajudantes para os tradutores ──────────────────────────────────

// maxBody limita o tamanho de uma resposta de app (protege a memória do container).
const maxBody = 2 << 20

// FetchError é um erro com mensagem em pt-BR para mostrar na interface.
type FetchError struct{ Msg string }

func (e *FetchError) Error() string { return e.Msg }

func ferr(format string, a ...any) error { return &FetchError{fmt.Sprintf(format, a...)} }

// getJSON faz GET em url com os cabeçalhos dados e decodifica a resposta em v.
func getJSON(ctx context.Context, c *http.Client, url string, hdr map[string]string, user, pass string, v any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, ferr("endereço inválido")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "BrunOS")
	for k, val := range hdr {
		req.Header.Set(k, val)
	}
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := c.Do(req)
	if err != nil {
		var ue interface{ Timeout() bool }
		if errors.As(err, &ue) && ue.Timeout() {
			return 0, ferr("o app demorou demais para responder")
		}
		return 0, ferr("não consegui falar com o app (confira o endereço e se ele está ligado)")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		return resp.StatusCode, ferr("o app recusou o acesso (confira usuário, senha ou token)")
	case resp.StatusCode == http.StatusNotFound:
		return resp.StatusCode, ferr("o app respondeu 404 (endereço ou versão diferente do esperado)")
	case resp.StatusCode >= 300:
		return resp.StatusCode, ferr("o app respondeu com erro %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(v); err != nil {
		return resp.StatusCode, ferr("resposta do app em formato inesperado")
	}
	return resp.StatusCode, nil
}

// nowFunc existe para os testes.
var nowFunc = time.Now
