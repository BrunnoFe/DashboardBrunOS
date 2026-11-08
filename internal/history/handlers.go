package history

import (
	"encoding/json"
	"net/http"
)

// Register liga GET /api/stats/history?range=1h|24h|7d. wrap aplica autenticação e CSRF.
func (s *Store) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/stats/history", wrap(func(w http.ResponseWriter, r *http.Request) {
		rng := r.URL.Query().Get("range")
		if rng != "1h" && rng != "7d" {
			rng = "24h"
		}
		step, pts := s.Series(rng)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"range": rng, "step": step, "points": pts})
	}))
}
