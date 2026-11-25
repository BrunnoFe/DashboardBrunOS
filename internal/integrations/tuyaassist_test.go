package integrations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// a mesma conta do tinytuya (Cloud._tuyaplatform): apiKey + token + t + "GET\n" + sha256("") + "\n" +
// (cabeçalhos assinados: nenhum) + "\n" + caminho, HMAC-SHA256 com o segredo, hexadecimal maiúsculo
func TestTuyaSignLikeTinytuya(t *testing.T) {
	payload := "idX" + "tokY" + "1700000000000" + "GET\n" + "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" + "\n" + "\n" + "/v1.0/token?grant_type=1"
	m := hmac.New(sha256.New, []byte("segredoZ"))
	m.Write([]byte(payload))
	want := strings.ToUpper(hex.EncodeToString(m.Sum(nil)))
	if got := tuyaSign("idX", "tokY", "segredoZ", "1700000000000", "GET", "/v1.0/token?grant_type=1", nil); got != want {
		t.Fatalf("assinatura: %s, queria %s", got, want)
	}
}

const (
	tAccess = "abcd1234efgh5678"
	tSecret = "segredo0123456789abcdef"
	tKey    = "0123456789abcdef" // chave local de mentira (16)
)

// nuvem de mentira: confere a assinatura e responde o token e a lista
func fakeCloud(t *testing.T, devices string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("access_token")
		want := tuyaSign(tAccess, tok, tSecret, r.Header.Get("t"), r.Method, r.URL.RequestURI(), nil)
		if r.Header.Get("client_id") != tAccess || r.Header.Get("sign") != want {
			_, _ = w.Write([]byte(`{"success":false,"code":1004,"msg":"sign invalid"}`))
			return
		}
		switch r.URL.Path {
		case "/v1.0/token":
			_, _ = w.Write([]byte(`{"success":true,"result":{"access_token":"tok123","uid":"u1","expire_time":7200}}`))
		case "/v1.0/iot-01/associated-users/devices":
			if tok != "tok123" {
				_, _ = w.Write([]byte(`{"success":false,"code":1010,"msg":"token invalid"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"result":{"devices":` + devices + `,"has_more":false}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := tuyaCloudBase
	tuyaCloudBase = func(region string) string {
		if region == "" {
			return ""
		}
		return srv.URL
	}
	t.Cleanup(func() { tuyaCloudBase = old })
	return srv
}

func TestTuyaAssistCloudAndScan(t *testing.T) {
	srv := fakeCloud(t, `[{"id":"bf123456","name":"Servidor","local_key":"`+tKey+`","product_name":"Smart Plug","online":true},
		{"id":"bf999999","name":"Abajur","local_key":"fedcba9876543210","online":false},
		{"id":"bfsemkey","name":"Lâmpada","local_key":""}]`)
	st := testStore(t)
	h := NewHub(st, nil, nil)
	h.Client = srv.Client() // o cliente de verdade não deixa falar com 127.0.0.1 (a nuvem de mentira)
	dir := t.TempDir()
	a := NewTuyaAssist(h, filepath.Join(dir, "tuya-cloud.json"))
	a.LAN = func() []string { return []string{"192.168.1.50", "", "não é ip"} }

	// credencial errada: a mensagem explica
	if err := a.SaveCloud(context.Background(), "us", tAccess, "outrosegredo123456"); err == nil || !strings.Contains(err.Error(), "Access Secret está errado") {
		t.Fatalf("segredo errado: %v", err)
	}
	if a.Info().Configured {
		t.Fatal("credencial errada não pode ficar guardada")
	}
	if err := a.SaveCloud(context.Background(), "", tAccess, tSecret); err == nil {
		t.Fatal("região vazia deveria falhar")
	}
	if err := a.SaveCloud(context.Background(), "us", "x;rm", tSecret); err == nil {
		t.Fatal("Access ID com caracteres estranhos deveria falhar")
	}
	if err := a.SaveCloud(context.Background(), "us", tAccess, tSecret); err != nil {
		t.Fatal(err)
	}
	info := a.Info()
	if !info.Configured || info.AccessID != "abcd••••5678" || info.Region != "us" {
		t.Fatalf("info: %+v", info)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "tuya-cloud.json"))
	if strings.Contains(string(raw), tSecret) {
		t.Fatal("o Access Secret ficou em texto no arquivo")
	}

	// já existe um cadastro da tomada "Servidor" num IP antigo
	old := tKey
	prev, err := st.Add(Input{Kind: "tuya", Name: "Tomada do servidor", Secret: &old, Options: map[string]string{"host": "192.168.1.40", "devid": "bf123456", "version": "3.5"}})
	if err != nil {
		t.Fatal(err)
	}

	// rede de mentira: só o .60 atende, e só a chave certa na versão 3.4 responde
	oldOpen, oldProbe := tuyaPortOpen, tuyaProbe
	t.Cleanup(func() { tuyaPortOpen, tuyaProbe = oldOpen, oldProbe })
	scanned := map[string]bool{}
	var smu sync.Mutex
	tuyaPortOpen = func(_ context.Context, ip string) bool {
		smu.Lock()
		scanned[strings.Join(strings.Split(ip, ".")[:3], ".")] = true
		smu.Unlock()
		return ip == "192.168.1.60"
	}
	tuyaProbe = func(_ context.Context, ip, dev, key, ver string) (map[string]any, error) {
		if ip == "192.168.1.60" && dev == "bf123456" && key == tKey && ver == "3.4" {
			return map[string]any{"19": 169.0, "1": true}, nil
		}
		return nil, errors.New("não respondeu")
	}
	list, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scanned) != 1 || !scanned["192.168.1"] {
		t.Fatalf("faixas procuradas: %v (a do servidor e a da tomada cadastrada são a mesma)", scanned)
	}
	byID := map[string]TuyaFound{}
	for _, f := range list {
		byID[f.ID] = f
	}
	s := byID["bf123456"]
	if s.IP != "192.168.1.60" || s.Version != "3.4" || s.Power == nil || *s.Power != 16.9 || s.Known != prev.ID || s.Same {
		t.Fatalf("tomada achada: %+v", s)
	}
	if list[0].ID != "bf123456" {
		t.Fatalf("as achadas vêm primeiro: %+v", list)
	}
	if !byID["bf999999"].NotFound || !byID["bfsemkey"].NoKey {
		t.Fatalf("não achada / sem chave: %+v", byID)
	}
	b, _ := json.Marshal(list)
	if strings.Contains(string(b), tKey) || strings.Contains(string(b), "local") {
		t.Fatalf("a chave local não pode ir para o navegador: %s", b)
	}

	// Atualizar: o mesmo cadastro, com o IP e a versão novos
	it, err := a.Apply("bf123456", "update", "")
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != prev.ID || it.Name != "Tomada do servidor" || it.Options["host"] != "192.168.1.60" || it.Options["version"] != "3.4" {
		t.Fatalf("atualizar: %+v", it)
	}
	if cfg, _ := st.Config(it); cfg.Secret != tKey {
		t.Fatal("a chave local não foi gravada")
	}
	if len(st.List()) != 1 {
		t.Fatal("atualizar não pode criar outro cadastro")
	}
	// agora a busca diz "tudo certo"
	list, _ = a.Scan(context.Background())
	if !list[0].Same {
		t.Fatalf("depois de atualizar deveria estar igual: %+v", list[0])
	}
	// Adicionar como nova (o teste do Bruno com "Tomada (teste)")
	it2, err := a.Apply("bf123456", "add", "Tomada (teste)")
	if err != nil || it2.ID == prev.ID || it2.Name != "Tomada (teste)" || !it2.Alerts["down"].On {
		t.Fatalf("adicionar: %+v %v", it2, err)
	}
	// tomada que não foi achada na rede não pode ser gravada
	if _, err := a.Apply("bf999999", "add", ""); err == nil {
		t.Fatal("tomada não achada não pode ser gravada")
	}
	if _, err := a.Apply("bf123456", "trocar", ""); err == nil {
		t.Fatal("modo desconhecido deveria falhar")
	}
	// esquecer a credencial apaga também a busca
	if err := a.ForgetCloud(); err != nil || a.Info().Configured {
		t.Fatal("esquecer a credencial")
	}
	if _, err := a.Apply("bf123456", "update", ""); err == nil {
		t.Fatal("sem busca guardada não pode gravar")
	}
	if _, err := a.Scan(context.Background()); err == nil {
		t.Fatal("sem credencial a busca deveria pedir o Access ID")
	}
}

func TestTuyaCloudErrors(t *testing.T) {
	for code, want := range map[int]string{1004: "Access Secret", 28841002: "região", 1106: "Link App Account", 28841105: "IoT Core", 999: "código 999"} {
		msg := map[int]string{28841002: "No permission. The data center is suspended", 1106: "permission deny"}[code]
		if err := tuyaCloudErr(code, msg); !strings.Contains(err.Error(), want) {
			t.Errorf("código %d: %v", code, err)
		}
	}
}

func TestLanPrefixes(t *testing.T) {
	got := lanPrefixes([]string{"192.168.100.56", "192.168.100.52", "10.0.0.5", "8.8.8.8", "fe80::1", "", "172.18.0.1"})
	if strings.Join(got, ",") != "192.168.100.,10.0.0.,172.18.0." {
		t.Fatalf("faixas: %v", got)
	}
}
