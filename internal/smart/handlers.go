package smart

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Register liga GET /api/smart e POST /api/smart/refresh ("Ler agora").
func (m *Monitor) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/smart", wrap(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, m.Snapshot())
	}))
	mux.HandleFunc("POST /api/smart/refresh", wrap(func(w http.ResponseWriter, r *http.Request) {
		if err := m.Refresh(r.Context()); err != nil {
			code := 502
			if errors.Is(err, ErrBusy) {
				code = 429
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, m.Snapshot())
	}))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
