package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"brunos/internal/events"
)

const (
	bgInterval   = 5 * time.Minute // coleta em segundo plano (gráfico sem histórico ou aviso ligado)
	downAfter    = 5 * time.Minute // "sem responder" só depois disso
	fetchTimeout = 15 * time.Second
	maxParallel  = 3
	tickEvery    = 5 * time.Second
	saveEvery    = 10 * time.Minute
)

// entry é o cache de uma integração.
type entry struct {
	values    map[string]Value
	at        int64 // unix da última busca boa
	err       string
	tried     time.Time
	busy      bool
	downSince time.Time
}

// View é o que a interface recebe de uma integração.
type View struct {
	ID     string           `json:"id"`
	At     int64            `json:"at,omitempty"`
	Err    string           `json:"err,omitempty"`
	Values map[string]Value `json:"values"`
}

// Hub agenda as buscas, guarda o último valor e avisa as abas abertas.
type Hub struct {
	Store    *Store
	Series   *SeriesStore
	Events   *events.Store
	Client   *http.Client
	Now      func() time.Time
	ImageDir string // cache das imagens (fotos, capas); vazio = sem cache

	mu    sync.Mutex
	cache map[string]*entry
	live  map[string]bool            // integrações usadas por algum widget
	bg    map[string]map[string]bool // integração → métricas que precisam de histórico próprio
	subs  map[chan []byte]struct{}
	wake  chan struct{}
	sem   chan struct{}
	saved time.Time
}

func NewHub(st *Store, series *SeriesStore, ev *events.Store) *Hub {
	return &Hub{
		Store: st, Series: series, Events: ev, Now: time.Now,
		Client: &http.Client{Timeout: fetchTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		cache:  map[string]*entry{}, live: map[string]bool{}, bg: map[string]map[string]bool{},
		subs: map[chan []byte]struct{}{}, wake: make(chan struct{}, 1), sem: make(chan struct{}, maxParallel),
	}
}

func (h *Hub) poke() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// Run roda até ctx terminar. Sem nada para buscar, cada volta só confere alguns mapas.
func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	h.saved = h.Now()
	for {
		select {
		case <-ctx.Done():
			return // o main grava as séries ao desligar
		case <-t.C:
		case <-h.wake:
		}
		h.Tick(ctx)
	}
}

// Tick dispara as buscas vencidas (exportado para os testes).
func (h *Hub) Tick(ctx context.Context) {
	now := h.Now()
	viewers := h.Viewers() > 0
	for _, it := range h.Store.List() {
		h.mu.Lock()
		e := h.cache[it.ID]
		if e == nil {
			e = &entry{}
			h.cache[it.ID] = e
		}
		interval, ok := h.intervalLocked(it, viewers)
		due := ok && !e.busy && now.Sub(e.tried) >= interval
		if due {
			e.busy, e.tried = true, now
		}
		h.mu.Unlock()
		if due {
			go h.fetch(ctx, it)
		}
	}
	if now.Sub(h.saved) >= saveEvery {
		h.saved = now
		go h.Series.Save()
	}
}

// intervalLocked: de quanto em quanto tempo buscar esta integração agora (ok=false: não buscar).
func (h *Hub) intervalLocked(it Integration, viewers bool) (time.Duration, bool) {
	own := time.Duration(it.Interval) * time.Second
	if viewers && h.live[it.ID] {
		return own, true
	}
	if len(h.bg[it.ID]) > 0 || alertsOn(it) {
		return max(own, bgInterval), true
	}
	return 0, false
}

func alertsOn(it Integration) bool {
	for _, a := range it.Alerts {
		if a.On {
			return true
		}
	}
	return false
}

