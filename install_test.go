package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"brunos/internal/events"
	"brunos/internal/runtipi"
)

// fakeHost faz o papel do script do servidor: responde e anota os pedidos.
type fakeHost struct {
	mu    sync.Mutex
	calls []string
	ins   map[string][]byte
	resp  map[string]string
	fail  map[string]error
}

func (f *fakeHost) run(_ context.Context, cmd string, in []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, cmd)
	verb := strings.Fields(cmd)[0]
	f.ins[verb] = in
	if err := f.fail[verb]; err != nil {
		return nil, err
	}
	return []byte(f.resp[verb]), nil
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

var testRuntipiDir string

func installServer(t *testing.T) (*server, *fakeHost) {
	dir := t.TempDir()
	testRuntipiDir = dir
	writeFile(t, filepath.Join(dir, "repos/migrated/apps/alist/config.json"), `{"name":"AList","port":5244,"exposable":true,"tipi_version":1,"available":true,
		"form_fields":[{"type":"text","label":"Usuário","env_variable":"ALIST_USER","required":true},
		{"type":"password","label":"Senha","env_variable":"ALIST_PASS","required":true,"min":8},
		{"type":"number","label":"Workers","env_variable":"WORKERS","min":1,"max":8},
		{"type":"boolean","label":"Modo leitura","env_variable":"RO"},
		{"type":"text","label":"Tema","env_variable":"THEME","options":[{"label":"Claro","value":"light"},{"label":"Escuro","value":"dark"}]},
		{"type":"random","label":"Segredo","env_variable":"SECRET"}]}`)
	writeFile(t, filepath.Join(dir, "repos/migrated/apps/plex/config.json"), `{"name":"Plex","port":32400,"tipi_version":1,"available":true}`)
	writeFile(t, filepath.Join(dir, "apps/migrated/plex/config.json"), `{"name":"Plex","id":"plex","port":32400,"tipi_version":1}`)
	f := &fakeHost{ins: map[string][]byte{}, resp: map[string]string{}, fail: map[string]error{}}
	s := &server{tipi: runtipi.New(dir), events: events.New(""), storeHost: f.run}
	return s, f
}

