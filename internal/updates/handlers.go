package updates

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Register liga as rotas das atualizações:
//
//	GET  /api/updates          o que há para atualizar, a frequência e a atualização em andamento
//	POST /api/updates/check    conferir agora (?part=apps ou ?part=linux para só uma parte)
//	PUT  /api/updates/config   {"every": "6h"|"12h"|"24h"|"168h"|"manual"}
//	POST /api/updates/run      {"kind": "linux"} ou {"kind": "app", "urn": "immich:migrated", "backup": true}
//	GET  /api/updates/backup-size?urn=immich:migrated   {"size": bytes, "free": bytes}
//	POST /api/updates/script   confere (e troca sozinho, se der) o script do servidor
func (c *Checker) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/updates", wrap(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, c.Snapshot())
	}))
	// ?part=apps (atualiza as lojas do Runtipi e confere os apps), ?part=linux, ou sem nada (tudo)
	mux.HandleFunc("POST /api/updates/check", wrap(func(w http.ResponseWriter, r *http.Request) {
		if err := c.CheckPart(r.Context(), r.URL.Query().Get("part")); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, c.Snapshot())
	}))
	mux.HandleFunc("PUT /api/updates/config", wrap(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Every string `json:"every"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
			return
		}
		if err := c.SetEvery(body.Every); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, c.Snapshot())
	}))
	mux.HandleFunc("POST /api/updates/run", wrap(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Kind   string `json:"kind"`
			URN    string `json:"urn"`
			Backup bool   `json:"backup"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
			return
		}
		if err := c.Start(r.Context(), body.Kind, body.URN, body.Backup); err != nil {
			code := 502
			switch {
			case errors.Is(err, ErrBusy):
				code = 409
			case errors.Is(err, ErrKind), errors.Is(err, ErrNoUpdate):
				code = 400
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 202, c.Snapshot())
	}))
	// tamanho do backup que o Runtipi faria do app (para mostrar na confirmação)
	mux.HandleFunc("GET /api/updates/backup-size", wrap(func(w http.ResponseWriter, r *http.Request) {
		size, free, err := c.BackupSize(r.Context(), r.URL.Query().Get("urn"))
		if err != nil {
			code := 502
			if errors.Is(err, ErrNoUpdate) {
				code = 400
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]int64{"size": size, "free": free})
	}))
	// confere a versão do script do servidor agora (e troca sozinho se der)
	mux.HandleFunc("POST /api/updates/script", wrap(func(w http.ResponseWriter, r *http.Request) {
		c.CheckScript(r.Context())
		writeJSON(w, 200, c.Snapshot())
	}))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
