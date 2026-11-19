package integrations

// Destinos proibidos e credencial presa ao endereço (auditoria de 19/11/2026).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"brunos/internal/secret"
)

// Os servidores falsos dos testes ficam em 127.0.0.1: libera o loopback em todos os testes do pacote,
// menos onde o teste desliga de propósito.
func TestMain(m *testing.M) {
	allowLoopback = true
	os.Exit(m.Run())
}

func TestBlockedDestinations(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.Write([]byte(`{}`)) }))
	defer srv.Close()
	allowLoopback = false
	defer func() { allowLoopback = true }()

	c := &http.Client{Transport: safeTransport()}
	for _, u := range []string{srv.URL, "http://169.254.169.254/latest/meta-data/", "http://[::1]:1/", "http://0.0.0.0:1/"} {
		var v map[string]any
		_, err := getJSON(context.Background(), c, u, nil, "", "", &v)
		if err == nil || !strings.Contains(err.Error(), "não é permitido") {
			t.Errorf("%s deveria ser recusado como endereço não permitido, veio %v", u, err)
		}
	}
	if called {
		t.Fatal("o pedido chegou ao servidor local mesmo bloqueado")
	}

	// o proxy do Docker (BlockedHosts) também
	old := BlockedHosts
	BlockedHosts = func() []string { return []string{"127.0.0.1"} }
	blocked.at = blocked.at.AddDate(-1, 0, 0) // força resolver de novo
	defer func() { BlockedHosts = old; blocked.at = blocked.at.AddDate(-1, 0, 0) }()
	allowLoopback = true
	var v map[string]any
	if _, err := getJSON(context.Background(), c, srv.URL, nil, "", "", &v); err == nil {
		t.Fatal("o endereço do proxy do Docker deveria ser recusado")
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	box, err := secret.Load(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	return NewStore(filepath.Join(dir, "integrations.json"), box)
}

func TestSecretBoundToOrigin(t *testing.T) {
	st := testStore(t)
	tok := "TOKEN-TESTE"
	prev, err := st.Validate(Input{Kind: "speedtest", URL: "http://192.168.1.50:8765", Secret: &tok}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prev.ID = "abc"
	// outro servidor sem digitar o token de novo: recusado
	for _, u := range []string{"http://192.168.1.99:8765", "http://192.168.1.50:9999", "https://192.168.1.50:8765"} {
		if _, err := st.Validate(Input{Kind: "speedtest", URL: u}, &prev); err == nil || !strings.Contains(err.Error(), "digite a senha/token de novo") {
			t.Errorf("%s: o token guardado não pode ir para outro endereço (veio %v)", u, err)
		}
	}
	// mesmo servidor (outro caminho, barra no fim, maiúsculas): mantém o token
	for _, u := range []string{"http://192.168.1.50:8765/", "HTTP://192.168.1.50:8765/sub"} {
		if it, err := st.Validate(Input{Kind: "speedtest", URL: u}, &prev); err != nil || it.Secret != prev.Secret {
			t.Errorf("%s: mesmo servidor deveria manter o token (veio %v)", u, err)
		}
	}
	// digitando o token de novo, pode trocar
	if _, err := st.Validate(Input{Kind: "speedtest", URL: "http://192.168.1.99:8765", Secret: &tok}, &prev); err != nil {
		t.Fatal(err)
	}
	// Tomada Tuya: o IP da tomada fica nas opções
	key := "0123456789abcdef"
	tp, err := st.Validate(Input{Kind: "tuya", Secret: &key, Options: map[string]string{"host": "192.168.1.60", "devid": "abc"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Validate(Input{Kind: "tuya", Options: map[string]string{"host": "192.168.1.61", "devid": "abc"}}, &tp); err == nil {
		t.Fatal("trocar o IP da tomada sem digitar a chave local deveria ser recusado")
	}
}

func TestURLWithoutQueryOrFragment(t *testing.T) {
	st := testStore(t)
	for _, u := range []string{"http://192.168.1.50:9091/v1.43/containers/x/stop#", "http://192.168.1.50:9091/?a=1", "http://192.168.1.50:9091/#x"} {
		if _, err := st.Validate(Input{Kind: "transmission", URL: u}, nil); err == nil {
			t.Errorf("%s deveria ser recusado", u)
		}
	}
}
