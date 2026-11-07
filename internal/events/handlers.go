package events

import (
	"encoding/json"
	"io"
	"net/http"
)

// Register liga as rotas /api/events*. wrap aplica autenticação e CSRF.
func (s *Store) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/events", wrap(s.hList))
	mux.HandleFunc("POST /api/events/read", wrap(s.hRead))
	mux.HandleFunc("POST /api/events/clear", wrap(s.hClear))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Store) hList(w http.ResponseWriter, r *http.Request) {
	list, unread := s.List()
	writeJSON(w, 200, map[string]any{"events": list, "unread": unread})
}

func (s *Store) hRead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil || len(body.IDs) > maxEvents {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	s.MarkRead(body.IDs)
	_, unread := s.List()
	writeJSON(w, 200, map[string]int{"unread": unread})
}

func (s *Store) hClear(w http.ResponseWriter, r *http.Request) {
	s.Clear()
	writeJSON(w, 200, map[string]int{"unread": 0})
}
