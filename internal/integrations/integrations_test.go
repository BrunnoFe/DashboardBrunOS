package integrations

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"brunos/internal/events"
	"brunos/internal/secret"
)

func testBox(t *testing.T) *secret.Box {
	b, err := secret.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fakeAdGuard(t *testing.T, hits *int32, protection *atomic.Bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "admin" || p != "senha" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/control/stats":
			atomic.AddInt32(hits, 1)
			_, _ = w.Write([]byte(`{"time_units":"hours","num_dns_queries":1000,"num_blocked_filtering":250,
				"num_replaced_safebrowsing":0,"num_replaced_parental":0,"avg_processing_time":0.0123,
				"top_queried_domains":[{"a.com":10},{"b.com":30}],"top_blocked_domains":[{"ads.com":5}],
				"top_clients":[{"192.168.0.2":40}],"dns_queries":[1,2,3],"blocked_filtering":[0,1,1]}`))
		case "/control/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"protection_enabled": protection.Load(), "version": "v0.107.0", "running": true})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestAdGuardFetch(t *testing.T) {
	var hits int32
	var prot atomic.Bool
	prot.Store(true)
	srv := fakeAdGuard(t, &hits, &prot)
	defer srv.Close()
	vals, err := adguard{}.Fetch(context.Background(), srv.Client(), Config{URL: srv.URL + "/", User: "admin", Secret: "senha"})
	if err != nil {
		t.Fatal(err)
	}
	if *vals["queries"].Num != 1000 || *vals["blocked_pct"].Num != 25 || *vals["avg_time"].Num != 12.3 {
		t.Fatalf("valores: %+v %+v %+v", *vals["queries"].Num, *vals["blocked_pct"].Num, *vals["avg_time"].Num)
	}
	if top := vals["top_domains"].List; len(top) != 2 || top[0].Label != "b.com" {
		t.Fatalf("top ordenado errado: %+v", top)
	}
	if s := vals["queries_series"].Series; s == nil || len(s.Points) != 3 || s.Step != 3600 {
		t.Fatalf("série: %+v", s)
	}
	if *vals["protection"].Text != "Ligada" {
		t.Fatal("proteção")
	}
	_, err = adguard{}.Fetch(context.Background(), srv.Client(), Config{URL: srv.URL, User: "admin", Secret: "errada"})
	if err == nil || !strings.Contains(err.Error(), "recusou") {
		t.Fatalf("senha errada deveria dar erro em pt-BR: %v", err)
	}
}

func TestSpeedtestFetch(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 30, 0, 0, time.UTC)
	nowFunc = func() time.Time { return now }
	defer func() { nowFunc = time.Now }()
	var lists int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("Accept") != "application/json" {
			w.WriteHeader(401)
			return
		}
		res := func(id int, at time.Time, bits float64) map[string]any {
			return map[string]any{"id": id, "ping": 12.34, "download": bits / 8, "upload": 1e6, "download_bits": bits,
				"upload_bits": 8e6, "status": "completed", "created_at": at.Format(time.RFC3339),
				"data": map[string]any{"isp": "Provedor", "ping": map[string]any{"jitter": 1.5}, "server": map[string]any{"name": "SP", "location": "São Paulo"}}}
		}
		switch r.URL.Path {
		case "/api/v1/results/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": res(7, now.Add(-time.Hour), 300e6)})
		case "/api/v1/results":
			atomic.AddInt32(&lists, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{res(7, now.Add(-time.Hour), 300e6), res(6, now.Add(-3*time.Hour), 200e6)}})
		}
	}))
	defer srv.Close()
	cfg := Config{URL: srv.URL, Secret: "tok"}
	vals, err := speedtest{}.Fetch(context.Background(), srv.Client(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if *vals["download"].Num != 300 || *vals["upload"].Num != 8 || *vals["ping"].Num != 12.3 || *vals["server"].Text != "SP · São Paulo" {
		t.Fatalf("valores: %v %v %v %q", *vals["download"].Num, *vals["upload"].Num, *vals["ping"].Num, *vals["server"].Text)
	}
	s := vals["download_series"].Series
	if s == nil || len(s.Points) != 168 || s.Points[167-1] != 300 || s.Points[167-3] != 200 || !math.IsNaN(s.Points[167]) {
		t.Fatalf("série: %v", s.Points[160:])
	}
	// sem teste novo: não baixa a lista de novo
	cfg.Prev = vals
	if _, err := (speedtest{}).Fetch(context.Background(), srv.Client(), cfg); err != nil {
		t.Fatal(err)
	}
	if lists != 1 {
		t.Fatalf("lista baixada %d vezes", lists)
	}
	b, _ := json.Marshal(vals["download_series"])
	if !strings.Contains(string(b), "null") {
		t.Fatal("NaN deveria virar null no JSON")
	}
}

func TestStoreNeverLeaksSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "integrations.json")
	st := NewStore(path, testBox(t))
	tok := "meu-token-secreto"
	it, err := st.Add(Input{Kind: "speedtest", URL: "http://10.0.0.2:8765/", Secret: &tok})
	if err != nil {
		t.Fatal(err)
	}
	if it.URL != "http://10.0.0.2:8765" || it.Interval != 600 || it.Name != "Speedtest Tracker" {
		t.Fatalf("padrões: %+v", it)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), tok) {
		t.Fatal("token em texto puro no disco")
	}
	pub, _ := json.Marshal(it.Public())
	if strings.Contains(string(pub), "secret\"") || strings.Contains(string(pub), tok) || !strings.Contains(string(pub), `"hasSecret":true`) {
		t.Fatalf("público vazou: %s", pub)
	}
	// editar sem mandar segredo mantém o anterior
	it2, err := st.Update(it.ID, Input{Name: "Internet", URL: "http://10.0.0.2:8765", Interval: 900})
	if err != nil || it2.Secret != it.Secret || it2.Kind != "speedtest" {
		t.Fatalf("update: %+v %v", it2, err)
	}
	cfg, _ := st.Config(it2)
	if cfg.Secret != tok {
		t.Fatal("segredo não decifra")
	}
	for _, bad := range []Input{
		{Kind: "nada", URL: "http://x"},
		{Kind: "adguard", URL: "ftp://x"},
		{Kind: "adguard", URL: "http://u:p@x"},
		{Kind: "adguard", URL: "http://x", Interval: 5},
		{Kind: "speedtest", URL: "http://x"}, // token obrigatório
	} {
		if _, err := st.Add(bad); err == nil {
			t.Fatalf("deveria recusar %+v", bad)
		}
	}
	if err := st.Delete(it.ID); err != nil {
		t.Fatal(err)
	}
	if len(NewStore(path, testBox(t)).List()) != 0 {
		t.Fatal("apagar não persistiu")
	}
}

