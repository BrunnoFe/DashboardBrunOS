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
	Backup     bool     `json:"backup,omitempty"` // backup do Runtipi antes de atualizar o app
	Phase      string   `json:"phase,omitempty"`  // backup | atualizando
	Warn       string   `json:"warn,omitempty"`   // terminou bem, mas com aviso (repositório quebrado)
	Seen       bool     `json:"seen,omitempty"`   // já foi vista rodando no servidor (o registro é dela)
}

// Script é a versão do script do servidor (brunos-host) e se ele se atualiza sozinho.
type Script struct {
	Have      int    `json:"have"`           // versão no servidor (0 = não deu para ler)
	Want      int    `json:"want"`           // versão que este BrunOS traz
	Auto      bool   `json:"auto"`           // esta compilação traz o script assinado
	CheckedAt int64  `json:"checkedAt"`      // última conferência
	Err       string `json:"err,omitempty"`  // não deu para conferir ou para atualizar
	At        int64  `json:"at,omitempty"`   // última atualização automática
	From      int    `json:"from,omitempty"` // de qual versão veio
}

type state struct {
	Version   int              `json:"v"`
	Apps      []runtipi.Update `json:"apps"`
	OS        OS               `json:"os"`
	CheckedAt int64            `json:"checkedAt"`
	Every     string           `json:"every,omitempty"`
	Job       *Job             `json:"job,omitempty"`
	Script    Script           `json:"script"`
}

// Checker faz as conferências e acompanha as atualizações.
type Checker struct {
	File  string
	Apps  func() []runtipi.Update
	Query func(ctx context.Context, cmd string) ([]byte, error) // script do servidor (leitura)
	Act   func(ctx context.Context, cmd string) ([]byte, error) // script do servidor (atualizar)
	// UpgradeScript troca o script do servidor pelo desta versão (assinado). nil = sem assinatura.
	UpgradeScript func(ctx context.Context) error
	Events        *events.Store
	NotifyApps    func() bool // aviso de versão nova de app (Configurações → Apps)
	Now           func() time.Time

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
	c.st.Script.Want = hostcmd.ScriptVersion
	return c
}

// CheckScript lê a versão do script do servidor e, se ele já souber se atualizar (7+) e esta
// compilação trouxer o script assinado, troca pelo novo sozinho. Avisa no sino quando troca e
// quando não consegue.
func (c *Checker) CheckScript(ctx context.Context) {
	if c.Query == nil {
		return
	}
	qctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	out, err := c.Query(qctx, hostcmd.CmdVersion)
	cancel()
	now := c.Now().Unix()
	c.mu.Lock()
	sc := &c.st.Script
	sc.Want, sc.Auto, sc.CheckedAt, sc.Err = hostcmd.ScriptVersion, c.UpgradeScript != nil, now, ""
	if err != nil {
		sc.Have, sc.Err = 0, friendly(err)
		c.mu.Unlock()
		c.save()
		return
	}
	have := hostcmd.ParseVersion(out)
	sc.Have = have
	auto := have >= hostcmd.SelfUpdateFrom && have < hostcmd.ScriptVersion && c.UpgradeScript != nil
	c.mu.Unlock()
	if auto {
		uctx, cancel := context.WithTimeout(ctx, time.Minute)
		uerr := c.UpgradeScript(uctx)
		cancel()
		c.mu.Lock()
		if uerr != nil {
			sc.Err = "não deu para atualizar o script do servidor sozinho: " + friendly(uerr)
		} else {
			sc.From, sc.Have, sc.At = have, hostcmd.ScriptVersion, now
		}
		c.mu.Unlock()
		if c.Events != nil {
			if uerr != nil {
				c.Events.Emit(events.Event{Kind: "script-upgrade", Level: events.Aviso, Key: fmt.Sprintf("script-upgrade-fail:%d", hostcmd.ScriptVersion),
					Title: "Não deu para atualizar o script do servidor", Body: friendly(uerr) + ". No botão de atualizações (ao lado do sino) dá para atualizar pelo Terminal."})
			} else {
				c.Events.Emit(events.Event{Kind: "script-upgrade", Level: events.Info, Key: fmt.Sprintf("script-upgrade:%d", hostcmd.ScriptVersion),
					Title: fmt.Sprintf("Script do servidor atualizado (versão %d)", hostcmd.ScriptVersion),
					Body:  fmt.Sprintf("O BrunOS trocou sozinho o script do servidor da versão %d para a %d. A troca só acontece com um script assinado pela publicação do BrunOS.", have, hostcmd.ScriptVersion)})
			}
		}
	}
	c.save()
}

