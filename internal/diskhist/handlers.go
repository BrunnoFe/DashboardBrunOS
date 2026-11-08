package diskhist

import (
	"encoding/json"
	"net/http"
)

// Register liga a rota GET /api/disks/forecast. wrap aplica autenticação e CSRF.
func (s *Store) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/disks/forecast", wrap(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"disks": s.Forecasts()})
	}))
}
