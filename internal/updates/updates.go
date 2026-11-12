// Package updates confere as atualizações dos apps do Runtipi e do Linux do servidor (A1) na
// frequência escolhida (6 h, 12 h, diária, semanal ou só quando o Bruno manda) e, desde a 0.12.0,
// atualiza pela interface (decisão 47).
//
//   - Apps: runtipi.CheckUpdates (cópia instalada × loja baixada, pelo tipi_version).
//   - Linux: pedido "atualizacoes" ao script do servidor (apt-check, reboot-required e a lista).
//   - Atualizar: pedidos "atualizar-linux" e "atualizar-app <app>:<loja>" ao script, que roda a
//     atualização solta no servidor (systemd-run) e registra o andamento; o BrunOS lê o registro
//     ("atualizacao-estado") a cada poucos segundos e retoma depois de reiniciar (a atualização do
//     Docker ou do próprio BrunOS reinicia o contêiner no meio).
//
// Avisos no sino uma vez por mudança: versão nova de um app (desligável), reinício necessário,
// atualizações de segurança e o fim de cada atualização. Estado em /data/updates.json.
package updates

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"brunos/internal/events"
	"brunos/internal/hostcmd"
	"brunos/internal/runtipi"
	"brunos/internal/store"
)

// Frequências aceitas (Configurações → Sistema → Procurar atualizações).
var everyOpts = map[string]time.Duration{"6h": 6 * time.Hour, "12h": 12 * time.Hour, "24h": 24 * time.Hour, "168h": 7 * 24 * time.Hour, "manual": 0}

const defaultEvery = "24h"

// Tempos do acompanhamento (variáveis só para os testes encurtarem).
var (
	pollEvery  = 3 * time.Second  // leitura do andamento
	startGrace = 45 * time.Second // parado e sem registro novo depois disso = não começou
	maxJob     = 3 * time.Hour    // desiste de acompanhar
)

// OS é o que se sabe das atualizações do Linux.
type OS struct {
	Total     int      `json:"total"`
	Security  int      `json:"security"`
	Reboot    bool     `json:"reboot"`
	Packages  []string `json:"packages"`
	CheckedAt int64    `json:"checkedAt"`
	Err       string   `json:"err,omitempty"`
}

// Job é uma atualização pedida pela interface.
type Job struct {
	Kind       string   `json:"kind"`             // linux | app
	Target     string   `json:"target,omitempty"` // urn do app ("immich:migrated")
	Name       string   `json:"name"`             // "Linux" ou o nome do app
	From       string   `json:"from,omitempty"`
	To         string   `json:"to,omitempty"`
	StartedAt  int64    `json:"startedAt"`
	Running    bool     `json:"running"`
	Lines      []string `json:"lines"`
	FinishedAt int64    `json:"finishedAt,omitempty"`
	OK         bool     `json:"ok"`
	Err        string   `json:"err,omitempty"`
}

type state struct {
	Version   int              `json:"v"`
	Apps      []runtipi.Update `json:"apps"`
	OS        OS               `json:"os"`
	CheckedAt int64            `json:"checkedAt"`
	Every     string           `json:"every,omitempty"`
	Job       *Job             `json:"job,omitempty"`
}

// Checker faz as conferências e acompanha as atualizações.
type Checker struct {
	File       string
	Apps       func() []runtipi.Update
	Query      func(ctx context.Context, cmd string) ([]byte, error) // script do servidor (leitura)
	Act        func(ctx context.Context, cmd string) ([]byte, error) // script do servidor (atualizar)
	Events     *events.Store
	NotifyApps func() bool // aviso de versão nova de app (Configurações → Apps)
	Now        func() time.Time

	mu        sync.Mutex
	st        state
	base      context.Context
	running   bool
	following bool
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
	if _, ok := everyOpts[c.st.Every]; !ok {
		c.st.Every = defaultEvery
	}
	return c
}

// Run confere ao ligar (se já passou o intervalo) e depois de hora em hora vê se está na hora.
// Retoma o acompanhamento de uma atualização que estava rodando quando o BrunOS parou.
func (c *Checker) Run(ctx context.Context) {
	c.mu.Lock()
	c.base = ctx
	resume := c.st.Job != nil && c.st.Job.Running
	c.mu.Unlock()
	if resume {
		c.follow()
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(45 * time.Second):
	}
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		c.mu.Lock()
		every := everyOpts[c.st.Every]
		due := every > 0 && c.Now().Sub(time.Unix(c.st.CheckedAt, 0)) >= every
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

func (c *Checker) bg() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.base != nil {
		return c.base
	}
	return context.Background()
}

// SetEvery muda a frequência ("6h", "12h", "24h", "168h" ou "manual").
func (c *Checker) SetEvery(v string) error {
	if _, ok := everyOpts[v]; !ok {
		return errors.New("frequência inválida")
	}
	c.mu.Lock()
	c.st.Every = v
	c.mu.Unlock()
	c.save()
	return nil
}