// BackupSize mede o backup que o Runtipi faria do app (pasta do app + app-data) e o espaço livre.
func (c *Checker) BackupSize(ctx context.Context, urn string) (size, free int64, err error) {
	if c.Query == nil || !hostcmd.ValidApp(urn) {
		return 0, 0, ErrNoUpdate
	}
	qctx, cancel := context.WithTimeout(ctx, 70*time.Second)
	out, err := c.Query(qctx, hostcmd.CmdBackupSize+" "+urn)
	cancel()
	if err != nil {
		return 0, 0, errors.New(friendly(err))
	}
	f := strings.Fields(string(out))
	if len(f) != 4 || f[0] != "tamanho" || f[2] != "livre" {
		return 0, 0, errors.New("resposta inesperada do servidor")
	}
	size, _ = strconv.ParseInt(f[1], 10, 64)
	free, _ = strconv.ParseInt(f[3], 10, 64)
	return size, free, nil
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
	c.CheckScript(ctx) // ao ligar: script do servidor desatualizado é trocado sozinho (7+)
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
	c.CheckScript(ctx)
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

// Start pede ao servidor para atualizar o Linux (kind "linux") ou um app (kind "app", urn; backup =
// o backup do Runtipi antes, e só atualiza se ele der certo).
func (c *Checker) Start(ctx context.Context, kind, urn string, backup bool) error {
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
		flag := "nao"
		if backup {
			flag = "sim"
		}
		cmd = hostcmd.CmdUpdateApp + " " + urn + " " + flag
		job.Target, job.Name, job.From, job.To, job.Backup = urn, found.Name, found.Installed, found.Available, backup
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
	started, seen := j.StartedAt, j.Seen
	c.mu.Unlock()
	now := c.Now()
	qctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	out, err := c.Act(qctx, hostcmd.CmdUpdateStatus)
	cancel()
	if err != nil { // BrunOS ou Docker reiniciando, rede: tenta de novo até o limite
		if now.Sub(time.Unix(started, 0)) > maxJob {
			c.finish(false, "não consegui acompanhar a atualização até o fim: confira no servidor (/var/log/brunos-atualizar.log)", "", nil)
			return true
		}
		return false
	}
	st := ParseStatus(out, started, seen)
	if st.Running && !seen { // viu rodando: o registro é desta atualização até o fim (grava para retomar)
		c.mu.Lock()
		c.st.Job.Seen = true
		c.mu.Unlock()
		c.save()
	}
	switch {
	case st.Done:
		msg, warn := "", ""
		switch {
		case st.BackupFailed:
			msg = "o backup do Runtipi falhou, então nada foi atualizado"
		case st.Exit != 0:
			msg = exitMessage(st.Exit, st.Lines)
		case st.Warn:
			warn = "um repositório extra do apt está com problema"
			if st.Repo != "" {
				warn += " (" + st.Repo + ")"
			}
			warn += "; o resto foi instalado"
		}
		c.finish(st.Exit == 0 && !st.BackupFailed, msg, warn, st.Lines)
		return true
	case !st.Running && !st.Started && now.Sub(time.Unix(started, 0)) > startGrace:
		c.finish(false, "a atualização não começou no servidor", "", st.Lines)
		return true
	case !st.Running && st.Started:
		c.finish(false, "a atualização parou sem terminar (o servidor reiniciou?)", "", st.Lines)
		return true
	}
	c.mu.Lock()
	if st.Started {
		c.st.Job.Lines, c.st.Job.Phase = st.Lines, st.Phase
	}
	c.mu.Unlock()
	return false
}

// exitMessage explica em português os erros mais comuns do apt e do runtipi-cli.
func exitMessage(code int, lines []string) string {
	all := strings.Join(lines, "\n")
	switch {
	case strings.Contains(all, "Could not get lock") || strings.Contains(all, "is held by process"):
		return "outra instalação está rodando no servidor (por exemplo, a atualização automática do Ubuntu): tente de novo daqui a alguns minutos"
	case strings.Contains(all, "No space left on device"):
		return "faltou espaço no disco do servidor"
	case strings.Contains(all, "Temporary failure resolving") || strings.Contains(all, "Could not resolve"):
		return "o servidor ficou sem internet durante a atualização"
	case strings.Contains(all, "dpkg was interrupted"):
		return "uma instalação anterior ficou pela metade: rode sudo dpkg --configure -a no Terminal"
	}
	return fmt.Sprintf("a atualização terminou com erro (código %d): veja as últimas linhas", code)
}

func (c *Checker) finish(ok bool, msg, warn string, lines []string) {
	c.mu.Lock()
	j := c.st.Job
	if j == nil {
		c.mu.Unlock()
		return
	}
	j.Running, j.OK, j.Err, j.Warn, j.Phase, j.FinishedAt = false, ok, msg, warn, "", c.Now().Unix()
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
			if done.Warn != "" {
				ev.Body += " Aviso: " + done.Warn + "."
			}
		case ok:
			ev.Title, ev.Body = done.Name+" atualizado", fmt.Sprintf("%s foi atualizado de %s para %s.", done.Name, done.From, done.To)
			if done.Backup {
				ev.Body += " Antes, o Runtipi fez um backup do app (restaurável pela página dele no Runtipi)."
			}
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
	Running      bool     // o serviço brunos-atualizar está ativo
	Started      bool     // o registro é desta atualização (começou depois de pedida)
	Done         bool     // o registro tem a linha "fim"
	Exit         int      // código de saída
	Lines        []string // últimas linhas do registro, limpas
	Phase        string   // backup | atualizando ("" = sem backup)
	BackupFailed bool     // "backup-falhou": nada foi atualizado
	Warn         bool     // "aviso-repositorio": instalou, mas um repositório falhou no apt update
	Repo         string   // o repositório que falhou (da linha "E: The repository '...'")
}

var repoRe = regexp.MustCompile(`The repository '([^']+)'`)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// ParseStatus lê "rodando|parado", "=== registro" e o registro ("inicio <ts> ...", saída, "fim <n>").
// Registro de uma atualização anterior (início antes do pedido) não conta. O registro é reescrito a
// cada atualização, então se ela já foi vista rodando (seen) ou está rodando agora, ele é desta, mesmo
// que a linha "inicio" não apareça (o script 7 só mandava as últimas 30 linhas).
func ParseStatus(out []byte, startedAt int64, seen bool) Status {
	s := Status{Started: seen}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	inLog := false
	for sc.Scan() {
		ln := sc.Text()
		if !inLog {
			switch strings.TrimSpace(ln) {
			case "rodando":
				s.Running, s.Started = true, true
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
			s.Started, s.Done, s.Lines = ts >= startedAt-60 || s.Running || seen, false, nil
			s.Phase, s.BackupFailed, s.Warn, s.Repo = "", false, false, ""
			continue
		} else if f[0] == "fim" && len(f) == 2 && s.Started {
			s.Done = true
			s.Exit, _ = strconv.Atoi(f[1])
			continue
		}
		switch {
		case ln == "backup-inicio":
			s.Phase = "backup"
			continue
		case ln == "backup-ok":
			s.Phase = "atualizando"
			continue
		case strings.HasPrefix(ln, "backup-falhou"):
			s.BackupFailed = true
			continue
		case ln == "aviso-repositorio":
			s.Warn = true
			continue
		}
		if m := repoRe.FindStringSubmatch(ln); m != nil && s.Repo == "" {
			s.Repo = m[1]
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
	Script    Script           `json:"script"`
}

func (c *Checker) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Snapshot{Apps: append([]runtipi.Update{}, c.st.Apps...), OS: c.st.OS, CheckedAt: c.st.CheckedAt, Every: c.st.Every, Checking: c.running, Script: c.st.Script}
	s.Script.Want, s.Script.Auto = hostcmd.ScriptVersion, c.UpgradeScript != nil
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
