// Package updates confere, uma vez por dia, as atualizações dos apps do Runtipi e do Linux do
// servidor (A1, decisão 44). Só avisa: o BrunOS não atualiza nada sozinho.
//
//   - Apps: runtipi.CheckUpdates (cópia instalada × loja baixada, pelo tipi_version).
//   - Linux: pedido "atualizacoes" ao script do servidor (apt-check, reboot-required e a lista).
//
// Avisos no sino uma vez por mudança: versão nova de um app (desligável), reinício necessário e
// atualizações de segurança. Estado em /data/updates.json.
package updates

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"brunos/internal/events"
	"brunos/internal/runtipi"
	"brunos/internal/store"
)

const every = 24 * time.Hour

// OS é o que se sabe das atualizações do Linux.
type OS struct {
	Total     int      `json:"total"`
	Security  int      `json:"security"`
	Reboot    bool     `json:"reboot"`
	Packages  []string `json:"packages"`
	CheckedAt int64    `json:"checkedAt"`
	Err       string   `json:"err,omitempty"`
}

type state struct {
	Version   int              `json:"v"`
	Apps      []runtipi.Update `json:"apps"`
	OS        OS               `json:"os"`
	CheckedAt int64            `json:"checkedAt"`
}

// Checker faz as conferências.
type Checker struct {
	File       string
	Apps       func() []runtipi.Update
	Query      func(ctx context.Context, cmd string) ([]byte, error) // script do servidor
	Events     *events.Store
	NotifyApps func() bool // aviso de versão nova de app (Configurações → Apps)
	Now        func() time.Time

	mu      sync.Mutex
	st      state
	running bool
}

func New(file string) *Checker {
	c := &Checker{File: file, Now: time.Now, st: state{Version: 1, Apps: []runtipi.Update{}}}
	if file != "" {
		if _, err := store.ReadJSON(file, &c.st); err != nil {
			slog.Warn("atualizações: estado ilegível", "erro", err)
		}
	}
	if c.st.Apps == nil {
		c.st.Apps = []runtipi.Update{}
	}
	return c
}

// Run confere ao ligar (se a última foi há mais de um dia) e depois uma vez por dia.
func (c *Checker) Run(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(45 * time.Second):
	}
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		c.mu.Lock()
		due := c.Now().Sub(time.Unix(c.st.CheckedAt, 0)) >= every
		c.mu.Unlock()
		if due {
			c.Check(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Check confere agora ("Conferir agora" e a rotina diária).
func (c *Checker) Check(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.running = false; c.mu.Unlock() }()
	now := c.Now()
	var apps []runtipi.Update
	if c.Apps != nil {
		apps = c.Apps()
	}
	if apps == nil {
		apps = []runtipi.Update{}
	}
	osu := OS{CheckedAt: now.Unix(), Packages: []string{}}
	if c.Query != nil {
		qctx, cancel := context.WithTimeout(ctx, time.Minute)
		out, err := c.Query(qctx, "atualizacoes")
		cancel()
		if err != nil {
			osu.Err = friendly(err)
		} else {
			osu = ParseOS(out)
			osu.CheckedAt = now.Unix()
		}
	}
	c.mu.Lock()
	prev := c.st
	c.st.Apps, c.st.OS, c.st.CheckedAt = apps, osu, now.Unix()
	st := c.st
	c.mu.Unlock()
	c.notify(prev, apps, osu)
	if c.File != "" {
		if err := store.WriteJSON(c.File, st, 0o600); err != nil {
			slog.Warn("atualizações: não deu para gravar", "erro", err)
		}
	}
}

func (c *Checker) notify(prev state, apps []runtipi.Update, osu OS) {
	if c.Events == nil {
		return
	}
	if c.NotifyApps == nil || c.NotifyApps() {
		for _, a := range apps {
			c.Events.Emit(events.Event{Kind: "app-update", Level: "info", Key: "app-update:" + a.URN + ":" + a.Available,
				Title: fmt.Sprintf("%s: atualização para %s", a.Name, a.Available),
				Body:  fmt.Sprintf("Instalada: %s. Atualize pela página do app no Runtipi.", a.Installed)})
		}
	}
	if osu.Err != "" {
		return // sem leitura do Linux: não mexe nos avisos dele
	}
	if osu.Reboot {
		c.Events.Emit(events.Event{Kind: "os-reboot", Level: "aviso", Key: "os-reboot",
			Title: "O servidor precisa reiniciar", Body: "Uma atualização do Linux (normalmente do kernel) só vale depois de reiniciar o servidor."})
	} else if prev.OS.Reboot {
		c.Events.Resolve("os-reboot", nil)
	}
	if osu.Security > 0 {
		c.Events.Emit(events.Event{Kind: "os-security", Level: "info", Key: fmt.Sprintf("os-security:%d:%d", osu.Security, osu.Total),
			Title: fmt.Sprintf("%d atualizações de segurança do Linux", osu.Security),
			Body:  fmt.Sprintf("%d atualizações no total. Em Configurações → Sistema, \"Atualizar no Terminal\".", osu.Total)})
	}
}

func friendly(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "script do servidor é antigo") || strings.Contains(msg, "pedido inválido") {
		return "o script do servidor é antigo: em Configurações → Energia, copie os comandos de atualização e cole no servidor"
	}
	return msg
}

// ParseOS lê a saída do pedido "atualizacoes": "apt N;M", "reiniciar" e a lista do apt.
func ParseOS(out []byte) OS {
	o := OS{Packages: []string{}}
	sc := bufio.NewScanner(bytes.NewReader(out))
	pkgs := false
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		switch {
		case ln == "":
		case strings.HasPrefix(ln, "apt "):
			a, b, ok := strings.Cut(strings.TrimPrefix(ln, "apt "), ";")
			if ok {
				o.Total, _ = strconv.Atoi(strings.TrimSpace(a))
				o.Security, _ = strconv.Atoi(strings.TrimSpace(b))
			}
		case ln == "reiniciar":
			o.Reboot = true
		case ln == "=== pacotes":
			pkgs = true
		case pkgs:
			name, _, _ := strings.Cut(ln, "/")
			if name != "" && !strings.Contains(name, " ") && len(o.Packages) < 300 {
				o.Packages = append(o.Packages, name)
			}
		}
	}
	if o.Total == 0 && len(o.Packages) > 0 { // sem apt-check: conta pela lista
		o.Total = len(o.Packages)
	}
	return o
}

// Snapshot é o que a interface recebe.
type Snapshot struct {
	Apps      []runtipi.Update `json:"apps"`
	OS        OS               `json:"os"`
	CheckedAt int64            `json:"checkedAt"`
}

func (c *Checker) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Snapshot{Apps: append([]runtipi.Update{}, c.st.Apps...), OS: c.st.OS, CheckedAt: c.st.CheckedAt}
	s.OS.Packages = append([]string{}, c.st.OS.Packages...)
	return s
}

// For devolve a atualização de um app (urn), se houver.
func (c *Checker) For(urn string) (runtipi.Update, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, a := range c.st.Apps {
		if a.URN == urn {
			return a, true
		}
	}
	return runtipi.Update{}, false
}
