package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"brunos/internal/events"
)

// JPEG mínimo (o DetectContentType reconhece pelo começo)
var fakeJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, make([]byte, 64)...)

const asset1 = "11111111-2222-3333-4444-555555555555"

func TestImmich(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return now }
	defer func() { nowFunc = time.Now }()
	var randoms, thumbs int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" {
			w.WriteHeader(401)
			return
		}
		a := map[string]any{"id": asset1, "type": "IMAGE", "localDateTime": "2019-11-08T10:00:00.000Z", "exifInfo": map[string]any{"city": "Ouro Preto", "country": "Brasil"}}
		switch r.URL.Path {
		case "/api/assets/statistics":
			_, _ = w.Write([]byte(`{"images":1200,"videos":34,"total":1234}`))
		case "/api/server/statistics":
			w.WriteHeader(403) // chave sem administrador
		case "/api/server/about":
			_, _ = w.Write([]byte(`{"version":"v2.1.0"}`))
		case "/api/memories":
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"type": "on_this_day", "data": map[string]any{"year": 2019}, "assets": []any{a}}})
		case "/api/search/random":
			atomic.AddInt32(&randoms, 1)
			_ = json.NewEncoder(w).Encode([]any{a, map[string]any{"id": "x", "type": "VIDEO"}})
		case "/api/albums":
			_, _ = w.Write([]byte(`[{"id":"al1","albumName":"Viagens"}]`))
		case "/api/assets/" + asset1 + "/thumbnail":
			if r.URL.Query().Get("size") != "preview" {
				w.WriteHeader(400)
				return
			}
			atomic.AddInt32(&thumbs, 1)
			_, _ = w.Write(fakeJPEG)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	cfg := Config{URL: srv.URL, Secret: "k", Options: map[string]string{"album": "viagens"}}
	vals, err := immich{}.Fetch(context.Background(), srv.Client(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if *vals["photos"].Num != 1200 || *vals["version"].Text != "v2.1.0" {
		t.Fatalf("totais: %+v", vals)
	}
	if _, ok := vals["usage"]; ok {
		t.Fatal("sem administrador não deveria ter espaço usado")
	}
	today := vals["photo_today"].List
	if len(today) != 1 || today[0].Img != asset1 || today[0].Label != "8 de nov. de 2019" || today[0].Text != "há 7 anos · Ouro Preto, Brasil" {
		t.Fatalf("neste dia: %+v", today)
	}
	if len(vals["photo_album"].List) != 1 || len(vals["photo_random"].List) != 1 {
		t.Fatal("álbum/aleatória (vídeo deveria ficar de fora)")
	}
	// mesmo dia: não pede fotos de novo
	before := randoms
	cfg.Prev = vals
	if _, err := (immich{}).Fetch(context.Background(), srv.Client(), cfg); err != nil {
		t.Fatal(err)
	}
	if randoms != before {
		t.Fatal("listas de fotos deveriam valer o dia todo")
	}
	// imagem: só id válido, tamanho prévia, e cache no disco
	st := NewStore("", testBox(t))
	key := "k"
	it, _ := st.Add(Input{Kind: "immich", URL: srv.URL, Secret: &key})
	h := NewHub(st, NewSeriesStore(""), nil)
	h.ImageDir = t.TempDir()
	mux := http.NewServeMux()
	h.Register(mux, func(f http.HandlerFunc) http.HandlerFunc { return f }, func() ([]AppRef, string) { return nil, "" })
	get := func(ref string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/integrations/"+it.ID+"/image?ref="+ref, nil))
		return rec
	}
	if rec := get(asset1); rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("imagem: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get(asset1); rec.Code != 200 || thumbs != 1 {
		t.Fatalf("segunda vez deveria vir do cache (pedidos ao Immich: %d)", thumbs)
	}
	for _, bad := range []string{"../../etc/passwd", "x", "11111111-2222-3333-4444-55555555555Z"} {
		if rec := get(bad); rec.Code != 400 {
			t.Fatalf("ref %q deveria ser recusada, veio %d", bad, rec.Code)
		}
	}
}

func TestPlex(t *testing.T) {
	var transcodeURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "tok" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/status/sessions":
			_, _ = w.Write([]byte(`{"MediaContainer":{"size":2,"Metadata":[
				{"type":"episode","title":"O Começo","grandparentTitle":"Minha Série","parentIndex":2,"index":5,"grandparentThumb":"/library/metadata/10/thumb/170","duration":1000,"viewOffset":250,"User":{"title":"Ana"},"Player":{"title":"TV da sala","state":"playing"},"TranscodeSession":{"key":"x"}},
				{"type":"movie","title":"Um Filme","thumb":"/library/metadata/20/thumb/171","duration":0,"User":{"title":"Bruno"},"Player":{"title":"Celular","state":"paused"}}]}}`))
		case "/library/sections":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","type":"movie"},{"key":"2","type":"show"},{"key":"3","type":"photo"}]}}`))
		case "/library/sections/1/all":
			_, _ = w.Write([]byte(`{"MediaContainer":{"totalSize":412,"size":0}}`))
		case "/library/sections/2/all":
			_, _ = w.Write([]byte(`{"MediaContainer":{"totalSize":37}}`))
		case "/identity":
			_, _ = w.Write([]byte(`{"MediaContainer":{"version":"1.41.2.9200-abcdef"}}`))
		case "/photo/:/transcode":
			transcodeURL = r.URL.Query().Get("url")
			_, _ = w.Write(fakeJPEG)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	cfg := Config{URL: srv.URL, Secret: "tok"}
	vals, err := plex{}.Fetch(context.Background(), srv.Client(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if *vals["sessions"].Num != 2 || *vals["transcodes"].Num != 1 || *vals["movies"].Num != 412 || *vals["shows"].Num != 37 || *vals["version"].Text != "1.41.2.9200" {
		t.Fatalf("valores: %v %v %v %v", *vals["sessions"].Num, *vals["transcodes"].Num, *vals["movies"].Num, *vals["version"].Text)
	}
	m := vals["now_media"].List
	if m[0].Label != "Minha Série · T2E5 · O Começo" || *m[0].Num != 25 || m[0].Img != "/library/metadata/10/thumb/170" || m[0].Text != "Ana · TV da sala" {
		t.Fatalf("episódio: %+v", m[0])
	}
	if m[1].Num != nil || !strings.Contains(m[1].Text, "pausado") {
		t.Fatalf("filme pausado: %+v", m[1])
	}
	rc, err := plex{}.Image(context.Background(), srv.Client(), cfg, "/library/metadata/10/thumb/170")
	if err != nil || transcodeURL != "/library/metadata/10/thumb/170" {
		t.Fatalf("capa: %v %q", err, transcodeURL)
	}
	rc.Close()
	for _, bad := range []string{"/library/../etc", "http://evil/x", "/library/metadata/1/thumb/1?x=1"} {
		if _, err := (plex{}).Image(context.Background(), srv.Client(), cfg, bad); err == nil {
			t.Fatalf("capa %q deveria ser recusada", bad)
		}
	}
}

func TestWatchYourLANAndNewDeviceAlert(t *testing.T) {
	var hosts atomic.Value
	hosts.Store(`[{"ID":1,"Name":"Notebook","IP":"192.168.1.10","Mac":"aa:aa","Hw":"Dell","Known":1,"Now":1},
		{"ID":2,"Name":"","DNS":"galaxy.lan.","IP":"192.168.1.31","Mac":"BB:BB","Hw":"Samsung","Known":0,"Now":1}]`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/all" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(hosts.Load().(string)))
	}))
	defer srv.Close()
	vals, err := watchyourlan{}.Fetch(context.Background(), srv.Client(), Config{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	u := vals["unknown_list"].List
	if *vals["online"].Num != 2 || *vals["unknown"].Num != 1 || len(u) != 1 || u[0].Label != "galaxy.lan" || u[0].ID != "bb:bb" {
		t.Fatalf("valores: %+v", u)
	}
	// aviso: liga, aparece o desconhecido → 1 aviso; de novo → nada; marcado como conhecido → fecha
	st := NewStore("", testBox(t))
	it, _ := st.Add(Input{Kind: "watchyourlan", URL: srv.URL, Alerts: map[string]AlertSetting{"new_device": {On: true}}})
	ev := events.New("")
	h := NewHub(st, NewSeriesStore(""), ev)
	run := func() {
		got, _ := st.Get(it.ID)
		h.fetch(context.Background(), got)
	}
	run()
	run()
	list, _ := ev.List()
	if len(list) != 1 || !strings.Contains(list[0].Title, "Aparelho novo na rede: galaxy.lan") || !strings.Contains(list[0].Body, "marque como conhecido") {
		t.Fatalf("avisos: %+v", list)
	}
	hosts.Store(`[{"ID":2,"Name":"Celular","Mac":"BB:BB","Known":1,"Now":1}]`)
	run()
	if ev.IsActive(alertKey(it.ID, "new_device") + ":bb:bb") {
		t.Fatal("marcar como conhecido deveria fechar o aviso")
	}
}

func TestTailscale(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return now }
	defer func() { nowFunc = time.Now; HostQuery = nil }()
	HostQuery = func(ctx context.Context, cmd string) ([]byte, error) {
		if cmd != "tailscale" {
			return nil, errors.New("pedido errado")
		}
		return []byte(`{"BackendState":"Running","Self":{"HostName":"servidor","TailscaleIPs":["100.64.0.1","fd7a::1"]},
			"Peer":{"nodekey:a":{"HostName":"iPhone","DNSName":"iphone-bruno.tail1.ts.net.","OS":"iOS","TailscaleIPs":["100.64.0.2"],"Online":true,"PublicKey":"nodekey:a"},
			"nodekey:b":{"HostName":"pc","DNSName":"","OS":"windows","TailscaleIPs":["100.64.0.3"],"Online":false,"LastSeen":"2026-11-08T09:00:00Z","ExitNode":true,"PublicKey":"nodekey:b"}},
			"Health":[]}`), nil
	}
	vals, err := tailscale{}.Fetch(context.Background(), nil, Config{})
	if err != nil {
		t.Fatal(err)
	}
	p := vals["peers"].List
	if *vals["state"].Text != "Conectado" || *vals["online"].Num != 1 || *vals["self_ip"].Text != "100.64.0.1" || *vals["exit_node"].Text != "pc" {
		t.Fatalf("valores: %+v", vals)
	}
	if p[0].Label != "iphone-bruno" || !strings.HasPrefix(p[0].Text, "online") || !strings.HasPrefix(p[1].Text, "há 3 h") {
		t.Fatalf("aparelhos: %+v", p)
	}
	// sem endereço: a integração aceita URL vazia
	st := NewStore("", testBox(t))
	it, err := st.Add(Input{Kind: "tailscale"})
	if err != nil || it.URL != "" || it.Interval != 60 {
		t.Fatalf("integração sem endereço: %+v %v", it, err)
	}
	HostQuery = func(context.Context, string) ([]byte, error) {
		return nil, errors.New("o script do servidor é antigo")
	}
	if _, err := (tailscale{}).Fetch(context.Background(), nil, Config{}); err == nil || !strings.Contains(err.Error(), "antigo") {
		t.Fatalf("script antigo: %v", err)
	}
}

func TestImageCachePrune(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < imageCached+5; i++ {
		p := filepath.Join(dir, strings.Repeat("a", 3)+string(rune('A'+i%26))+string(rune('a'+i/26)))
		_ = os.WriteFile(p, fakeJPEG, 0o600)
		_ = os.Chtimes(p, time.Now().Add(time.Duration(i)*time.Second), time.Now().Add(time.Duration(i)*time.Second))
	}
	pruneImages(dir)
	if e, _ := os.ReadDir(dir); len(e) != imageCached {
		t.Fatalf("cache com %d arquivos", len(e))
	}
}
