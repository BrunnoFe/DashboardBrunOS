package integrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// AdGuard de mentira que aceita as Ações. old = versão sem /control/protection.
type adgFake struct {
	mu     sync.Mutex
	old    bool
	bodies map[string][]map[string]any
	on     bool
	left   float64
}

func (f *adgFake) server(t *testing.T) *httptest.Server {
	f.bodies = map[string][]map[string]any{}
	f.on = true
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "senha" {
			w.WriteHeader(401)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && (r.URL.Path == "/control/protection" && !f.old || r.URL.Path == "/control/dns_config"):
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			f.bodies[r.URL.Path] = append(f.bodies[r.URL.Path], b)
			_, _ = w.Write([]byte("OK"))
		case r.URL.Path == "/control/stats":
			_, _ = w.Write([]byte(`{"num_dns_queries":10}`))
		case r.URL.Path == "/control/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"protection_enabled": f.on, "protection_disabled_duration": f.left})
		default:
			w.WriteHeader(404)
		}
	}))
}

func newActionHub(t *testing.T, url string) (*Hub, string) {
	st := NewStore("", testBox(t))
	pass := "senha"
	it, err := st.Add(Input{Kind: "adguard", URL: url, User: "admin", Secret: &pass})
	if err != nil {
		t.Fatal(err)
	}
	return NewHub(st, NewSeriesStore(""), nil), it.ID
}

func TestAdGuardActions(t *testing.T) {
	f := &adgFake{}
	srv := f.server(t)
	defer srv.Close()
	h, id := newActionHub(t, srv.URL)
	ctx := context.Background()

	if err := h.DoAction(ctx, id, ActionReq{Action: "pause", Minutes: 30}); err != nil {
		t.Fatal(err)
	}
	if err := h.DoAction(ctx, id, ActionReq{Action: "pause"}); err != nil {
		t.Fatal(err)
	}
	if err := h.DoAction(ctx, id, ActionReq{Action: "resume"}); err != nil {
		t.Fatal(err)
	}
	got := f.bodies["/control/protection"]
	if len(got) != 3 {
		t.Fatalf("pedidos = %+v", got)
	}
	if got[0]["enabled"] != false || got[0]["duration"] != float64(1_800_000) {
		t.Errorf("pausar 30 min = %+v", got[0])
	}
	if _, ok := got[1]["duration"]; ok || got[1]["enabled"] != false {
		t.Errorf("pausar até ligar = %+v", got[1])
	}
	if got[2]["enabled"] != true {
		t.Errorf("ligar = %+v", got[2])
	}

	// pedidos inválidos não chegam ao AdGuard
	for _, req := range []ActionReq{{Action: "apagar"}, {Action: "pause", Minutes: -1}, {Action: "pause", Minutes: 99999}} {
		if err := h.DoAction(ctx, id, req); err == nil {
			t.Errorf("%+v deveria ser recusado", req)
		}
	}
	if err := h.DoAction(ctx, "nao-existe", ActionReq{Action: "pause"}); err == nil {
		t.Error("integração que não existe deveria dar erro")
	}
	if len(f.bodies["/control/protection"]) != 3 {
		t.Error("pedido inválido chegou ao AdGuard")
	}
}

func TestAdGuardActionOldVersion(t *testing.T) {
	f := &adgFake{old: true}
	srv := f.server(t)
	defer srv.Close()
	h, id := newActionHub(t, srv.URL)
	ctx := context.Background()
	err := h.DoAction(ctx, id, ActionReq{Action: "pause", Minutes: 5})
	if err == nil || !strings.Contains(err.Error(), "antigo") {
		t.Fatalf("pausa com tempo num AdGuard antigo: %v", err)
	}
	if err := h.DoAction(ctx, id, ActionReq{Action: "pause"}); err != nil {
		t.Fatal(err)
	}
	if b := f.bodies["/control/dns_config"]; len(b) != 1 || b[0]["protection_enabled"] != false {
		t.Fatalf("caminho antigo = %+v", b)
	}
}

