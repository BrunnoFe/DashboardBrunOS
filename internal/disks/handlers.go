package disks

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Register liga as rotas da janela Discos (wrap aplica autenticação e CSRF):
//
//	GET  /api/disks                lista com SMART, temperatura, espaço e apps (pede de novo se passou 1 min)
//	POST /api/disks/refresh        pede a lista agora
//	GET  /api/disks/timeline?key=  Linha do tempo de um disco
//	GET  /api/disks/state?dev=     dormindo ou acordado (não acorda o disco)
//	GET  /api/disks/errors?dev=    linhas do registro do Linux sobre o disco (30 dias)
//	POST /api/disks/speed          {dev}: teste de velocidade de leitura (em segundo plano)
//	POST /api/disks/eject          {dev}: ejetar com segurança (só USB sem nada montado)
func (m *Monitor) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/disks", wrap(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		stale, never, busy := m.Now().Sub(m.at) >= staleAfter, m.at.IsZero(), m.loading
		m.mu.Unlock()
		if stale && !busy {
			done := m.start()
			if never { // primeira vez: espera um pouco para já mostrar a lista
				select {
				case <-done:
				case <-time.After(quickWait):
				}
			}
		}
		writeJSON(w, 200, m.Snapshot(r.Context()))
	}))
	mux.HandleFunc("POST /api/disks/refresh", wrap(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-m.start():
		case <-time.After(quickWait):
		}
		writeJSON(w, 200, m.Snapshot(r.Context()))
	}))
	mux.HandleFunc("GET /api/disks/timeline", wrap(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"events": m.Timeline(r.URL.Query().Get("key"))})
	}))
	mux.HandleFunc("GET /api/disks/state", wrap(func(w http.ResponseWriter, r *http.Request) {
		st, why, err := m.State(r.Context(), r.URL.Query().Get("dev"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"state": st, "why": why})
	}))
	mux.HandleFunc("GET /api/disks/errors", wrap(func(w http.ResponseWriter, r *http.Request) {
		lines, err := m.Errors(r.Context(), r.URL.Query().Get("dev"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"lines": lines})
	}))
	mux.HandleFunc("POST /api/disks/speed", wrap(func(w http.ResponseWriter, r *http.Request) {
		dev, ok := readDev(w, r)
		if !ok {
			return
		}
		if err := m.StartSpeed(dev); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 202, m.Snapshot(r.Context()))
	}))
	mux.HandleFunc("POST /api/disks/eject", wrap(func(w http.ResponseWriter, r *http.Request) {
		dev, ok := readDev(w, r)
		if !ok {
			return
		}
		if err := m.Eject(r.Context(), dev); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))
}

func readDev(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Dev string `json:"dev"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || body.Dev == "" {
		writeJSON(w, 400, map[string]string{"error": "faltou dizer qual disco"})
		return "", false
	}
	return body.Dev, true
}

func fail(w http.ResponseWriter, err error) {
	code := 502
	switch {
	case errors.Is(err, ErrNoDisk):
		code = 404
	case errors.Is(err, ErrBusy):
		code = 429
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
