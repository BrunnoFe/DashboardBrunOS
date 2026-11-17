package main

// Instalar, editar a instalação e Ajustes dentro do BrunOS (0.20.0, decisão 61).
//
// Instalar e editar: o BrunOS monta o formulário com as perguntas do app (lidas da loja), confere as
// respostas e manda ao Runtipi pelo script do servidor, que chama a API dele com a chave do runtipi-cli
// (a chave nunca sai do servidor). O Runtipi continua dono do app: banco, atualização e backup.
//
// Ajustes: o arquivo user-config/<loja>/<app>/docker-compose.yml do Runtipi, lido e editado pelo pacote
// appcfg (só o que a tela mostra muda; o que foi escrito à mão fica). Gravar pede a Senha de configuração,
// conferida pelo script do servidor.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"brunos/internal/appcfg"
	"brunos/internal/events"
	"brunos/internal/hostcmd"
	"brunos/internal/runtipi"
)

var domainRe = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$`)

// installReq é o formulário de instalação (ou de edição) que vem da tela.
type installReq struct {
	URN          string         `json:"urn"`
	Values       map[string]any `json:"values"` // env_variable → resposta (texto, número ou verdadeiro/falso)
	Port         int            `json:"port"`   // 0 = a porta da loja
	Exposed      bool           `json:"exposed"`
	Domain       string         `json:"domain"`
	ExposedLocal bool           `json:"exposedLocal"`
	OpenPort     *bool          `json:"openPort"`
}

// tipiApp é o que a API do Runtipi conta de um app instalado (GET /api/apps/<urn>).
type tipiApp struct {
	App *struct {
		Status                    string         `json:"status"`
		Port                      *int           `json:"port"`
		Exposed                   bool           `json:"exposed"`
		ExposedLocal              bool           `json:"exposedLocal"`
		OpenPort                  *bool          `json:"openPort"`
		Domain                    *string        `json:"domain"`
		Config                    map[string]any `json:"config"`
		IsVisibleOnGuestDashboard *bool          `json:"isVisibleOnGuestDashboard"`
		EnableAuth                *bool          `json:"enableAuth"`
		LocalSubdomain            *string        `json:"localSubdomain"`
		MaxBackups                *int           `json:"maxBackups"`
	} `json:"app"`
}

func (s *server) tipiInfo(ctx context.Context, urn string) (tipiApp, error) {
	var t tipiApp
	out, err := s.storeRun(ctx, hostcmd.CmdAppInfo+" "+urn, nil)
	if err != nil {
		return t, tipiError(err)
	}
	if json.Unmarshal(out, &t) != nil {
		return t, errors.New("o Runtipi respondeu algo que o BrunOS não entendeu")
	}
	return t, nil
}

// tipiErrors traduz os erros do Runtipi (chaves de tradução dele) para o português.
var tipiErrors = map[string]string{
	"APP_ERROR_PORT_ALREADY_IN_USE":            "a porta {port} já é usada por outro app: escolha outra",
	"APP_ERROR_DOMAIN_REQUIRED_IF_EXPOSE_APP":  "para expor o app, preencha o domínio",
	"APP_ERROR_DOMAIN_NOT_VALID":               "o domínio não é válido",
	"APP_ERROR_DOMAIN_ALREADY_IN_USE":          "esse domínio já é usado por outro app",
	"APP_ERROR_ARCHITECTURE_NOT_SUPPORTED":     "este app não roda no processador do servidor",
	"APP_ERROR_APP_NOT_FOUND":                  "o Runtipi não achou o app: atualize as lojas e tente de novo",
	"APP_ERROR_APP_ALREADY_INSTALLED":          "este app já está instalado",
	"APP_ERROR_APP_NOT_EXPOSABLE":              "este app não pode ser exposto num domínio",
	"APP_ERROR_LOCAL_SUBDOMAIN_ALREADY_IN_USE": "esse endereço local já é usado por outro app",
	"SYSTEM_ERROR_INVALID_BODY":                "o Runtipi recusou o formulário",
}

// tipiError transforma a resposta de erro do Runtipi ({"message": "APP_ERROR_...", "intlParams": {...}}).
func tipiError(err error) error {
	msg := err.Error()
	i := strings.Index(msg, "{")
	if i < 0 {
		return err
	}
	var e struct {
		Message    string         `json:"message"`
		IntlParams map[string]any `json:"intlParams"`
	}
	if json.Unmarshal([]byte(msg[i:]), &e) != nil || e.Message == "" {
		return err
	}
	t, ok := tipiErrors[e.Message]
	if !ok {
		return fmt.Errorf("o Runtipi recusou: %s", e.Message)
	}
	for k, v := range e.IntlParams {
		t = strings.ReplaceAll(t, "{"+k+"}", fmt.Sprint(v))
	}
	return friendlyErr{t, err}
}

// fieldValue confere uma resposta conforme o tipo da pergunta e devolve o valor no tipo que o Runtipi espera.
func fieldValue(f runtipi.FormField, v any) (any, error) {
	bad := func(why string) error { return fmt.Errorf("%s: %s", f.Label, why) }
	if f.Type == "boolean" {
		b, ok := v.(bool)
		if !ok {
			return nil, bad("escolha sim ou não")
		}
		return b, nil
	}
	if f.Type == "number" {
		var n float64
		switch x := v.(type) {
		case float64:
			n = x
		case string:
			var err error
			if n, err = strconv.ParseFloat(strings.TrimSpace(x), 64); err != nil {
				return nil, bad("digite um número")
			}
		default:
			return nil, bad("digite um número")
		}
		if math.IsNaN(n) || math.IsInf(n, 0) || (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
			return nil, bad("número fora do permitido")
		}
		return n, nil
	}
	sv, ok := v.(string)
	if !ok {
		return nil, bad("resposta inválida")
	}
	if len(sv) > 2000 || strings.ContainsAny(sv, "\n\r\x00") {
		return nil, bad("texto inválido")
	}
	if len(f.Options) > 0 {
		for _, o := range f.Options {
			if o.Value == sv {
				return sv, nil
			}
		}
		return nil, bad("escolha uma das opções")
	}
	if f.Min != nil && float64(len(sv)) < *f.Min {
		return nil, bad(fmt.Sprintf("precisa de pelo menos %d caracteres", int(*f.Min)))
	}
	if f.Max != nil && float64(len(sv)) > *f.Max {
		return nil, bad(fmt.Sprintf("pode ter no máximo %d caracteres", int(*f.Max)))
	}
	switch f.Type {
	case "email":
		if !strings.Contains(sv, "@") || strings.ContainsAny(sv, " ") {
			return nil, bad("e-mail inválido")
		}
	case "url":
		if u, err := url.Parse(sv); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, bad("endereço inválido (comece com http:// ou https://)")
		}
	case "fqdn":
		if !domainRe.MatchString(sv) {
			return nil, bad("domínio inválido")
		}
	case "ip":
		if net.ParseIP(sv) == nil {
			return nil, bad("IP inválido")
		}
	case "fqdnip":
		if net.ParseIP(sv) == nil && !domainRe.MatchString(sv) {
			return nil, bad("domínio ou IP inválido")
		}
	}
	if f.Regex != "" {
		if re, err := regexp.Compile(f.Regex); err == nil && !re.MatchString(sv) {
			if f.PatternErr != "" {
				return nil, bad(f.PatternErr)
			}
			return nil, bad("formato inválido")
		}
	}
	return sv, nil
}

// buildBody monta o corpo que vai ao Runtipi. cur (editar) guarda as respostas atuais: senha deixada em
// branco continua a mesma, e as "random" (o Runtipi gerou) nunca mudam.
func buildBody(fields []runtipi.FormField, exposable bool, req installReq, cur *tipiApp) (map[string]any, error) {
	body := map[string]any{}
	var old map[string]any
	if cur != nil && cur.App != nil {
		old = cur.App.Config
	}
	for _, f := range fields {
		prev, hasPrev := old[f.Env]
		if f.Type == "random" {
			if hasPrev {
				body[f.Env] = prev
			}
			continue
		}
		v, ok := req.Values[f.Env]
		if !ok || v == nil || v == "" {
			if hasPrev && f.Type == "password" {
				body[f.Env] = prev
				continue
			}
			if f.Required {
				return nil, fmt.Errorf("preencha: %s", f.Label)
			}
			continue
		}
		val, err := fieldValue(f, v)
		if err != nil {
			return nil, err
		}
		body[f.Env] = val
	}
	if req.Port != 0 {
		if req.Port < 1024 || req.Port > 65535 {
			return nil, errors.New("a porta precisa estar entre 1024 e 65535")
		}
		body["port"] = req.Port
	}
	body["exposed"] = false
	if exposable {
		if req.Exposed {
			d := strings.TrimSpace(req.Domain)
			if !domainRe.MatchString(d) {
				return nil, errors.New("para expor o app, preencha um domínio válido (ex.: app.meudominio.com)")
			}
			body["exposed"], body["domain"] = true, d
		}
		body["exposedLocal"] = req.ExposedLocal
	}
	body["openPort"] = req.OpenPort == nil || *req.OpenPort
	if cur != nil && cur.App != nil { // o que a tela não mostra continua como está
		a := cur.App
		if a.IsVisibleOnGuestDashboard != nil {
			body["isVisibleOnGuestDashboard"] = *a.IsVisibleOnGuestDashboard
		}
		if a.EnableAuth != nil {
			body["enableAuth"] = *a.EnableAuth
		}
		if a.LocalSubdomain != nil && *a.LocalSubdomain != "" {
			body["localSubdomain"] = *a.LocalSubdomain
		}
		if a.MaxBackups != nil {
			body["maxBackups"] = *a.MaxBackups
		}
		if _, ok := body["port"]; !ok && a.Port != nil {
			body["port"] = *a.Port
		}
	}
	return body, nil
}

// hostFolder é uma pasta do servidor oferecida pronta nos Ajustes e no formulário de instalação.
type hostFolder struct {
	Name   string `json:"name"`
	Host   string `json:"host"`             // caminho no servidor (ou com variável do Runtipi)
	Kind   string `json:"kind"`             // disk, home, media
	RootID string `json:"rootId,omitempty"` // raiz do Arquivos, para sugerir as subpastas
}

func (s *server) hostFolders() []hostFolder {
	out := []hostFolder{}
	base := filepath.ToSlash(filepath.Clean(s.cfg.FilesDir))
	if s.files != nil {
		for _, r := range s.files.Roots() {
			p := filepath.ToSlash(r.Path)
			rel, ok := strings.CutPrefix(p, base)
			if !ok {
				continue
			}
			switch {
			case strings.HasPrefix(rel, "/mnt/"):
				out = append(out, hostFolder{Name: r.Name, Host: rel, Kind: "disk", RootID: r.ID})
			case rel == "/home":
				out = append(out, hostFolder{Name: "Pastas pessoais (/home)", Host: rel, Kind: "home", RootID: r.ID})
			}
		}
	}
	return append(out,
		hostFolder{Name: "Mídia do Runtipi (filmes, séries, músicas)", Host: "${ROOT_FOLDER_HOST}/media/data", Kind: "media"},
		hostFolder{Name: "Downloads do Runtipi", Host: "${ROOT_FOLDER_HOST}/media/downloads", Kind: "media"})
}

// formCurrent é o que a tela recebe das respostas atuais (sem nenhuma senha).
type formCurrent struct {
	Status       string         `json:"status"`
	Port         int            `json:"port,omitempty"`
	Exposed      bool           `json:"exposed"`
	Domain       string         `json:"domain,omitempty"`
	ExposedLocal bool           `json:"exposedLocal"`
	OpenPort     bool           `json:"openPort"`
	Values       map[string]any `json:"values"`
	Secrets      []string       `json:"secrets"` // perguntas de senha que já têm resposta (a tela mostra "guardada")
}

func currentOf(t tipiApp, fields []runtipi.FormField) *formCurrent {
	if t.App == nil {
		return nil
	}
	a := t.App
	c := &formCurrent{Status: a.Status, Exposed: a.Exposed, ExposedLocal: a.ExposedLocal, OpenPort: a.OpenPort == nil || *a.OpenPort, Values: map[string]any{}, Secrets: []string{}}
	if a.Port != nil {
		c.Port = *a.Port
	}
	if a.Domain != nil {
		c.Domain = *a.Domain
	}
	for _, f := range fields {
		v, ok := a.Config[f.Env]
		if !ok || f.Type == "random" {
			continue
		}
		if f.Type == "password" {
			if v != nil && v != "" {
				c.Secrets = append(c.Secrets, f.Env)
			}
			continue
		}
		c.Values[f.Env] = v
	}
	return c
}

// hStoreForm: GET /api/store/form?urn= — perguntas do app, pastas prontas e, se instalado, as respostas atuais.
func (s *server) hStoreForm(w http.ResponseWriter, r *http.Request) {
	urn := r.URL.Query().Get("urn")
	d, ok := s.tipi.CatalogDetail(urn)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "app não encontrado na loja"})
		return
	}
	_, info := s.tipi.Apps()
	out := map[string]any{"fields": d.Fields, "exposable": d.Exposable, "port": d.Port, "installed": d.Installed,
		"folders": s.hostFolders(), "localDomain": info.LocalDomain, "script": s.updates.Snapshot().Script.Have, "want": hostcmd.ScriptVersion}
	if d.Installed {
		fields, _, _ := s.tipi.FormFields(urn)
		if t, err := s.tipiInfo(r.Context(), urn); err != nil {
			out["currentErr"] = err.Error()
		} else {
			out["current"] = currentOf(t, fields)
		}
	}
	writeJSON(w, 200, out)
}

// hStoreInstall: POST /api/store/install (instalar) e /api/store/reconfig (editar as respostas e reiniciar).
func (s *server) hStoreInstall(w http.ResponseWriter, r *http.Request) {
	edit := strings.HasSuffix(r.URL.Path, "/reconfig")
	var req installReq
	if err := decode(r, &req, 64<<10); err != nil || !hostcmd.ValidApp(req.URN) {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	a, ok := s.tipi.CatalogDetail(req.URN)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "app não encontrado na loja"})
		return
	}
	if a.Installed != edit {
		msg := "este app já está instalado"
		if edit {
			msg = "este app não está instalado"
		}
		writeJSON(w, 409, map[string]string{"error": msg})
		return
	}
	if a.ID == selfApp {
		writeJSON(w, 400, map[string]string{"error": "este é o próprio BrunOS: mude a instalação dele pelo painel do Runtipi"})
		return
	}
	fields, exposable, ok := s.tipi.FormFields(req.URN)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "não deu para ler as perguntas do app na loja"})
		return
	}
	if !s.storeMu.TryLock() {
		writeJSON(w, 409, map[string]string{"error": "a Loja já está fazendo outra coisa no servidor: espere terminar"})
		return
	}
	defer s.storeMu.Unlock()
	var cur *tipiApp
	if edit {
		t, err := s.tipiInfo(r.Context(), req.URN)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		cur = &t
	}
	body, err := buildBody(fields, exposable, req, cur)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	b, _ := json.Marshal(body)
	cmd := hostcmd.CmdAppInstall
	if edit {
		cmd = hostcmd.CmdAppReconfig
	}
	if _, err := s.storeRun(r.Context(), cmd+" "+req.URN, b); err != nil {
		writeJSON(w, 502, map[string]string{"error": tipiError(err).Error(), "old": strconv.FormatBool(errors.Is(err, hostcmd.ErrOldScript))})
		return
	}
	s.tipi.Invalidate()
	if edit {
		s.events.Emit(events.Event{Kind: "store", Level: events.Info, Title: a.Name + ": instalação alterada", Body: "Respostas da instalação alteradas pela Loja do BrunOS; o app foi reiniciado."})
	} else {
		s.events.Emit(events.Event{Kind: "store", Level: events.Info, Title: "Instalando " + a.Name, Body: "Pedido pela Loja do BrunOS ao Runtipi."})
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// hStoreStatus: GET /api/store/status?urn= — em que pé está o app no Runtipi (para acompanhar a instalação).
func (s *server) hStoreStatus(w http.ResponseWriter, r *http.Request) {
	urn := r.URL.Query().Get("urn")
	if !hostcmd.ValidApp(urn) {
		writeJSON(w, 400, map[string]string{"error": "app inválido"})
		return
	}
	t, err := s.tipiInfo(r.Context(), urn)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	st := "gone" // o Runtipi apaga o app quando a instalação falha
	if t.App != nil {
		st = t.App.Status
	}
	if st == "running" || st == "stopped" || st == "gone" {
		s.tipi.Invalidate()
	}
	writeJSON(w, 200, map[string]string{"status": st})
}

func (s *server) installedApp(urn string) (runtipi.App, bool) {
	a, ok := s.appByURN(urn)
	return a, ok && hostcmd.ValidApp(urn)
}

// hStoreSettings: GET /api/store/settings?urn= — os Ajustes de um app instalado.
func (s *server) hStoreSettings(w http.ResponseWriter, r *http.Request) {
	urn := r.URL.Query().Get("urn")
	a, ok := s.installedApp(urn)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "esse app não está instalado"})
		return
	}
	raw, err := s.storeRun(r.Context(), hostcmd.CmdAppCfgRead+" "+urn, nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error(), "old": strconv.FormatBool(errors.Is(err, hostcmd.ErrOldScript))})
		return
	}
	st, err := appcfg.Parse(raw, a.ID)
	if err != nil {
		writeJSON(w, 200, map[string]any{"error": err.Error(), "raw": string(raw), "folders": s.hostFolders()})
		return
	}
	writeJSON(w, 200, map[string]any{"state": st, "raw": string(raw), "folders": s.hostFolders(), "service": a.ID})
}

// hStoreSettingsSave: POST /api/store/settings {urn, settings, password, restart}.
func (s *server) hStoreSettingsSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URN      string          `json:"urn"`
		Settings appcfg.Settings `json:"settings"`
		Password string          `json:"password"`
		Restart  bool            `json:"restart"`
	}
	if err := decode(r, &req, 64<<10); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	a, ok := s.installedApp(req.URN)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "esse app não está instalado"})
		return
	}
	if a.ID == selfApp {
		writeJSON(w, 400, map[string]string{"error": "este é o próprio BrunOS: os ajustes dele ficam no painel do Runtipi"})
		return
	}
	if req.Password == "" || len(req.Password) > 200 || strings.ContainsAny(req.Password, "\r\n") {
		writeJSON(w, 400, map[string]string{"error": "digite a Senha de configuração"})
		return
	}
	if !s.storeMu.TryLock() {
		writeJSON(w, 409, map[string]string{"error": "a Loja já está fazendo outra coisa no servidor: espere terminar"})
		return
	}
	defer s.storeMu.Unlock()
	raw, err := s.storeRun(r.Context(), hostcmd.CmdAppCfgRead+" "+req.URN, nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	out, err := appcfg.Apply(raw, a.ID, req.Settings)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	in := append([]byte(req.Password+"\n"), out...)
	if _, err := s.storeRun(r.Context(), hostcmd.CmdAppCfgWrite+" "+req.URN, in); err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	s.events.Emit(events.Event{Kind: "store", Level: events.Info, Title: a.Name + ": ajustes alterados", Body: "Pastas, limites ou variáveis alterados pela Loja do BrunOS (cópia do arquivo anterior guardada no servidor)."})
	restarted := ""
	if req.Restart {
		if _, err := s.storeRun(r.Context(), hostcmd.CmdAppRestart+" "+req.URN, nil); err != nil {
			restarted = tipiError(err).Error()
		}
	}
	st, _ := appcfg.Parse(out, a.ID)
	writeJSON(w, 200, map[string]any{"ok": true, "state": st, "raw": string(out), "restartErr": restarted})
}