func TestAdGuardPauseLeft(t *testing.T) {
	f := &adgFake{}
	srv := f.server(t)
	defer srv.Close()
	f.on, f.left = false, 125_400
	vals, err := adguard{}.Fetch(context.Background(), srv.Client(), Config{URL: srv.URL, User: "admin", Secret: "senha"})
	if err != nil {
		t.Fatal(err)
	}
	if *vals["protection"].Text != "Pausada" || *vals["pause_left"].Num != 125 || *vals["protection_on"].Num != 0 || *vals["_guard"].Num != 1 {
		t.Fatalf("pausada: %v %v", *vals["protection"].Text, vals["pause_left"])
	}
	f.left = 0
	vals, _ = adguard{}.Fetch(context.Background(), srv.Client(), Config{URL: srv.URL, User: "admin", Secret: "senha"})
	if *vals["protection"].Text != "Desligada" || vals["pause_left"].Num != nil || *vals["_guard"].Num != 0 {
		t.Fatalf("desligada sem prazo: %v %v", *vals["protection"].Text, vals["pause_left"])
	}
}

func TestPlexQuery(t *testing.T) {
	for in, want := range map[string]string{
		"X2.2003.REMASTERED.PROPER.1080p.BluRay.mkv":    "X2",
		"The.Chronicles.of.Narnia.2005.1080p.mkv":       "The Chronicles of Narnia",
		"2001.A.Space.Odyssey.1968.2160p.mkv":           "2001 A Space Odyssey",
		"Minha Série S02E05 Episódio.mkv":               "Minha Série",
		"X-Men.The.Last.Stand.2006.REMASTERED.x264.mkv": "X Men The Last Stand",
		"filme_caseiro.mp4":                             "filme caseiro",
		"[Grupo] Anime (2019) [1080p].mkv":              "Grupo Anime",
	} {
		if got := plexQuery(in); got != want {
			t.Errorf("plexQuery(%q) = %q, esperado %q", in, got, want)
		}
	}
}

func TestPlexFind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "tok" {
			w.WriteHeader(401)
			return
		}
		media := func(file string) []map[string]any {
			return []map[string]any{{"Part": []map[string]any{{"file": file}}}}
		}
		var md []map[string]any
		switch r.URL.Path {
		case "/search":
			switch strings.ToLower(r.URL.Query().Get("query")) { // o Plex não liga para maiúsculas
			case "x2":
				md = []map[string]any{
					{"ratingKey": "10", "type": "movie", "Media": media("/data/filmes/X2.2003.Outro.mkv")},
					{"ratingKey": "11", "type": "movie", "Media": media("/data/filmes/X2.2003.REMASTERED.mkv")},
				}
			case "minha serie":
				md = []map[string]any{{"ratingKey": "50", "type": "show"}}
			}
		case "/library/metadata/50/allLeaves":
			md = []map[string]any{{"ratingKey": "51", "type": "episode", "Media": media(`D:\series\Minha.Serie.S01E02.mkv`)}}
		case "/identity":
			_, _ = w.Write([]byte(`{"MediaContainer":{"machineIdentifier":"abc123"}}`))
			return
		default:
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"Metadata": md}})
	}))
	defer srv.Close()
	cfg := Config{URL: srv.URL + "/", Secret: "tok"}
	ctx := context.Background()

	u, err := plexFind(ctx, srv.Client(), cfg, "x2.2003.remastered.mkv")
	if err != nil || u != srv.URL+"/web/index.html#!/server/abc123/details?key=%2Flibrary%2Fmetadata%2F11" {
		t.Fatalf("filme: %q %v", u, err)
	}
	u, err = plexFind(ctx, srv.Client(), cfg, "Minha.Serie.S01E02.mkv")
	if err != nil || !strings.HasSuffix(u, "%2Flibrary%2Fmetadata%2F51") {
		t.Fatalf("episódio: %q %v", u, err)
	}
	// nome parecido não vale
	if u, _ := plexFind(ctx, srv.Client(), cfg, "X2.2003.Diferente.mkv"); u != "" {
		t.Fatalf("achou um arquivo diferente: %q", u)
	}
}
