package updates

import (
	"encoding/json"
	"net/http"
)

// Register liga GET /api/updates e POST /api/updates/check ("Conferir agora").
func (c *Checker) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/updates", wrap(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, c.Snapshot())
	}))
	mux.HandleFunc("POST /api/updates/check", wrap(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Context())
		writeJSON(w, 200, c.Snapshot())
	}))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
