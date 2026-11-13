package smart

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// quickWait: quanto o pedido espera a leitura antes de responder "lendo" (202). Erros rápidos (sem
// conexão configurada, script antigo) e leituras curtas voltam na hora.
var quickWait = 1500 * time.Millisecond

// Register liga GET /api/smart, POST /api/smart/refresh ("Ler agora") e POST /api/smart/retry
// ("Tentar de novo" de um disco sem SMART). As leituras rodam em segundo plano: a interface acompanha
// pelo campo "running" do GET.
func (m *Monitor) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/smart", wrap(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, m.Snapshot())
	}))
	mux.HandleFunc("POST /api/smart/refresh", wrap(func(w http.ResponseWriter, r *http.Request) {
		m.answer(w, m.Start())
	}))
	mux.HandleFunc("POST /api/smart/retry", wrap(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || body.Key == "" {
			writeJSON(w, 400, map[string]string{"error": "faltou dizer qual disco"})
			return
		}
		m.answer(w, m.Retry(body.Key))
	}))
	// teste do próprio disco (0.15.0): kind = curto ou longo
	mux.HandleFunc("POST /api/smart/selftest", wrap(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Key  string `json:"key"`
			Kind string `json:"kind"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || body.Key == "" {
			writeJSON(w, 400, map[string]string{"error": "faltou dizer qual disco"})
			return
		}
		if err := m.SelfTest(r.Context(), body.Key, body.Kind); err != nil {
			code := 502
			switch {
			case errors.Is(err, ErrBusy):
				code = 429
			case errors.Is(err, ErrNoDisk):
				code = 404
			case errors.Is(err, ErrTestRunning):
				code = 409
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, m.Snapshot())
	}))
}

func (m *Monitor) answer(w http.ResponseWriter, done <-chan error) {
	select {
	case err := <-done:
		if err != nil {
			code := 502
			switch {
			case errors.Is(err, ErrBusy):
				code = 429
			case errors.Is(err, ErrNoDisk):
				code = 404
			}
			writeJSON(w, code, map[string]string{"error": friendly(err)})
			return
		}
		writeJSON(w, 200, m.Snapshot())
	case <-time.After(quickWait):
		writeJSON(w, 202, m.Snapshot()) // continua lendo; o GET mostra quando terminar
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
