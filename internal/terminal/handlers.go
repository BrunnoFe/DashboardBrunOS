package terminal

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Register liga as rotas /api/terminal/*. wrap aplica autenticação e CSRF.
//
// Transporte sem WebSocket: a saída chega por Server-Sent Events (como as estatísticas) e as
// teclas vão por POST. Na rede local a latência é imperceptível e não precisa de biblioteca extra.
func (m *Manager) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/terminal/sessions", wrap(m.hList))
	mux.HandleFunc("POST /api/terminal/open", wrap(m.hOpen))
	mux.HandleFunc("GET /api/terminal/stream", wrap(m.hStream))
	mux.HandleFunc("POST /api/terminal/input", wrap(m.hInput))
	mux.HandleFunc("POST /api/terminal/resize", wrap(m.hResize))
	mux.HandleFunc("POST /api/terminal/close", wrap(m.hClose))
	mux.HandleFunc("POST /api/terminal/forget", wrap(m.hForget))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request, v any, limit int64) error {
	return json.NewDecoder(io.LimitReader(r.Body, limit)).Decode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var unk *UnknownHostError
	var mis *HostKeyMismatchError
	switch {
	case errors.As(err, &unk):
		writeJSON(w, 409, map[string]any{"error": unk.Error(), "unknownHost": true, "addr": unk.Addr, "keyType": unk.KeyType, "fingerprint": unk.Fingerprint})
	case errors.As(err, &mis):
		writeJSON(w, 409, map[string]any{"error": mis.Error(), "hostKeyChanged": true, "addr": mis.Addr, "keyType": mis.KeyType, "fingerprint": mis.Fingerprint, "known": mis.Known})
	case errors.Is(err, ErrNoSession):
		writeJSON(w, 404, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrBadRequest), errors.Is(err, ErrBadKey):
		writeJSON(w, 400, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrAuth):
		// 403 e não 401: 401 faria a interface achar que a sessão do BrunOS expirou
		writeJSON(w, 403, map[string]string{"error": err.Error(), "sshAuth": "1"})
	case errors.Is(err, ErrTooMany), errors.Is(err, ErrRateLimited):
		writeJSON(w, 429, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, 502, map[string]string{"error": err.Error()})
	}
}

func (m *Manager) hList(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, m.List()) }

func (m *Manager) hOpen(w http.ResponseWriter, r *http.Request) {
	var req OpenRequest
	if err := decode(r, &req, 64<<10); err != nil {
		writeErr(w, ErrBadRequest)
		return
	}
	s, err := m.Open(req)
	req.Password, req.Passphrase, req.Key = "", "", ""
	if err != nil {
		slog.Info("terminal: conexão recusada", "destino", req.Host, "usuario", req.User, "erro", err)
		writeErr(w, err)
		return
	}
	slog.Info("terminal: sessão aberta", "destino", s.Addr, "usuario", s.User)
	writeJSON(w, 200, map[string]any{"id": s.ID, "addr": s.Addr, "user": s.User})
}

func (m *Manager) hStream(w http.ResponseWriter, r *http.Request) {
	s, err := m.Get(r.URL.Query().Get("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "stream indisponível"})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	gen, detach := s.Attach()
	defer detach()
	fmt.Fprint(w, "retry: 2000\nevent: ready\ndata: {}\n\n")
	flusher.Flush()
	keep := time.NewTicker(20 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keep.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
			continue
		case <-s.Notify():
		}
		out, done, msg, stale := s.Next(gen)
		if stale {
			fmt.Fprint(w, "event: replaced\ndata: {}\n\n")
			flusher.Flush()
			return
		}
		if len(out) > 0 {
			fmt.Fprintf(w, "data: %s\n\n", base64.StdEncoding.EncodeToString(out))
			flusher.Flush()
			s.signal() // confere de novo: pode ter chegado mais saída ou o fim da sessão
		}
		if done {
			b, _ := json.Marshal(map[string]string{"message": msg})
			fmt.Fprintf(w, "event: exit\ndata: %s\n\n", b)
			flusher.Flush()
			m.Close(s.ID)
			return
		}
	}
}

func (m *Manager) hInput(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID   string `json:"id"`
		Data string `json:"data"`
	}
	if err := decode(r, &body, 256<<10); err != nil {
		writeErr(w, ErrBadRequest)
		return
	}
	s, err := m.Get(body.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Write(body.Data); err != nil {
		writeErr(w, ErrNoSession)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) hResize(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID   string `json:"id"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := decode(r, &body, 4<<10); err != nil {
		writeErr(w, ErrBadRequest)
		return
	}
	s, err := m.Get(body.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	_ = s.Resize(body.Cols, body.Rows)
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) hClose(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := decode(r, &body, 4<<10); err != nil {
		writeErr(w, ErrBadRequest)
		return
	}
	m.Close(body.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) hForget(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := decode(r, &body, 4<<10); err != nil || !hostRe.MatchString(body.Host) {
		writeErr(w, ErrBadRequest)
		return
	}
	if err := m.Forget(body.Host, body.Port); err != nil {
		writeJSON(w, 500, map[string]string{"error": "não deu para apagar a chave guardada"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
