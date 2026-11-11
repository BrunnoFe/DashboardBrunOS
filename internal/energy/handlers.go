package energy

import (
	"encoding/json"
	"io"
	"net/http"
)

// Register liga GET /api/energy, PUT /api/energy/config e GET /api/energy/history.
func (m *Monitor) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/energy", wrap(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"config": m.Config(), "now": m.Current()})
	}))
	mux.HandleFunc("PUT /api/energy/config", wrap(func(w http.ResponseWriter, r *http.Request) {
		var c Config
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&c); err != nil {
			writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
			return
		}
		if err := m.SetConfig(c); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		go m.Sample(r.Context()) // mostra logo o consumo do Medidor novo
		writeJSON(w, 200, map[string]any{"config": m.Config(), "now": m.Current()})
	}))
	mux.HandleFunc("GET /api/energy/history", wrap(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"minutes": m.Series(), "days": m.Days()})
	}))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