func (h *Hub) fetch(ctx context.Context, it Integration) {
	h.sem <- struct{}{}
	defer func() { <-h.sem }()
	k := KindByID(it.Kind)
	h.mu.Lock()
	var prev map[string]Value
	if e := h.cache[it.ID]; e != nil {
		prev = e.values
	} else if _, ok := h.Store.Get(it.ID); ok {
		h.cache[it.ID] = &entry{busy: true, tried: h.Now()}
	} else { // apagada enquanto esperava a vez
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	var vals map[string]Value
	cfg, err := h.Store.Config(it)
	if err == nil && k != nil {
		cfg.Prev = prev
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		vals, err = k.Fetch(fctx, h.Client, cfg)
		cancel()
	} else if k == nil {
		err = ferr("tipo de integração desconhecido")
	}
	if ctx.Err() != nil {
		return
	}
	now := h.Now()
	h.mu.Lock()
	e := h.cache[it.ID]
	if e == nil { // apagada durante a busca
		h.mu.Unlock()
		return
	}
	e.busy = false
	if err != nil {
		e.err = errText(err)
		if e.downSince.IsZero() {
			e.downSince = now
		}
	} else {
		e.values, e.at, e.err, e.downSince = vals, now.Unix(), "", time.Time{}
	}
	bg := h.bg[it.ID]
	downSince := e.downSince
	view := h.viewLocked(it.ID, e)
	h.mu.Unlock()

	if err == nil {
		for mid := range bg {
			if v, ok := vals[mid]; ok && v.Num != nil {
				h.Series.Add(it.ID+"."+mid, *v.Num)
			}
		}
	} else {
		slog.Debug("integração sem resposta", "nome", it.Name, "erro", err)
	}
	h.checkAlerts(it, prev, vals, err, downSince, now)
	h.publish(view)
}

func errText(err error) string {
	var fe *FetchError
	if errors.As(err, &fe) {
		return fe.Msg
	}
	return "não consegui buscar os dados"
}

func (h *Hub) viewLocked(id string, e *entry) View {
	v := View{ID: id, At: e.at, Err: e.err, Values: map[string]Value{}}
	for k, val := range e.values {
		if !strings.HasPrefix(k, "_") {
			v.Values[k] = val
		}
	}
	return v
}

// ── Avisos prontos ────────────────────────────────────────────────

func alertKey(id, alert string) string { return "integ:" + id + ":" + alert }

func (h *Hub) checkAlerts(it Integration, prev, vals map[string]Value, ferr error, downSince, now time.Time) {
	if h.Events == nil {
		return
	}
	k := KindByID(it.Kind)
	if k == nil {
		return
	}
	info := k.Info()
	units := map[string]string{}
	for _, m := range info.Metrics {
		units[m.ID] = m.Unit
	}
	for _, def := range info.Alerts {
		set := it.Alerts[def.ID]
		key := alertKey(it.ID, def.ID)
		if !set.On {
			h.Events.Resolve(key, nil)
			continue
		}
		if def.Op == "down" {
			switch {
			case ferr == nil:
				h.Events.Resolve(key, &events.Event{Kind: "integration-up", Level: events.Info,
					Title: it.Name + " voltou a responder"})
			case !downSince.IsZero() && now.Sub(downSince) >= downAfter:
				h.Events.Emit(events.Event{Kind: "integration-down", Level: def.Level, Key: key,
					Title: it.Name + " não está respondendo", Body: errText(ferr)})
			}
			continue
		}
		if ferr != nil {
			continue // sem dado novo: mantém o estado
		}
		if def.Op == "new" { // um aviso por item novo na lista (ex.: aparelho desconhecido na rede)
			seen := map[string]bool{}
			for _, item := range vals[def.Metric].List {
				if item.ID == "" {
					continue
				}
				seen[item.ID] = true
				body := strings.TrimSpace(strings.Trim(item.Text+" · "+def.Hint, " ·"))
				h.Events.Emit(events.Event{Kind: "integration-new", Level: def.Level, Key: key + ":" + item.ID,
					Title: def.Name + ": " + item.Label, Body: body})
			}
			for _, item := range prev[def.Metric].List {
				if item.ID != "" && !seen[item.ID] {
					h.Events.Resolve(key+":"+item.ID, nil) // saiu da lista (ficou conhecido ou foi apagado)
				}
			}
			continue
		}
		v, ok := vals[def.Metric]
		if !ok || v.Num == nil {
			continue
		}
		limit := def.Default
		if !def.Fixed && set.Value != 0 {
			limit = set.Value
		}
		bad := (def.Op == "<" && *v.Num < limit) || (def.Op == ">" && *v.Num > limit)
		if !bad {
			h.Events.Resolve(key, nil)
			continue
		}
		body := ""
		if !def.Fixed {
			u := def.Unit
			if u == "" {
				u = units[def.Metric]
			}
			body = fmt.Sprintf("Agora: %s %s (limite: %s %s)", fmtNum(*v.Num), u, fmtNum(limit), u)
		}
		h.Events.Emit(events.Event{Kind: "integration-alert", Level: def.Level, Key: key,
			Title: it.Name + ": " + lowerFirst(def.Name), Body: strings.TrimSpace(body)})
	}
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToLower(string(r[0])) + string(r[1:])
}

func fmtNum(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strings.Replace(strconv.FormatFloat(v, 'f', 1, 64), ".", ",", 1)
}

// ── Uso pelos widgets ─────────────────────────────────────────────

// SetUsage lê a preferência widgets2 e descobre o que precisa ser buscado:
// métrica num Espaço → integração "ao vivo"; número num gráfico ou mini-gráfico sem histórico do
// app → coleta em segundo plano.
func (h *Hub) SetUsage(settings map[string]any) {
	live := map[string]bool{}
	bg := map[string]map[string]bool{}
	w, _ := settings["widgets2"].(map[string]any)
	items, _ := w["items"].(map[string]any)
	defs := map[string]map[string]MetricDef{} // integração → métricas
	for _, it := range h.Store.List() {
		if k := KindByID(it.Kind); k != nil {
			m := map[string]MetricDef{}
			for _, d := range k.Info().Metrics {
				m[d.ID] = d
			}
			defs[it.ID] = m
		}
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		format, _ := item["format"].(string)
		slots, _ := item["slots"].([]any)
		for _, rs := range slots {
			slot, _ := rs.(map[string]any)
			metric, _ := slot["metric"].(string)
			iid, mid, ok := strings.Cut(metric, ".")
			if !ok || iid == "brunos" || defs[iid] == nil {
				continue
			}
			live[iid] = true
			spark, _ := slot["spark"].(bool)
			d, known := defs[iid][mid]
			if known && d.Kind == Num && d.History == "" && (format == "chart" || spark) {
				if bg[iid] == nil {
					bg[iid] = map[string]bool{}
				}
				bg[iid][mid] = true
			}
		}
	}
	h.mu.Lock()
	h.live, h.bg = live, bg
	h.mu.Unlock()
	h.poke()
}

// Changed é chamado depois de criar, editar ou apagar uma integração.
func (h *Hub) Changed(id string, deleted bool) {
	h.mu.Lock()
	if deleted {
		delete(h.cache, id)
	} else if e := h.cache[id]; e != nil {
		e.tried = time.Time{} // busca de novo já (endereço ou token podem ter mudado)
	}
	h.mu.Unlock()
	if h.Events != nil {
		it, ok := h.Store.Get(id)
		if k := KindByID(it.Kind); deleted || !ok || k == nil {
			// apagada: fecha todos os avisos abertos dela (sem conhecer o tipo, tenta os ids conhecidos)
			for _, ki := range Kinds() {
				for _, a := range ki.Alerts {
					h.Events.Resolve(alertKey(id, a.ID), nil)
				}
			}
		} else {
			for _, a := range k.Info().Alerts {
				if !it.Alerts[a.ID].On {
					h.Events.Resolve(alertKey(id, a.ID), nil)
				}
			}
		}
	}
	if deleted {
		h.Series.Forget(id + ".")
		h.publishRemoved(id)
	}
	h.poke()
}

// ── Abas abertas ──────────────────────────────────────────────────

// Subscribe recebe as mudanças (já em JSON) enquanto uma aba está aberta.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	h.poke() // alguém abriu: busca o que estiver velho
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *Hub) Viewers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

func (h *Hub) publish(v View) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.send(b)
}

func (h *Hub) publishRemoved(id string) {
	b, _ := json.Marshal(map[string]any{"id": id, "removed": true})
	h.send(b)
}

func (h *Hub) send(b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- b:
		default: // aba lenta: recebe no próximo valor
		}
	}
}

// Snapshot devolve o cache de todas as integrações (para o início do fluxo ao vivo).
func (h *Hub) Snapshot() []View {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []View{}
	for id, e := range h.cache {
		if e.at > 0 || e.err != "" {
			out = append(out, h.viewLocked(id, e))
		}
	}
	return out
}

// Test busca uma vez com a configuração dada, sem salvar nada.
func (h *Hub) Test(ctx context.Context, it Integration) (map[string]Value, error) {
	k := KindByID(it.Kind)
	if k == nil {
		return nil, ferr("tipo de integração desconhecido")
	}
	cfg, err := h.Store.Config(it)
	if err != nil {
		return nil, err
	}
	fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	vals, err := k.Fetch(fctx, h.Client, cfg)
	if err != nil {
		return nil, err
	}
	for k := range vals {
		if strings.HasPrefix(k, "_") {
			delete(vals, k)
		}
	}
	return vals, nil
}
