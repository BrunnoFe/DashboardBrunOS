package main

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"brunos/internal/store"
)

// Preferências da interface (/data/settings.json): widgets, páginas, dock, papel de parede, bloqueio...
//
// Proteções (0.11.1, depois de o Bruno achar tudo zerado numa manhã):
//   - erro ao ler o arquivo NÃO vira "preferências vazias" (a tela entraria com o padrão e gravaria
//     o padrão por cima): o GET responde 500 e a interface tenta de novo;
//   - o servidor recusa gravar preferências "de primeira vez" (sem welcomed) por cima de preferências
//     já configuradas (com welcomed): esse é o desenho exato do estrago;
//   - antes de substituir, guarda uma cópia por dia em /data/settings-backup (últimos 14 dias), que
//     dá para restaurar em Configurações → Sistema.

const settingsKeep = 14

var backupName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.json$`)

func (s *server) settingsPath() string { return filepath.Join(s.cfg.DataDir, "settings.json") }
func (s *server) settingsBackupDir() string {
	return filepath.Join(s.cfg.DataDir, "settings-backup")
}

// loadSettings lê o arquivo. Sem arquivo = {} (primeira vez); arquivo ilegível = erro.
func (s *server) loadSettings() (map[string]any, error) {
	m := map[string]any{}
	if _, err := store.ReadJSON(s.settingsPath(), &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// readSettings é para o servidor consultar uma preferência: na falha, usa os padrões.
func (s *server) readSettings() map[string]any {
	s.settingMu.Lock()
	defer s.settingMu.Unlock()
	m, err := s.loadSettings()
	if err != nil {
		return map[string]any{}
	}
	return m
}

func (s *server) hSettingsGet(w http.ResponseWriter, r *http.Request) {
	s.settingMu.Lock()
	m, err := s.loadSettings()
	s.settingMu.Unlock()
	if err != nil {
		slog.Error("ler preferências", "erro", err)
		writeJSON(w, 500, map[string]string{"error": "não consegui ler suas preferências (nada foi apagado): recarregue a página"})
		return
	}
	writeJSON(w, 200, m)
}

func welcomed(m map[string]any) bool { v, _ := m["welcomed"].(bool); return v }

func (s *server) hSettingsPut(w http.ResponseWriter, r *http.Request) {
	var m map[string]any
	if err := decode(r, &m, 512<<10); err != nil || m == nil {
		writeJSON(w, 400, map[string]string{"error": "preferências inválidas (máx. 512 KB)"})
		return
	}
	s.settingMu.Lock()
	defer s.settingMu.Unlock()
	old, err := s.loadSettings()
	if err == nil && welcomed(old) && !welcomed(m) {
		slog.Warn("preferências: recusei gravar o padrão por cima das preferências configuradas")
		writeJSON(w, 409, map[string]string{"error": "esta tela abriu sem as suas preferências: recarregue a página (nada foi apagado)"})
		return
	}
	if err == nil && welcomed(old) {
		s.backupSettings(old, time.Now())
	}
	if err := store.WriteJSON(s.settingsPath(), m, 0o600); err != nil {
		slog.Error("salvar preferências", "erro", err)
		writeJSON(w, 500, map[string]string{"error": "não foi possível salvar"})
		return
	}
	s.integ.SetUsage(m) // os widgets dizem quais integrações precisam ser buscadas
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// backupSettings guarda a versão anterior uma vez por dia e apaga as cópias mais velhas.
// Chamado com settingMu travado.
func (s *server) backupSettings(old map[string]any, now time.Time) {
	dir := s.settingsBackupDir()
	p := filepath.Join(dir, now.Format("2006-01-02")+".json")
	if _, err := os.Stat(p); err == nil {
		return
	}
	if err := store.WriteJSON(p, old, 0o600); err != nil {
		slog.Warn("cópia das preferências", "erro", err)
		return
	}
	names := s.backupNames()
	for len(names) > settingsKeep {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// backupNames lista as cópias, da mais velha para a mais nova.
func (s *server) backupNames() []string {
	entries, _ := os.ReadDir(s.settingsBackupDir())
	var out []string
	for _, e := range entries {
		if !e.IsDir() && backupName.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	slices.Sort(out)
	return out
}

type settingsBackup struct {
	Date    string `json:"date"`
	Widgets int    `json:"widgets"`
	Pages   int    `json:"pages"`
}

// resumo de widgets2 = { areas: { página: [ids] }, items: { id: widget } }
func widgetSummary(m map[string]any) (widgets, pages int) {
	w, _ := m["widgets2"].(map[string]any)
	items, _ := w["items"].(map[string]any)
	areas, _ := w["areas"].(map[string]any)
	for _, ids := range areas {
		if l, _ := ids.([]any); len(l) > 0 {
			pages++
		}
	}
	return len(items), pages
}

// hSettingsBackups lista as cópias (mais nova primeiro) com um resumo para reconhecer cada uma.
func (s *server) hSettingsBackups(w http.ResponseWriter, r *http.Request) {
	s.settingMu.Lock()
	names := s.backupNames()
	out := []settingsBackup{}
	for i := len(names) - 1; i >= 0; i-- {
		m := map[string]any{}
		if _, err := store.ReadJSON(filepath.Join(s.settingsBackupDir(), names[i]), &m); err != nil {
			continue
		}
		wd, pg := widgetSummary(m)
		out = append(out, settingsBackup{Date: names[i][:10], Widgets: wd, Pages: pg})
	}
	s.settingMu.Unlock()
	writeJSON(w, 200, out)
}

// hSettingsRestore volta as preferências para a cópia de um dia. As atuais viram a cópia de hoje,
// se ainda não houver uma (dá para desfazer).
func (s *server) hSettingsRestore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date string `json:"date"`
	}
	if err := decode(r, &body, 1<<10); err != nil || !backupName.MatchString(body.Date+".json") {
		writeJSON(w, 400, map[string]string{"error": "cópia inválida"})
		return
	}
	s.settingMu.Lock()
	defer s.settingMu.Unlock()
	m := map[string]any{}
	ok, err := store.ReadJSON(filepath.Join(s.settingsBackupDir(), body.Date+".json"), &m)
	if err != nil || !ok {
		writeJSON(w, 404, map[string]string{"error": "essa cópia não existe mais"})
		return
	}
	if old, err := s.loadSettings(); err == nil && welcomed(old) {
		s.backupSettings(old, time.Now())
	}
	if err := store.WriteJSON(s.settingsPath(), m, 0o600); err != nil {
		writeJSON(w, 500, map[string]string{"error": "não foi possível restaurar"})
		return
	}
	s.integ.SetUsage(m)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
