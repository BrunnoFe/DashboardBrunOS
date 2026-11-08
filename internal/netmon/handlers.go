package netmon

import (
	"encoding/json"
	"net/http"
)

// Register liga GET /api/internet. wrap aplica autenticação e CSRF.
func (m *Monitor) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/internet", wrap(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(m.Status())
	}))
}
