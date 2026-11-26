package integrations

// Ações de integração (decisão 73, revê a 38): comandos leves e reversíveis que o BrunOS manda para o
// app de uma Integração, como pausar o AdGuard por 10 minutos. Nunca apagam nada. Cada Tipo diz quais
// Ações tem (KindInfo.Actions) e as executa (ActionKind). Depois de uma Ação, a integração é buscada de
// novo na hora para a tela mostrar o estado novo.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// ActionDef descreve uma Ação para a interface.
type ActionDef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ActionReq é o pedido de uma Ação. Minutes serve às pausas (0 = até ligar de novo).
type ActionReq struct {
	Action  string `json:"action"`
	Minutes int    `json:"minutes"`
}

// ActionKind é um Tipo que aceita Ações. Devolve um texto curto para o registro (ex.: "pausar 30 min").
type ActionKind interface {
	Action(ctx context.Context, c *http.Client, cfg Config, req ActionReq) (string, error)
}

const maxPauseMinutes = 24 * 60

// DoAction executa uma Ação numa integração, registra nos Eventos e busca de novo.
func (h *Hub) DoAction(ctx context.Context, id string, req ActionReq) error {
	it, ok := h.Store.Get(id)
	if !ok {
		return ErrNotFound
	}
	k := KindByID(it.Kind)
	ak, ok := k.(ActionKind)
	if !ok || !hasAction(k.Info(), req.Action) {
		return &ErrInput{"ação desconhecida"}
	}
	if req.Minutes < 0 || req.Minutes > maxPauseMinutes {
		return &ErrInput{"tempo inválido"}
	}
	cfg, err := h.Store.Config(it)
	if err != nil {
		return err
	}
	actx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	what, err := ak.Action(actx, h.Client, cfg, req)
	if err != nil {
		return err
	}
	// só no registro do servidor: um aviso no sino a cada pausa seria barulho (o "até ligar" já tem o
	// Aviso pronto de proteção desligada)
	slog.Info("integração: ação", "nome", it.Name, "ação", what)
	h.mu.Lock()
	if e := h.cache[id]; e != nil {
		e.tried = time.Time{}
	}
	h.mu.Unlock()
	h.poke()
	return nil
}

func hasAction(info KindInfo, id string) bool {
	for _, a := range info.Actions {
		if a.ID == id {
			return true
		}
	}
	return false
}

func (h *Hub) hAction(w http.ResponseWriter, r *http.Request) {
	var req ActionReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		writeErr(w, &ErrInput{"requisição inválida"})
		return
	}
	if err := h.DoAction(r.Context(), r.PathValue("id"), req); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