// Check confere agora ("Conferir agora", o botão ao lado do sino e a rotina).
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
		out, err := c.Query(qctx, hostcmd.CmdUpdates)
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
	c.mu.Unlock()
	c.notify(prev, apps, osu)
	c.save()
}

func (c *Checker) save() {
	if c.File == "" {
		return
	}
	c.mu.Lock()
	st := c.st
	if st.Job != nil {
		j := *st.Job
		st.Job = &j
	}
	c.mu.Unlock()
	if err := store.WriteJSON(c.File, st, 0o600); err != nil {
		slog.Warn("atualizações: não deu para gravar", "erro", err)
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
				Body:  fmt.Sprintf("Instalada: %s. Atualize pelo botão de atualizações (ao lado do sino) ou pela página do app no Runtipi.", a.Installed)})
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
			Body:  fmt.Sprintf("%d atualizações no total. Atualize pelo botão de atualizações, ao lado do sino.", osu.Total)})
	}
}

func friendly(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "script do servidor é antigo") || strings.Contains(msg, "pedido inválido"):
		return "o script do servidor é antigo: em Configurações → Energia, copie os comandos de atualização e cole no servidor"
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "deadline exceeded"):
		return "o servidor demorou demais para responder: tente de novo daqui a pouco"
	}
	return msg
}

// ── atualizar pela interface ──────────────────────────────────────

var (
	ErrBusy     = errors.New("já tem uma atualização rodando: espere ela terminar")
	ErrNoUpdate = errors.New("não há atualização para esse app: confira de novo")
	ErrKind     = errors.New("tipo de atualização inválido")
)

// Start pede ao servidor para atualizar o Linux (kind "linux") ou um app (kind "app", urn).
func (c *Checker) Start(ctx context.Context, kind, urn string) error {
	if c.Act == nil {
		return errors.New("atualizar pela interface não está disponível")
	}
	job := &Job{Kind: kind, Lines: []string{}}
	var cmd string
	c.mu.Lock()
	if c.st.Job != nil && c.st.Job.Running {
		c.mu.Unlock()
		return ErrBusy
	}
	switch kind {
	case "linux":
		cmd, job.Name = hostcmd.CmdUpdateOS, "Linux"
	case "app":
		var found *runtipi.Update
		for i := range c.st.Apps {
			if c.st.Apps[i].URN == urn {
				found = &c.st.Apps[i]
			}
		}
		if found == nil || !hostcmd.ValidApp(urn) {
			c.mu.Unlock()
			return ErrNoUpdate
		}
		cmd = hostcmd.CmdUpdateApp + " " + urn
		job.Target, job.Name, job.From, job.To = urn, found.Name, found.Installed, found.Available
	default:
		c.mu.Unlock()
		return ErrKind
	}
	c.mu.Unlock()
	actx, cancel := context.WithTimeout(ctx, 30*time.Second)
	_, err := c.Act(actx, cmd)
	cancel()
	if err != nil {
		if strings.Contains(err.Error(), "já tem uma atualização rodando") {
			return ErrBusy
		}
		return errors.New(friendly(err))
	}
	job.StartedAt, job.Running = c.Now().Unix(), true
	c.mu.Lock()
	c.st.Job = job
	c.mu.Unlock()
	c.save()
	c.follow()
	return nil
}

// follow acompanha a atualização em segundo plano até terminar (uma goroutine só).
func (c *Checker) follow() {
	c.mu.Lock()
	if c.following {
		c.mu.Unlock()
		return
	}
	c.following = true
	c.mu.Unlock()
	go func() {
		ctx := c.bg()
		t := time.NewTicker(pollEvery)
		defer t.Stop()
		for {
			if c.poll(ctx) {
				// só sai se nenhuma atualização nova começou enquanto esta fechava
				c.mu.Lock()
				if c.st.Job == nil || !c.st.Job.Running {
					c.following = false
					c.mu.Unlock()
					return
				}
				c.mu.Unlock()
			}
			select {
			case <-ctx.Done():
				c.mu.Lock()
				c.following = false
				c.mu.Unlock()
				return
			case <-t.C:
			}
		}
	}()
}

