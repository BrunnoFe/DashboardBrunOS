package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"brunos/internal/store"
)

// AppRef é um app instalado no Runtipi (para sugerir integrações já preenchidas).
type AppRef struct {
	ID   string
	Name string
	Port int
}

// Suggestion é uma integração sugerida a partir de um app do Runtipi.
type Suggestion struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	URL  string `json:"url"`
	App  string `json:"app"`
}

// Suggest cruza os apps do Runtipi com os Tipos conhecidos. host = IP interno do servidor.
func Suggest(apps []AppRef, host string) []Suggestion {
	out := []Suggestion{}
	if host == "" {
		return out
	}
	for _, a := range apps {
		if a.Port <= 0 {
			continue
		}
		id := strings.ToLower(a.ID)
		for _, k := range kinds {
			info := k.Info()
			for _, rid := range info.Runtipi {
				if id == rid || strings.HasPrefix(id, rid+"-") {
					out = append(out, Suggestion{Kind: info.ID, Name: a.Name, URL: "http://" + host + ":" + strconv.Itoa(a.Port), App: a.ID})
				}
			}
		}
	}
	return out
}

// Register liga as rotas /api/integrations* e /api/metrics/series. wrap aplica autenticação e CSRF.
// apps devolve os apps do Runtipi e o IP interno do servidor.
func (h *Hub) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc, apps func() ([]AppRef, string)) {
	mux.HandleFunc("GET /api/integrations/kinds", wrap(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, Kinds())
	}))
	mux.HandleFunc("GET /api/integrations/suggest", wrap(func(w http.ResponseWriter, r *http.Request) {
		list, host := apps()
		writeJSON(w, 200, Suggest(list, host))
	}))
	mux.HandleFunc("GET /api/integrations", wrap(h.hList))
	mux.HandleFunc("POST /api/integrations", wrap(h.hAdd))
	mux.HandleFunc("PUT /api/integrations/{id}", wrap(h.hUpdate))
	mux.HandleFunc("DELETE /api/integrations/{id}", wrap(h.hDelete))
	mux.HandleFunc("POST /api/integrations/test", wrap(h.hTest))
	mux.HandleFunc("GET /api/integrations/{id}/image", wrap(h.hImage))
	mux.HandleFunc("GET /api/metrics/series", wrap(h.hSeries))
	mux.HandleFunc("GET /api/integrations/geocode", wrap(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		list, err := Geocode(ctx, h.Client, r.URL.Query().Get("q"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, list)
	}))
}

const (
	maxImage    = 8 << 20 // uma foto "prévia" do Immich tem uns 300 KB
	imageCached = 60      // quantas imagens ficam no cache
)

// hImage serve uma imagem de um app (foto do Immich, capa do Plex) sem expor o token: o BrunOS busca
// com a credencial guardada, confere que é imagem e guarda no cache (o widget troca de foto sem
// baixar de novo o que já mostrou).
func (h *Hub) hImage(w http.ResponseWriter, r *http.Request) {
	it, ok := h.Store.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, ErrNotFound)
		return
	}
	k, ok := KindByID(it.Kind).(ImageKind)
	ref := r.URL.Query().Get("ref")
	if !ok || ref == "" || len(ref) > 500 {
		writeJSON(w, 400, map[string]string{"error": "imagem inválida"})
		return
	}
	sum := sha256.Sum256([]byte(it.ID + "\x00" + ref))
	name := hex.EncodeToString(sum[:20])
	send := func(data []byte) {
		w.Header().Set("Content-Type", http.DetectContentType(data))
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(data)
	}
	if h.ImageDir != "" {
		if data, err := os.ReadFile(filepath.Join(h.ImageDir, name)); err == nil {
			send(data)
			return
		}
	}
	cfg, err := h.Store.Config(it)
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rc, err := k.Image(ctx, h.Client, cfg, ref)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxImage+1))
	if err != nil || len(data) > maxImage || !strings.HasPrefix(http.DetectContentType(data), "image/") {
		writeErr(w, ferr("o app não devolveu uma imagem válida"))
		return
	}
	if h.ImageDir != "" {
		if err := store.WriteFile(filepath.Join(h.ImageDir, name), data, 0o600); err == nil {
			pruneImages(h.ImageDir)
		}
	}
	send(data)
}

// pruneImages apaga as imagens mais antigas além do limite do cache.
func pruneImages(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= imageCached {
		return
	}
	type f struct {
		name string
		at   time.Time
	}
	files := make([]f, 0, len(entries))
	for _, e := range entries {
		if info, err := e.Info(); err == nil && e.Type().IsRegular() {
			files = append(files, f{e.Name(), info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.After(files[j].at) })
	for _, x := range files[min(len(files), imageCached):] {
		_ = os.Remove(filepath.Join(dir, x.name))
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var in *ErrInput
	var fe *FetchError
	switch {
	case errors.As(err, &in):
		writeJSON(w, 400, map[string]string{"error": in.Msg})
	case errors.As(err, &fe):
		writeJSON(w, 502, map[string]string{"error": fe.Msg})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, 404, map[string]string{"error": err.Error()})
	default:
		slog.Error("integrações", "erro", err)
		writeJSON(w, 500, map[string]string{"error": "não foi possível salvar"})
	}
}

func decodeInput(r *http.Request) (Input, error) {
	var in Input
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&in); err != nil {
		return in, &ErrInput{"requisição inválida"}
	}
	return in, nil
}

func (h *Hub) hList(w http.ResponseWriter, r *http.Request) {
	list := h.Store.List()
	pub := make([]Public, len(list))
	for i, it := range list {
		pub[i] = it.Public()
	}
	writeJSON(w, 200, map[string]any{"integrations": pub, "views": h.Snapshot()})
}

func (h *Hub) hAdd(w http.ResponseWriter, r *http.Request) {
	in, err := decodeInput(r)
	if err == nil {
		var it Integration
		if it, err = h.Store.Add(in); err == nil {
			h.Changed(it.ID, false)
			writeJSON(w, 200, it.Public())
			return
		}
	}
	writeErr(w, err)
}

func (h *Hub) hUpdate(w http.ResponseWriter, r *http.Request) {
	in, err := decodeInput(r)
	if err == nil {
		var it Integration
		if it, err = h.Store.Update(r.PathValue("id"), in); err == nil {
			h.Changed(it.ID, false)
			writeJSON(w, 200, it.Public())
			return
		}
	}
	writeErr(w, err)
}

func (h *Hub) hDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.Store.Delete(id); err != nil {
		writeErr(w, err)
		return
	}
	h.Changed(id, true)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// hTest busca uma vez sem salvar. Com "id", reaproveita a senha/token já guardado.
func (h *Hub) hTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Input
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&body); err != nil {
		writeErr(w, &ErrInput{"requisição inválida"})
		return
	}
	var prev *Integration
	if body.ID != "" {
		p, ok := h.Store.Get(body.ID)
		if !ok {
			writeErr(w, ErrNotFound)
			return
		}
		prev, body.Kind = &p, p.Kind
	}
	it, err := h.Store.Validate(body.Input, prev)
	if err != nil {
		writeErr(w, err)
		return
	}
	vals, err := h.Test(r.Context(), it)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "values": vals})
}

// hSeries devolve o histórico próprio (coleta em segundo plano) de uma métrica de integração.
func (h *Hub) hSeries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rng := q.Get("range")
	if rng != "1h" && rng != "7d" {
		rng = "24h"
	}
	d, _ := h.Series.Series(q.Get("id"), rng) // sem histórico ainda: série vazia
	writeJSON(w, 200, map[string]any{"range": rng, "series": d})
}
