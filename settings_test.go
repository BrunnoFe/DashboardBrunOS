package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"brunos/internal/integrations"
)

func settingsServer(t *testing.T) *server {
	t.Helper()
	dir := t.TempDir()
	s := &server{cfg: config{DataDir: dir}}
	s.integ = integrations.NewHub(integrations.NewStore(filepath.Join(dir, "integrations.json"), nil), integrations.NewSeriesStore(""), nil)
	return s
}

func put(t *testing.T, s *server, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.hSettingsPut(w, r)
	return w
}

// O estrago da 0.11.0: a tela abriu sem as preferências e gravou o padrão por cima.
func TestSettingsNeverOverwrittenByDefaults(t *testing.T) {
	s := settingsServer(t)
	if w := put(t, s, `{"welcomed":false,"widgets2":null}`); w.Code != 200 { // primeira vez: pode
		t.Fatalf("primeira vez: %d %s", w.Code, w.Body)
	}
	mine := `{"welcomed":true,"widgets2":{"items":{"a":{},"b":{}},"areas":{"0":["a"],"1":["b"]}}}`
	if w := put(t, s, mine); w.Code != 200 {
		t.Fatalf("configurar: %d", w.Code)
	}
	w := put(t, s, `{"welcomed":false,"widgets2":null}`)
	if w.Code != 409 {
		t.Fatalf("gravar o padrão por cima deveria ser recusado, veio %d", w.Code)
	}
	got := s.readSettings()
	if !welcomed(got) {
		t.Fatal("as preferências configuradas se perderam")
	}
	// mudar normalmente continua funcionando e guarda a cópia do dia
	if w := put(t, s, `{"welcomed":true,"widgets2":{"items":{},"areas":{}}}`); w.Code != 200 {
		t.Fatalf("mudança normal: %d", w.Code)
	}
	day := time.Now().Format("2006-01-02")
	if _, err := os.Stat(filepath.Join(s.settingsBackupDir(), day+".json")); err != nil {
		t.Fatal("deveria ter guardado a cópia de hoje")
	}
	// lista e restaura
	lw := httptest.NewRecorder()
	s.hSettingsBackups(lw, httptest.NewRequest(http.MethodGet, "/api/settings/backups", nil))
	var list []settingsBackup
	_ = json.Unmarshal(lw.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Date != day || list[0].Widgets != 2 || list[0].Pages != 2 {
		t.Fatalf("lista das cópias: %s", lw.Body)
	}
	rw := httptest.NewRecorder()
	s.hSettingsRestore(rw, httptest.NewRequest(http.MethodPost, "/api/settings/restore", strings.NewReader(`{"date":"`+day+`"}`)))
	if rw.Code != 200 {
		t.Fatalf("restaurar: %d %s", rw.Code, rw.Body)
	}
	if items := s.readSettings()["widgets2"].(map[string]any)["items"].(map[string]any); len(items) != 2 {
		t.Fatal("a cópia restaurada deveria ter os 2 widgets")
	}
	bad := httptest.NewRecorder()
	s.hSettingsRestore(bad, httptest.NewRequest(http.MethodPost, "/api/settings/restore", strings.NewReader(`{"date":"../settings"}`)))
	if bad.Code != 400 {
		t.Fatalf("nome de cópia estranho deveria ser recusado: %d", bad.Code)
	}
}

// Arquivo ilegível não vira "preferências vazias".
func TestSettingsReadErrorIsError(t *testing.T) {
	s := settingsServer(t)
	if err := os.WriteFile(s.settingsPath(), []byte(`{"welcomed":tr`), 0o600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.hSettingsGet(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if w.Code != 500 {
		t.Fatalf("arquivo quebrado deveria dar erro, veio %d %s", w.Code, w.Body)
	}
}

func TestSettingsBackupKeeps14(t *testing.T) {
	s := settingsServer(t)
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local)
	for i := range 20 {
		s.backupSettings(map[string]any{"welcomed": true}, start.AddDate(0, 0, i))
	}
	names := s.backupNames()
	if len(names) != settingsKeep || names[0] != "2026-01-07.json" {
		t.Fatalf("cópias: %v", names)
	}
}