// fakeClock deixa o teste controlar o tempo do Hub.
type fakeClock struct{ t atomic.Int64 }

func (c *fakeClock) now() time.Time      { return time.Unix(c.t.Load(), 0) }
func (c *fakeClock) add(d time.Duration) { c.t.Add(int64(d / time.Second)) }
func waitHits(t *testing.T, hits *int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(hits) < want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(hits); got != want {
		t.Fatalf("buscas = %d, esperado %d", got, want)
	}
}

func usage(metric string, format string) map[string]any {
	return map[string]any{"widgets2": map[string]any{"items": map[string]any{
		"w1": map[string]any{"format": format, "slots": []any{map[string]any{"metric": metric}}},
	}}}
}

func TestHubScheduling(t *testing.T) {
	var hits int32
	var prot atomic.Bool
	prot.Store(true)
	srv := fakeAdGuard(t, &hits, &prot)
	defer srv.Close()
	st := NewStore("", testBox(t))
	pass := "senha"
	it, err := st.Add(Input{Kind: "adguard", URL: srv.URL, User: "admin", Secret: &pass, Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	clk := &fakeClock{}
	clk.t.Store(1_800_000_000)
	ev := events.New("")
	h := NewHub(st, NewSeriesStore(""), ev)
	h.Now = clk.now
	ctx := context.Background()

	// nenhum widget, ninguém olhando: zero buscas
	h.Tick(ctx)
	time.Sleep(30 * time.Millisecond)
	if hits != 0 {
		t.Fatal("buscou sem ninguém usar")
	}
	// widget usa, mas ninguém está com a tela aberta: ainda zero
	h.SetUsage(usage(it.ID+".queries", "grid"))
	h.Tick(ctx)
	time.Sleep(30 * time.Millisecond)
	if hits != 0 {
		t.Fatal("buscou com a tela fechada")
	}
	// alguém abriu: busca já e depois só no intervalo
	ch, unsub := h.Subscribe()
	h.Tick(ctx)
	waitHits(t, &hits, 1)
	select {
	case msg := <-ch:
		if !strings.Contains(string(msg), `"queries"`) {
			t.Fatalf("mensagem: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("aba não recebeu o valor")
	}
	clk.add(30 * time.Second)
	h.Tick(ctx)
	time.Sleep(30 * time.Millisecond)
	if hits != 1 {
		t.Fatal("buscou antes do intervalo")
	}
	clk.add(31 * time.Second)
	h.Tick(ctx)
	waitHits(t, &hits, 2)
	if len(h.Snapshot()) != 1 {
		t.Fatal("snapshot")
	}
	// fechou a aba: para
	unsub()
	clk.add(10 * time.Minute)
	h.Tick(ctx)
	time.Sleep(30 * time.Millisecond)
	if hits != 2 {
		t.Fatal("continuou buscando com a tela fechada")
	}
	// aviso ligado: segundo plano a cada 5 min, mesmo sem ninguém olhando
	_, err = st.Update(it.ID, Input{URL: srv.URL, User: "admin", Interval: 60, Alerts: map[string]AlertSetting{"protection_off": {On: true}}})
	if err != nil {
		t.Fatal(err)
	}
	h.Changed(it.ID, false)
	prot.Store(false)
	h.Tick(ctx)
	waitHits(t, &hits, 3)
	time.Sleep(30 * time.Millisecond)
	if list, _ := ev.List(); len(list) != 1 || !strings.Contains(list[0].Title, "proteção desligada") {
		t.Fatalf("aviso: %+v", list)
	}
	clk.add(2 * time.Minute)
	h.Tick(ctx)
	time.Sleep(30 * time.Millisecond)
	if hits != 3 {
		t.Fatal("segundo plano mais rápido que 5 min")
	}
	clk.add(3 * time.Minute)
	h.Tick(ctx)
	waitHits(t, &hits, 4)
	if list, _ := ev.List(); len(list) != 1 {
		t.Fatal("aviso repetido")
	}
	// apagar a integração fecha o aviso e para tudo
	_ = st.Delete(it.ID)
	h.Changed(it.ID, true)
	if ev.IsActive(alertKey(it.ID, "protection_off")) {
		t.Fatal("aviso continuou aberto")
	}
}

func TestBackgroundSeries(t *testing.T) {
	st := NewStore("", testBox(t))
	tok := "t"
	it, _ := st.Add(Input{Kind: "speedtest", URL: "http://127.0.0.1:1", Secret: &tok})
	h := NewHub(st, NewSeriesStore(""), nil)
	// jitter não tem histórico no app: num gráfico, precisa de coleta própria
	h.SetUsage(usage(it.ID+".jitter", "chart"))
	if !h.bg[it.ID]["jitter"] {
		t.Fatal("jitter em gráfico deveria ir para o segundo plano")
	}
	// download tem histórico no app: não
	h.SetUsage(usage(it.ID+".download", "chart"))
	if len(h.bg[it.ID]) != 0 || !h.live[it.ID] {
		t.Fatal("download usa o histórico do próprio app")
	}
}

func TestSeriesStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metric-history.json")
	s := NewSeriesStore(path)
	base := time.Unix(1_800_000_000, 0)
	cur := base
	s.Now = func() time.Time { return cur }
	for i := 0; i < 24; i++ { // 2 h, a cada 5 min
		s.Add("x.m", float64(i))
		cur = cur.Add(5 * time.Minute)
	}
	cur = cur.Add(-5 * time.Minute)
	d, ok := s.Series("x.m", "1h")
	if !ok || len(d.Points) != 12 || d.Points[11] != 23 {
		t.Fatalf("1h: %v", d.Points)
	}
	d, _ = s.Series("x.m", "7d")
	if len(d.Points) != 168 || math.IsNaN(d.Points[167]) {
		t.Fatalf("7d último: %v", d.Points[165:])
	}
	s.Save()
	s2 := NewSeriesStore(path)
	s2.Now = s.Now
	d2, ok := s2.Series("x.m", "24h")
	if !ok || d2.Points[287] != 23 {
		t.Fatalf("depois de recarregar: %v", d2.Points[280:])
	}
	s2.Forget("x.")
	if _, ok := s2.Series("x.m", "24h"); ok {
		t.Fatal("Forget")
	}
}
