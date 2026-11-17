package main

// Janela Loja (0.18.0): o catálogo das lojas que o Runtipi já baixou (só leitura de arquivos) e o
// gerenciamento dos apps e das lojas pelo script do servidor, que chama o runtipi-cli. Instalar continua
// sendo na tela do Runtipi (o runtipi-cli não instala: a instalação tem formulário).

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"brunos/internal/events"
	"brunos/internal/framecheck"
	"brunos/internal/hostcmd"
	"brunos/internal/updates"
)

// selfApp é o próprio BrunOS na loja: parar ou desinstalar por aqui derrubaria o painel no meio do pedido.
const selfApp = "brunos"

var storeAnsi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// storeAct faz um pedido da janela Loja ao script do servidor (só os pedidos da Loja).
// in vai pela entrada padrão do script: a Senha de formatação no desinstalar (nunca na linha de comando).
func (s *server) storeAct(ctx context.Context, cmd string, in []byte) error {
	if !hostcmd.StoreActAllowed(cmd) {
		return errors.New("pedido não permitido")
	}
	run := s.storeHost
	if run == nil {
		run = s.power.HostOutputInput
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	_, err := run(ctx, cmd, in)
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(storeAnsi.ReplaceAllString(err.Error(), ""))
	switch {
	case errors.Is(err, hostcmd.ErrOldScript):
		msg = "o script do servidor é antigo e ainda não conhece a Loja: abra as atualizações (ao lado do sino) e atualize o script"
	case errors.Is(err, context.DeadlineExceeded):
		msg = "o servidor demorou demais para responder; o Runtipi pode ainda estar terminando: confira daqui a pouco"
	case len(msg) > 400:
		msg = msg[len(msg)-400:]
	}
	return errors.New(msg)
}

// panelKey guarda, junto dos apps, se o painel do Runtipi aceita abrir dentro do BrunOS (a tela de instalar).
const panelKey = "@painel-runtipi"

func (s *server) hStore(w http.ResponseWriter, r *http.Request) {
	sc := s.updates.Snapshot().Script
	frame := ""
	if _, info := s.tipi.Apps(); info.Found && info.InternalIP != "" {
		if now := time.Now(); s.frames.Need(panelKey, "", now) {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			s.frames.Set(panelKey, "", framecheck.Check(ctx, framecheck.Client, info.InternalIP, info.Port), now)
			cancel()
		}
		frame = s.frames.Get(panelKey)
	}
	writeJSON(w, 200, map[string]any{"catalog": s.tipi.Catalog(), "script": map[string]int{"have": sc.Have, "want": hostcmd.ScriptVersion},
		"self": selfApp, "panelFrame": frame})
}

func (s *server) hStoreApp(w http.ResponseWriter, r *http.Request) {
	d, ok := s.tipi.CatalogDetail(r.URL.Query().Get("urn"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "app não encontrado na loja"})
		return
	}
	writeJSON(w, 200, d)
}

func (s *server) hStoreLogo(w http.ResponseWriter, r *http.Request) {
	p, ok := s.tipi.CatalogLogo(r.URL.Query().Get("urn"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, p)
}

// hStoreAppAction: {"urn": "immich:migrated", "action": "start"|"stop"|"uninstall"}.
func (s *server) hStoreAppAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URN      string `json:"urn"`
		Action   string `json:"action"`
		Password string `json:"password"` // Senha de formatação (só no desinstalar); o servidor é quem confere
	}
	if err := decode(r, &req, 4<<10); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	cmds := map[string]string{"start": hostcmd.CmdAppStart, "stop": hostcmd.CmdAppStop, "uninstall": hostcmd.CmdAppUninstall}
	cmd, ok := cmds[req.Action]
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "ação inválida"})
		return
	}
	a, ok := s.appByURN(req.URN)
	if !ok || !hostcmd.ValidApp(req.URN) {
		writeJSON(w, 404, map[string]string{"error": "esse app não está instalado"})
		return
	}
	if a.ID == selfApp && req.Action != "start" {
		writeJSON(w, 400, map[string]string{"error": "este é o próprio BrunOS: pare ou desinstale pelo painel do Runtipi"})
		return
	}
	// desinstalar apaga os dados do app: pede a Senha de formatação, que o script do servidor confere
	// (com trava depois de 5 erros). O BrunOS só repassa pela entrada padrão; não grava nem registra.
	var in []byte
	if req.Action == "uninstall" {
		if req.Password == "" || len(req.Password) > 200 || strings.ContainsAny(req.Password, "\r\n") {
			writeJSON(w, 400, map[string]string{"error": "digite a Senha de formatação"})
			return
		}
		in = []byte(req.Password + "\n")
	}
	if !s.storeMu.TryLock() {
		writeJSON(w, 409, map[string]string{"error": "a Loja já está fazendo outra coisa no servidor: espere terminar"})
		return
	}
	defer s.storeMu.Unlock()
	if err := s.storeAct(r.Context(), cmd+" "+req.URN, in); err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	s.tipi.Invalidate()
	if req.Action == "uninstall" {
		s.events.Emit(events.Event{Kind: "store", Level: events.Info, Title: a.Name + " desinstalado", Body: "Desinstalado pela Loja do BrunOS, pelo Runtipi."})
		go s.updates.CheckPart(context.WithoutCancel(r.Context()), updates.PartApps) // some da lista de atualizações
	}
	writeJSON(w, 200, map[string]any{"ok": true, "catalog": s.tipi.Catalog()})
}

