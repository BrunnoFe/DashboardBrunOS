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
	// 0.16.0: montar, desmontar, rótulo e Formatar
	mux.HandleFunc("POST /api/disks/mount", wrap(func(w http.ResponseWriter, r *http.Request) {
		var b struct{ UUID, Folder string }
		if !readBody(w, r, &b) {
			return
		}
		if err := m.Mount(r.Context(), b.UUID, b.Folder); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "path": "/mnt/" + b.Folder})
	}))
	mux.HandleFunc("POST /api/disks/unmount", wrap(func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Folder string }
		if !readBody(w, r, &b) {
			return
		}
		res, err := m.Unmount(r.Context(), b.Folder)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, res)
	}))
	mux.HandleFunc("POST /api/disks/label", wrap(func(w http.ResponseWriter, r *http.Request) {
		var b struct{ UUID, Label string }
		if !readBody(w, r, &b) {
			return
		}
		if err := m.Label(r.Context(), b.UUID, b.Label); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /api/disks/format", wrap(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, m.FormatInfo(r.Context()))
	}))
	// formatar: a senha do BrunOS de novo (quem pegou uma sessão aberta não formata) e a Senha de
	// formatação, que só o servidor confere
	mux.HandleFunc("POST /api/disks/format", wrap(func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Dev, FS, Label, Password, FormatPassword string }
		if !readBody(w, r, &b) {
			return
		}
		if m.Verify == nil || !m.Verify(w, r, b.Password) {
			if m.Verify == nil {
				writeJSON(w, 500, map[string]string{"error": "formatar indisponível"})
			}
			return
		}
		if err := m.Format(r.Context(), b.Dev, b.FS, b.Label, b.FormatPassword); err != nil {
			code := 502
			switch {
			case errors.Is(err, ErrNoDisk):
				code = 404
			case errors.Is(err, ErrFormatBusy):
				code = 409
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 202, m.FormatInfo(r.Context()))
	}))
}

func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(v); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return false
	}
	return true
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
