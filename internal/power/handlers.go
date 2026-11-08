package power

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"brunos/internal/hostcmd"
	"brunos/internal/sshknown"
)

// Register liga as rotas /api/power*. wrap aplica autenticação e CSRF.
func (m *Monitor) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/power", wrap(m.hGet))
	mux.HandleFunc("PUT /api/power/config", wrap(m.hConfig))
	mux.HandleFunc("POST /api/power/cancel", wrap(m.hCancel))
	mux.HandleFunc("GET /api/power/setup", wrap(m.hSetup))
	mux.HandleFunc("POST /api/power/test", wrap(m.hTest))
	mux.HandleFunc("POST /api/power/charge", wrap(m.hCharge))
}

// hCharge aplica o limite de carga ({limit}) ou a proteção ({protect: "on"|"off"}), ou
// procura de novo o que o notebook oferece ({detect: true}).
func (m *Monitor) hCharge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Limit   int    `json:"limit"`
		Protect string `json:"protect"`
		Detect  bool   `json:"detect"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	if body.Detect {
		m.DetectCharge()
		writeJSON(w, 200, map[string]any{"charge": m.ChargeInfo()})
		return
	}
	info, err := m.SetCharge(r.Context(), body.Limit, body.Protect)
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error(), "charge": info})
		return
	}
	writeJSON(w, 200, map[string]any{"charge": info})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(v)
}

func (m *Monitor) suggested() string {
	if h := m.Config().Host; h != "" {
		return h
	}
	if m.SuggestHost != nil {
		return m.SuggestHost()
	}
	return ""
}

func (m *Monitor) hGet(w http.ResponseWriter, r *http.Request) {
	keyReady := m.runner != nil && m.runner.HasKey()
	writeJSON(w, 200, map[string]any{
		"config": m.Config(), "status": m.Status(), "outages": m.Outages(), "drainW": m.DrainW(),
		"keyReady": keyReady, "suggestedHost": m.suggested(), "needsUpdate": m.Config().NeedsUpdate(),
		"charge": m.ChargeInfo(),
	})
}

func (m *Monitor) hConfig(w http.ResponseWriter, r *http.Request) {
	var c Config
	if err := decode(r, &c); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	c.Host = strings.TrimSpace(c.Host)
	if err := m.SetConfig(c); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("energia: configuração alterada", "ligado", c.Enabled, "limite", c.Threshold, "simulacao", c.Simulate)
	writeJSON(w, 200, map[string]any{"config": m.Config(), "status": m.Status()})
}

func (m *Monitor) hCancel(w http.ResponseWriter, r *http.Request) {
	if err := m.Cancel(); err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"status": m.Status()})
}

// SetupCommands monta os comandos que o usuário cola no servidor para criar o usuário dedicado.
func SetupCommands(user, authorizedLine string) string {
	home := "/home/" + user
	return strings.Join(append([]string{
		"sudo useradd --system --create-home --shell /bin/sh " + user,
		"sudo usermod -p '*' " + user,
		"sudo install -d -m 700 -o " + user + " -g " + user + " " + home + "/.ssh",
	}, keyAndSudoers(user, authorizedLine)...), "\n")
}

// UpdateCommands atualiza um servidor já configurado (usuário existente) para a versão atual:
// instala o script e reescreve a linha do authorized_keys (chave nova) e a do sudoers.
func UpdateCommands(user, authorizedLine string) string {
	return strings.Join(keyAndSudoers(user, authorizedLine), "\n")
}

func keyAndSudoers(user, authorizedLine string) []string {
	home := "/home/" + user
	return []string{
		// o script vai num heredoc com aspas ('EOF'): nada dentro dele é interpretado ao colar
		"sudo tee " + hostcmd.ScriptPath + " >/dev/null <<'EOF'\n" + strings.TrimRight(hostcmd.HostScript, "\n") + "\nEOF",
		"sudo chown root:root " + hostcmd.ScriptPath,
		"sudo chmod 755 " + hostcmd.ScriptPath,
		"echo '" + authorizedLine + "' | sudo tee " + home + "/.ssh/authorized_keys >/dev/null",
		"sudo chown " + user + ":" + user + " " + home + "/.ssh/authorized_keys",
		"sudo chmod 600 " + home + "/.ssh/authorized_keys",
		"echo '" + user + " ALL=(root) NOPASSWD: " + hostcmd.ScriptPath + "' | sudo tee /etc/sudoers.d/" + user + " >/dev/null",
		"sudo chmod 440 /etc/sudoers.d/" + user,
		"sudo visudo -c",
	}
}

func (m *Monitor) hSetup(w http.ResponseWriter, r *http.Request) {
	if m.runner == nil {
		writeJSON(w, 503, map[string]string{"error": "comandos no servidor indisponíveis"})
		return
	}
	line, fp, err := m.runner.PublicKey()
	if err != nil {
		slog.Error("energia: chave do BrunOS", "erro", err)
		writeJSON(w, 500, map[string]string{"error": "não foi possível criar a chave do BrunOS"})
		return
	}
	auth, _ := m.runner.AuthorizedLine()
	user := m.Config().User
	writeJSON(w, 200, map[string]any{
		"publicKey": line, "fingerprint": fp, "authorizedLine": auth, "user": user,
		"commands": SetupCommands(user, auth), "updateCommands": UpdateCommands(user, auth),
		"command": hostcmd.ScriptPath, "suggestedHost": m.suggested(),
	})
}

func (m *Monitor) hTest(w http.ResponseWriter, r *http.Request) {
	if m.runner == nil {
		writeJSON(w, 503, map[string]string{"error": "comandos no servidor indisponíveis"})
		return
	}
	var body struct {
		Host  string `json:"host"`
		Port  int    `json:"port"`
		User  string `json:"user"`
		Trust string `json:"trust"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	t := hostcmd.Target{Host: strings.TrimSpace(body.Host), Port: body.Port, User: strings.TrimSpace(body.User)}
	if t.Port == 0 {
		t.Port = 22
	}
	if t.User == "" {
		t.User = hostcmd.DefaultUser
	}
	if t.Host == "" || !HostOK(t.Host) || t.Port < 1 || t.Port > 65535 || !userRe.MatchString(t.User) || len(body.Trust) > 128 {
		writeJSON(w, 400, map[string]string{"error": "endereço, porta ou usuário do servidor inválido"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	err := m.runner.Test(ctx, t, body.Trust)
	var unk *sshknown.UnknownHostError
	var mis *sshknown.HostKeyMismatchError
	switch {
	case err == nil:
		if err := m.MarkTested(t); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		slog.Info("energia: teste de conexão passou", "destino", sshknown.Addr(t.Host, t.Port), "usuario", t.User)
		writeJSON(w, 200, map[string]any{"ok": true, "config": m.Config()})
	case errors.As(err, &unk):
		writeJSON(w, 409, map[string]any{"error": unk.Error(), "unknownHost": true, "addr": unk.Addr, "keyType": unk.KeyType, "fingerprint": unk.Fingerprint})
	case errors.As(err, &mis):
		writeJSON(w, 409, map[string]any{"error": mis.Error(), "hostKeyChanged": true, "addr": mis.Addr, "fingerprint": mis.Fingerprint, "known": mis.Known})
	default:
		writeJSON(w, 502, map[string]string{"error": err.Error()})
	}
}