// poll lê o andamento uma vez. Devolve true quando a atualização terminou.
func (c *Checker) poll(ctx context.Context) bool {
	c.mu.Lock()
	j := c.st.Job
	if j == nil || !j.Running {
		c.mu.Unlock()
		return true
	}
	started := j.StartedAt
	c.mu.Unlock()
	now := c.Now()
	qctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	out, err := c.Act(qctx, hostcmd.CmdUpdateStatus)
	cancel()
	if err != nil { // BrunOS ou Docker reiniciando, rede: tenta de novo até o limite
		if now.Sub(time.Unix(started, 0)) > maxJob {
			c.finish(false, "não consegui acompanhar a atualização até o fim: confira no servidor (/var/log/brunos-atualizar.log)", nil)
			return true
		}
		return false
	}
	st := ParseStatus(out, started)
	switch {
	case st.Done:
		msg := ""
		if st.Exit != 0 {
			msg = fmt.Sprintf("a atualização terminou com erro (código %d)", st.Exit)
		}
		c.finish(st.Exit == 0, msg, st.Lines)
		return true
	case !st.Running && !st.Started && now.Sub(time.Unix(started, 0)) > startGrace:
		c.finish(false, "a atualização não começou no servidor", st.Lines)
		return true
	case !st.Running && st.Started:
		c.finish(false, "a atualização parou sem terminar (o servidor reiniciou?)", st.Lines)
		return true
	}
	c.mu.Lock()
	if st.Started {
		c.st.Job.Lines = st.Lines
	}
	c.mu.Unlock()
	return false
}

func (c *Checker) finish(ok bool, msg string, lines []string) {
	c.mu.Lock()
	j := c.st.Job
	if j == nil {
		c.mu.Unlock()
		return
	}
	j.Running, j.OK, j.Err, j.FinishedAt = false, ok, msg, c.Now().Unix()
	if lines != nil {
		j.Lines = lines
	}
	done := *j
	c.mu.Unlock()
	c.save()
	if c.Events != nil {
		ev := events.Event{Kind: "update-done", Level: events.Info, Key: fmt.Sprintf("update-done:%d", done.StartedAt)}
		switch {
		case ok && done.Kind == "linux":
			ev.Title, ev.Body = "Linux atualizado", "As atualizações do Linux foram instaladas. Se aparecer \"reinício necessário\", reinicie o servidor quando puder."
		case ok:
			ev.Title, ev.Body = done.Name+" atualizado", fmt.Sprintf("%s foi atualizado de %s para %s.", done.Name, done.From, done.To)
		default:
			ev.Level, ev.Title = events.Aviso, "Não deu para atualizar: "+done.Name
			ev.Body = msg
			if n := len(done.Lines); n > 0 {
				ev.Body += ". Última linha: " + done.Lines[n-1]
			}
		}
		c.Events.Emit(ev)
	}
	c.Check(c.bg()) // a lista de atualizações muda depois de atualizar
}

// Status é a leitura do pedido "atualizacao-estado".
type Status struct {
	Running bool     // o serviço brunos-atualizar está ativo
	Started bool     // o registro é desta atualização (começou depois de pedida)
	Done    bool     // o registro tem a linha "fim"
	Exit    int      // código de saída
	Lines   []string // últimas linhas do registro, limpas
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// ParseStatus lê "rodando|parado", "=== registro" e o registro ("inicio <ts> ...", saída, "fim <n>").
// Registro de uma atualização anterior (início antes do pedido) não conta.
func ParseStatus(out []byte, startedAt int64) Status {
	var s Status
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	inLog := false
	for sc.Scan() {
		ln := sc.Text()
		if !inLog {
			switch strings.TrimSpace(ln) {
			case "rodando":
				s.Running = true
			case "=== registro":
				inLog = true
			}
			continue
		}
		if i := strings.LastIndexByte(ln, '\r'); i >= 0 { // barras de progresso: fica o último pedaço
			ln = ln[i+1:]
		}
		ln = strings.TrimSpace(ansiRe.ReplaceAllString(ln, ""))
		if ln == "" {
			continue
		}
		if f := strings.Fields(ln); f[0] == "inicio" && len(f) >= 2 {
			ts, _ := strconv.ParseInt(f[1], 10, 64)
			s.Started, s.Done, s.Lines = ts >= startedAt-60, false, nil
			continue
		} else if f[0] == "fim" && len(f) == 2 && s.Started {
			s.Done = true
			s.Exit, _ = strconv.Atoi(f[1])
			continue
		}
		if len(ln) > 200 {
			ln = ln[:200] + "…"
		}
		s.Lines = append(s.Lines, ln)
	}
	if !s.Started {
		s.Lines = nil
	}
	if len(s.Lines) > 15 {
		s.Lines = s.Lines[len(s.Lines)-15:]
	}
	if s.Lines == nil {
		s.Lines = []string{}
	}
	return s
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
	Every     string           `json:"every"`
	Checking  bool             `json:"checking,omitempty"`
	Job       *Job             `json:"job,omitempty"`
}

func (c *Checker) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Snapshot{Apps: append([]runtipi.Update{}, c.st.Apps...), OS: c.st.OS, CheckedAt: c.st.CheckedAt, Every: c.st.Every, Checking: c.running}
	s.OS.Packages = append([]string{}, c.st.OS.Packages...)
	if c.st.Job != nil {
		j := *c.st.Job
		j.Lines = append([]string{}, c.st.Job.Lines...)
		s.Job = &j
	}
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