func call(t *testing.T, h http.HandlerFunc, method, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	w := httptest.NewRecorder()
	h(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestInstall(t *testing.T) {
	s, f := installServer(t)
	ok := map[string]any{"urn": "alist:migrated", "port": 8123, "values": map[string]any{"ALIST_USER": "admin", "ALIST_PASS": "senha-boa-123", "WORKERS": 4, "RO": true, "THEME": "dark", "SECRET": "não-pode"}}
	code, out := call(t, s.hStoreInstall, "POST", "/api/store/install", ok)
	if code != 200 {
		t.Fatalf("instalar: %d %v", code, out)
	}
	var body map[string]any
	if err := json.Unmarshal(f.ins["instalar-app"], &body); err != nil {
		t.Fatal(err)
	}
	if body["ALIST_USER"] != "admin" || body["ALIST_PASS"] != "senha-boa-123" || body["WORKERS"] != 4.0 || body["RO"] != true || body["THEME"] != "dark" ||
		body["port"] != 8123.0 || body["exposed"] != false || body["openPort"] != true {
		t.Fatalf("corpo: %v", body)
	}
	if _, has := body["SECRET"]; has {
		t.Fatal("pergunta random não pode ir do navegador ao Runtipi")
	}
	if f.calls[len(f.calls)-1] != "instalar-app alist:migrated" {
		t.Fatalf("pedido: %v", f.calls)
	}
	bad := []map[string]any{
		{"urn": "alist:migrated", "values": map[string]any{"ALIST_PASS": "senha-boa-123"}},                                                    // falta obrigatória
		{"urn": "alist:migrated", "values": map[string]any{"ALIST_USER": "a", "ALIST_PASS": "curta"}},                                         // min
		{"urn": "alist:migrated", "values": map[string]any{"ALIST_USER": "a", "ALIST_PASS": "senha-boa-123", "WORKERS": 99}},                  // max
		{"urn": "alist:migrated", "values": map[string]any{"ALIST_USER": "a", "ALIST_PASS": "senha-boa-123", "THEME": "roxo"}},                // opção
		{"urn": "alist:migrated", "values": map[string]any{"ALIST_USER": "a\nb", "ALIST_PASS": "senha-boa-123"}},                              // quebra de linha
		{"urn": "alist:migrated", "port": 80, "values": map[string]any{"ALIST_USER": "a", "ALIST_PASS": "senha-boa-123"}},                     // porta baixa
		{"urn": "alist:migrated", "exposed": true, "domain": "x", "values": map[string]any{"ALIST_USER": "a", "ALIST_PASS": "senha-boa-123"}}, // domínio
	}
	for i, b := range bad {
		if code, out := call(t, s.hStoreInstall, "POST", "/api/store/install", b); code != 400 {
			t.Errorf("caso %d deveria ser recusado: %d %v", i, code, out)
		}
	}
	if code, _ := call(t, s.hStoreInstall, "POST", "/api/store/install", map[string]any{"urn": "plex:migrated"}); code != 409 {
		t.Error("instalar app já instalado deveria dar 409")
	}
	// erro do Runtipi traduzido
	f.fail["instalar-app"] = errors.New(`{"statusCode":500,"message":"APP_ERROR_PORT_ALREADY_IN_USE","intlParams":{"port":8123}}`)
	if code, out := call(t, s.hStoreInstall, "POST", "/api/store/install", ok); code != 502 || !strings.Contains(out["error"].(string), "porta 8123 já é usada") {
		t.Fatalf("erro traduzido: %d %v", code, out)
	}
}

func TestReconfigGuardaSenhaESegredo(t *testing.T) {
	s, f := installServer(t)
	writeFile(t, filepath.Join(testRuntipiDir, "apps/migrated/alist/config.json"), `{"name":"AList","id":"alist","port":8123,"tipi_version":1}`)
	s.tipi.Invalidate()
	f.resp["info-app"] = `{"app":{"status":"running","port":8123,"config":{"ALIST_USER":"admin","ALIST_PASS":"antiga-123456","SECRET":"gerado"},"enableAuth":true}}`
	code, out := call(t, s.hStoreInstall, "POST", "/api/store/reconfig", map[string]any{"urn": "alist:migrated", "values": map[string]any{"ALIST_USER": "novo", "ALIST_PASS": ""}})
	if code != 200 {
		t.Fatalf("editar: %d %v", code, out)
	}
	var body map[string]any
	_ = json.Unmarshal(f.ins["reconfigurar-app"], &body)
	if body["ALIST_USER"] != "novo" || body["ALIST_PASS"] != "antiga-123456" || body["SECRET"] != "gerado" || body["enableAuth"] != true || body["port"] != 8123.0 {
		t.Fatalf("corpo da edição: %v", body)
	}
	// a tela nunca recebe senha nem segredo
	fields, _, _ := s.tipi.FormFields("alist:migrated")
	var t2 tipiApp
	_ = json.Unmarshal([]byte(f.resp["info-app"]), &t2)
	c := currentOf(t2, fields)
	if _, has := c.Values["ALIST_PASS"]; has || len(c.Secrets) != 1 || c.Secrets[0] != "ALIST_PASS" {
		t.Fatalf("respostas atuais: %+v", c)
	}
	if _, has := c.Values["SECRET"]; has {
		t.Fatal("segredo random foi para a tela")
	}
}

func TestInstallPrivilegiosEAjustes(t *testing.T) {
	s, f := installServer(t)
	writeFile(t, filepath.Join(testRuntipiDir, "repos/migrated/apps/alist/docker-compose.yml"),
		"services:\n  alist:\n    image: xhofe/alist:v3.30.0\n    privileged: true\n    environment:\n      TZ: UTC\n")
	s.tipi.Invalidate()
	vals := map[string]any{"ALIST_USER": "admin", "ALIST_PASS": "senha-boa-123"}
	// selo no catálogo e lista na página do app
	if d, ok := s.tipi.CatalogDetail("alist:migrated"); !ok || !d.Priv || len(d.Privileges) != 1 {
		t.Fatalf("detalhe: %+v", d)
	}
	if code, out := call(t, s.hStoreInstall, "POST", "/api/store/install", map[string]any{"urn": "alist:migrated", "values": vals}); code != 400 || !strings.Contains(out["error"].(string), "Entendo os riscos") {
		t.Fatalf("sem aceitar os riscos: %d %v", code, out)
	}
	if code, out := call(t, s.hStoreInstall, "POST", "/api/store/install", map[string]any{"urn": "alist:migrated", "values": vals, "acceptRisks": true}); code != 400 || !strings.Contains(out["error"].(string), "Senha de configuração") {
		t.Fatalf("sem senha: %d %v", code, out)
	}
	f.resp["info-app"] = `{"app":{"status":"running"}}`
	req := map[string]any{"urn": "alist:migrated", "values": vals, "acceptRisks": true, "password": "certa-123", "localSubdomain": "Minha-Lista",
		"enableAuth": true, "guest": false, "settings": map[string]any{"memoryMB": 1024, "env": []map[string]any{{"key": "TZ", "value": "America/Sao_Paulo"}}}}
	code, out := call(t, s.hStoreInstall, "POST", "/api/store/install", req)
	if code != 200 {
		t.Fatalf("instalar com privilégios e ajustes: %d %v", code, out)
	}
	in := string(f.ins["instalar-app-senha"])
	parts := strings.SplitN(in, "\n", 3)
	if len(parts) != 3 || parts[0] != "certa-123" || !strings.Contains(parts[2], "memory: 1G") || !strings.Contains(parts[2], "TZ: America/Sao_Paulo") {
		t.Fatalf("entrada do script:\n%s", in)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(parts[1]), &body); err != nil || body["localSubdomain"] != "minha-lista" || body["enableAuth"] != true || body["isVisibleOnGuestDashboard"] != false {
		t.Fatalf("formulário: %v %v", err, body)
	}
	if _, has := body["maxBackups"]; has {
		t.Fatal("limite de backups não vai na instalação (o Runtipi ignora)")
	}
	// nome de endereço local inválido
	req["localSubdomain"] = "-ruim"
	if code, _ := call(t, s.hStoreInstall, "POST", "/api/store/install", req); code != 400 {
		t.Fatal("nome de endereço local inválido deveria dar 400")
	}
	// trocar a imagem por outra (não só a versão) é recusado
	req["localSubdomain"] = ""
	req["settings"] = map[string]any{"image": "outro/app:1"}
	if code, out := call(t, s.hStoreInstall, "POST", "/api/store/install", req); code != 400 || !strings.Contains(out["error"].(string), "versão da imagem") {
		t.Fatalf("imagem trocada: %d %v", code, out)
	}
	req["settings"] = map[string]any{"image": "xhofe/alist:v3.29.0"}
	if code, out := call(t, s.hStoreInstall, "POST", "/api/store/install", req); code != 200 {
		t.Fatalf("só a versão: %d %v", code, out)
	}
	// app comum, Ajustes iguais aos de hoje: instala sem senha, pelo pedido de sempre
	writeFile(t, filepath.Join(testRuntipiDir, "repos/migrated/apps/alist/docker-compose.yml"), "services:\n  alist:\n    image: xhofe/alist:v3.30.0\n")
	f.calls = nil
	if code, out := call(t, s.hStoreInstall, "POST", "/api/store/install", map[string]any{"urn": "alist:migrated", "values": vals, "settings": map[string]any{}}); code != 200 {
		t.Fatalf("comum: %d %v", code, out)
	}
	if last := f.calls[len(f.calls)-1]; last != "instalar-app alist:migrated" {
		t.Fatalf("pedido: %v", f.calls)
	}
}

// 0.21.1: enquanto o Runtipi instala, ligar/parar/reconfigurar/Ajustes esperam (recriar o container no meio
// da instalação estragou o banco de um app); o erro da fila do Runtipi sai em português.
func TestOcupadoEnquantoInstala(t *testing.T) {
	s, f := installServer(t)
	f.resp["info-app"] = `{"app":{"status":"installing","config":{}}}`
	for _, c := range []struct {
		h    http.HandlerFunc
		path string
		body map[string]any
	}{
		{s.hStoreAppAction, "/api/store/app", map[string]any{"urn": "plex:migrated", "action": "start"}},
		{s.hStoreAppAction, "/api/store/app", map[string]any{"urn": "plex:migrated", "action": "stop"}},
		{s.hStoreSettingsSave, "/api/store/settings", map[string]any{"urn": "plex:migrated", "settings": map[string]any{}, "password": "x"}},
	} {
		f.calls = nil
		code, out := call(t, c.h, "POST", c.path, c.body)
		if code != 409 || out["busy"] != "installing" || !strings.Contains(out["error"].(string), "instalando") {
			t.Fatalf("%s %v: %d %v", c.path, c.body, code, out)
		}
		for _, cmd := range f.calls {
			if !strings.HasPrefix(cmd, "info-app") {
				t.Fatalf("pediu %q com o app instalando", cmd)
			}
		}
	}
	// terminou de instalar: liga normalmente
	f.resp["info-app"] = `{"app":{"status":"stopped"}}`
	if code, out := call(t, s.hStoreAppAction, "POST", "/api/store/app", map[string]any{"urn": "plex:migrated", "action": "start"}); code != 200 {
		t.Fatalf("ligar depois de instalar: %d %v", code, out)
	}
	// fila interna do Runtipi fora do ar
	f.fail["iniciar-app"] = errors.New("Error: dial tcp 127.0.0.1:5672: connect: connection refused")
	if code, out := call(t, s.hStoreAppAction, "POST", "/api/store/app", map[string]any{"urn": "plex:migrated", "action": "start"}); code != 502 || !strings.Contains(out["error"].(string), "fila interna") {
		t.Fatalf("erro da fila: %d %v", code, out)
	}
}

// 0.22.0: editor do Avançado. Privilégio novo só com "Entendo os riscos"; o que já estava no arquivo não conta.
func TestSettingsRaw(t *testing.T) {
	s, f := installServer(t)
	f.resp["ajustes-ler"] = "services:\n  plex:\n    network_mode: host\n"
	base := map[string]any{"urn": "plex:migrated", "password": "certa-123"}
	with := func(kv map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range kv {
			m[k] = v
		}
		return m
	}
	// o privilégio que já estava (rede do servidor) continua sem pedir nada
	if code, out := call(t, s.hStoreSettingsRaw, "POST", "/api/store/settings/raw", with(map[string]any{"raw": "services:\n  plex:\n    network_mode: host\n    shm_size: 1gb\n"})); code != 200 {
		t.Fatalf("sem privilégio novo: %d %v", code, out)
	}
	f.calls = nil
	novo := with(map[string]any{"raw": "services:\n  plex:\n    network_mode: host\n    privileged: true\n"})
	code, out := call(t, s.hStoreSettingsRaw, "POST", "/api/store/settings/raw", novo)
	if code != 409 || len(out["risks"].([]any)) != 1 || !strings.Contains(out["risks"].([]any)[0].(string), "privileged") {
		t.Fatalf("privilégio novo sem aceitar: %d %v", code, out)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "ajustes-gravar") {
			t.Fatal("gravou sem aceitar os riscos")
		}
	}
	novo["acceptRisks"] = true
	if code, out := call(t, s.hStoreSettingsRaw, "POST", "/api/store/settings/raw", novo); code != 200 {
		t.Fatalf("aceitando: %d %v", code, out)
	}
	if in := string(f.ins["ajustes-gravar"]); !strings.HasPrefix(in, "certa-123\n") || !strings.Contains(in, "privileged: true") {
		t.Fatalf("gravado:\n%s", in)
	}
	for _, raw := range []string{"services: [a, b]\n", "services:\n  plex:\n    build: .\n", "services:\n  plex: x\n", "a: b: c\n"} {
		if code, _ := call(t, s.hStoreSettingsRaw, "POST", "/api/store/settings/raw", with(map[string]any{"raw": raw, "acceptRisks": true})); code != 400 {
			t.Errorf("deveria recusar %q: %d", raw, code)
		}
	}
	if code, _ := call(t, s.hStoreSettingsRaw, "POST", "/api/store/settings/raw", map[string]any{"urn": "plex:migrated", "raw": ""}); code != 400 {
		t.Error("sem senha deveria dar 400")
	}
}

func TestSettingsPreservaManual(t *testing.T) {
	s, f := installServer(t)
	f.resp["ajustes-ler"] = "services:\n  plex:\n    volumes:\n      - /mnt/disco1/Filmes:/movies # feito à mão\n    labels:\n      x: \"1\"\n"
	settings := map[string]any{"mounts": []map[string]any{{"host": "/mnt/disco1/Filmes", "container": "/movies"}, {"host": "/mnt/disco1/Series", "container": "/tv", "readOnly": true}}, "memoryMB": 2048}
	if code, _ := call(t, s.hStoreSettingsSave, "POST", "/api/store/settings", map[string]any{"urn": "plex:migrated", "settings": settings}); code != 400 {
		t.Fatal("sem a Senha de configuração deveria dar 400")
	}
	code, out := call(t, s.hStoreSettingsSave, "POST", "/api/store/settings", map[string]any{"urn": "plex:migrated", "settings": settings, "password": "certa-123", "restart": true})
	if code != 200 {
		t.Fatalf("gravar: %d %v", code, out)
	}
	in := string(f.ins["ajustes-gravar"])
	if !strings.HasPrefix(in, "certa-123\n") || !strings.Contains(in, "# feito à mão") || !strings.Contains(in, "x: \"1\"") ||
		!strings.Contains(in, "/mnt/disco1/Series:/tv:ro") || !strings.Contains(in, "memory: 2G") {
		t.Fatalf("arquivo gravado:\n%s", in)
	}
	if f.calls[len(f.calls)-1] != "reiniciar-app plex:migrated" {
		t.Fatalf("deveria reiniciar: %v", f.calls)
	}
	// pasta perigosa: recusada antes de chegar ao servidor
	f.calls = nil
	bad := map[string]any{"mounts": []map[string]any{{"host": "/etc", "container": "/x"}}}
	if code, _ := call(t, s.hStoreSettingsSave, "POST", "/api/store/settings", map[string]any{"urn": "plex:migrated", "settings": bad, "password": "certa-123"}); code != 400 {
		t.Fatal("pasta do sistema deveria ser recusada")
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "ajustes-gravar") {
			t.Fatal("gravou mesmo recusando")
		}
	}
}