// hStoreStores: {"action": "add", "name": "minha-loja", "url": "https://github.com/usuario/loja"},
// {"action": "remove", "name": "minha-loja"} ou {"action": "refresh"} (baixa a lista nova de todas).
func (s *server) hStoreStores(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		Name   string `json:"name"`
		URL    string `json:"url"`
	}
	if err := decode(r, &req, 4<<10); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	name := strings.TrimSpace(req.Name)
	u := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(req.URL), "/"), ".git")
	cat := s.tipi.Catalog()
	var cmd, title string
	switch req.Action {
	case "refresh":
	case "add":
		if !hostcmd.ValidStoreName(name) {
			writeJSON(w, 400, map[string]string{"error": "nome inválido: use só letras minúsculas, números e traço (ex.: minha-loja)"})
			return
		}
		if !hostcmd.ValidStoreURL(u) {
			writeJSON(w, 400, map[string]string{"error": "endereço inválido: use o endereço https do repositório (ex.: https://github.com/usuario/loja)"})
			return
		}
		for _, st := range cat.Stores {
			if st.Slug == name || (st.URL != "" && strings.EqualFold(st.URL, u)) {
				writeJSON(w, 409, map[string]string{"error": "essa loja já está adicionada (" + st.Slug + ")"})
				return
			}
		}
		cmd, title = hostcmd.CmdStoreAdd+" "+name+" "+u, "Loja "+name+" adicionada"
	case "remove":
		if !hostcmd.ValidStoreName(name) {
			writeJSON(w, 400, map[string]string{"error": "nome de loja inválido"})
			return
		}
		for _, a := range cat.Apps {
			if a.Installed && a.Store == name {
				writeJSON(w, 409, map[string]string{"error": "essa loja tem apps instalados (" + a.Name + "…): desinstale antes de remover a loja"})
				return
			}
		}
		cmd, title = hostcmd.CmdStoreRemove+" "+name, "Loja "+name+" removida"
	default:
		writeJSON(w, 400, map[string]string{"error": "ação inválida"})
		return
	}
	if !s.storeMu.TryLock() {
		writeJSON(w, 409, map[string]string{"error": "a Loja já está fazendo outra coisa no servidor: espere terminar"})
		return
	}
	defer s.storeMu.Unlock()
	if cmd != "" {
		if err := s.storeAct(r.Context(), cmd, nil); err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		s.events.Emit(events.Event{Kind: "store", Level: events.Info, Title: title, Body: "Feito pela Loja do BrunOS, pelo Runtipi."})
	}
	// baixa a lista nova das lojas (o mesmo "Conferir" dos apps) e relê o catálogo
	_ = s.updates.CheckPart(r.Context(), updates.PartApps)
	s.tipi.Invalidate()
	out := map[string]any{"ok": true, "catalog": s.tipi.Catalog()}
	if e := s.updates.Snapshot().StoresErr; e != "" && cmd == "" {
		out["warn"] = e
	}
	writeJSON(w, 200, out)
}
