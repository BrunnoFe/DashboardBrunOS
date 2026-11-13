package disks

// Montar, desmontar, trocar o rótulo e Formatar (0.16.0, decisão 54). Tudo pelo script do servidor, que
// confere de novo; Formatar só com a Senha de formatação, conferida pelo servidor (o BrunOS só repassa
// pela entrada padrão e nunca guarda). O container do BrunOS não acompanha montagens novas (o Runtipi
// não deixa ligar a propagação): depois de montar ou desmontar, o BrunOS precisa reiniciar para ver o
// disco (ou soltá-lo); a interface oferece o botão.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"brunos/internal/events"
	"brunos/internal/hostcmd"
)

// Limites (variáveis para os testes encurtarem).
var (
	mountWait  = 60 * time.Second
	formatWait = 90 * time.Second // conferir a senha (scrypt) e tudo antes de começar
	followStep = 2 * time.Second
	followMax  = 30 * time.Minute
)

// FormatJob é a formatação pedida pelo BrunOS.
type FormatJob struct {
	Dev     string   `json:"dev"`
	Name    string   `json:"name"`
	FS      string   `json:"fs"`
	Label   string   `json:"label"`
	Started int64    `json:"started"`
	Running bool     `json:"running"`
	Step    string   `json:"step,omitempty"` // apagando, particionando, formatando
	OK      bool     `json:"ok"`
	Err     string   `json:"err,omitempty"`
	UUID    string   `json:"uuid,omitempty"`
	Ended   int64    `json:"ended,omitempty"`
	Lines   []string `json:"lines,omitempty"`
}

// FormatState: se a Senha de formatação existe, trava por erros e a formatação em andamento.
type FormatState struct {
	HasPassword bool       `json:"hasPassword"`
	LockedUntil int64      `json:"lockedUntil,omitempty"`
	Fails       int        `json:"fails,omitempty"`
	Job         *FormatJob `json:"job,omitempty"`
	Err         string     `json:"err,omitempty"`
}

type formatting struct {
	mu  sync.Mutex
	job *FormatJob
}

func (m *Monitor) partByUUID(uuid string) (Disk, Part, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.list {
		for _, p := range d.Parts {
			if p.UUID != "" && p.UUID == uuid {
				return d, p, true
			}
		}
	}
	return Disk{}, Part{}, false
}

func (m *Monitor) partByFolder(folder string) (Disk, Part, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.list {
		for _, p := range d.Parts {
			for _, mp := range p.Mounts {
				if mp == "/mnt/"+folder {
					return d, p, true
				}
			}
		}
	}
	return Disk{}, Part{}, false
}

// Mount monta uma partição em /mnt/<folder> (fica no fstab, marcada pelo BrunOS, com nofail).
func (m *Monitor) Mount(ctx context.Context, uuid, folder string) error {
	if !hostcmd.ValidUUID(uuid) {
		return errors.New("partição inválida")
	}
	if !hostcmd.ValidFolder(folder) {
		return errors.New("nome de pasta inválido: use só letras, números, - e _ (até 32)")
	}
	d, p, ok := m.partByUUID(uuid)
	switch {
	case !ok:
		return ErrNoDisk
	case d.System:
		return errors.New("essa partição é do disco do sistema")
	case len(p.Mounts) > 0:
		return errors.New("essa partição já está montada em " + strings.Join(p.Mounts, ", "))
	case p.FSType == "" || p.Holder != "":
		return errors.New("essa partição não tem um sistema de arquivos que dê para montar")
	}
	qctx, cancel := context.WithTimeout(ctx, mountWait)
	_, err := m.Act(qctx, hostcmd.CmdMount+" "+uuid+" "+folder)
	cancel()
	if err != nil {
		return errors.New(friendly(err))
	}
	name := p.Label
	if name == "" {
		name = p.Name
	}
	m.emit([]events.Event{{Kind: "disk-mounted-new", Level: events.Info, Title: "Disco montado: " + name,
		Body: "A partição " + p.Name + " foi montada em /mnt/" + folder + " e volta sozinha quando o servidor reiniciar. Reinicie o BrunOS para ver os arquivos dela."}})
	if m.OnMount != nil {
		m.OnMount(folder)
	}
	go func() { _ = m.Refresh(m.bg()) }()
	return nil
}

// UnmountResult: o que ainda segura o disco depois de desmontar no servidor.
type UnmountResult struct {
	Stuck     []string `json:"stuck"`     // processos (o BrunOS e apps com pasta deste disco) com cópia da montagem
	FstabKept bool     `json:"fstabKept"` // a linha do fstab não era do BrunOS e ficou
}

// Unmount desmonta /mnt/<folder>. Recusa se algum programa usa o disco. Antes, o BrunOS solta a pasta
// dele no Arquivos (senão ele mesmo apareceria como "usando o disco").
func (m *Monitor) Unmount(ctx context.Context, folder string) (UnmountResult, error) {
	var res UnmountResult
	if !hostcmd.ValidFolder(folder) {
		return res, errors.New("nome de pasta inválido")
	}
	d, _, ok := m.partByFolder(folder)
	switch {
	case !ok:
		return res, errors.New("não há disco montado em /mnt/" + folder)
	case d.System:
		return res, errors.New("esse é o disco do sistema")
	}
	if m.Release != nil {
		m.Release(folder)
	}
	qctx, cancel := context.WithTimeout(ctx, mountWait)
	out, err := m.Act(qctx, hostcmd.CmdUnmount+" "+folder)
	cancel()
	if err != nil {
		if m.Unrelease != nil {
			m.Unrelease(folder)
		}
		return res, errors.New(friendly(err))
	}
	for _, line := range strings.Split(string(out), "\n") {
		switch f := strings.Fields(line); {
		case len(f) > 0 && f[0] == "preso":
			res.Stuck = append([]string{}, f[1:]...)
		case strings.TrimSpace(line) == "fstab ficou":
			res.FstabKept = true
		}
	}
	if res.Stuck == nil {
		res.Stuck = []string{}
	}
	if m.OnUnmount != nil {
		m.OnUnmount(folder)
	}
	m.emit([]events.Event{{Kind: "disk-unmounted-ok", Level: events.Info, Title: "Disco desmontado: /mnt/" + folder,
		Body: "O disco foi desmontado pelo BrunOS e não volta sozinho ao reiniciar o servidor."}})
	go func() { _ = m.Refresh(m.bg()) }()
	return res, nil
}

// Label troca o rótulo de uma partição desmontada.
func (m *Monitor) Label(ctx context.Context, uuid, label string) error {
	if !hostcmd.ValidUUID(uuid) || !hostcmd.ValidLabel(label, "") {
		return errors.New("rótulo inválido: use até 16 letras, números, - e _")
	}
	d, p, ok := m.partByUUID(uuid)
	switch {
	case !ok:
		return ErrNoDisk
	case d.System:
		return errors.New("essa partição é do disco do sistema")
	case len(p.Mounts) > 0:
		return errors.New("desmonte a partição antes de trocar o rótulo")
	}
	qctx, cancel := context.WithTimeout(ctx, mountWait)
	_, err := m.Act(qctx, hostcmd.CmdLabel+" "+uuid+" "+label)
	cancel()
	if err != nil {
		return errors.New(friendly(err))
	}
	go func() { _ = m.Refresh(m.bg()) }()
	return nil
}

// ErrFormatBusy: já tem uma formatação rodando.
var ErrFormatBusy = errors.New("já tem uma formatação rodando: espere terminar")

// Format apaga o disco inteiro e cria uma partição com o sistema de arquivos escolhido. A Senha de
// formatação vai pela entrada padrão do script, que a confere (com trava) e confere o disco de novo.
func (m *Monitor) Format(ctx context.Context, dev, fs, label, formatPassword string) error {
	d, ok := m.find(dev)
	switch {
	case !ok:
		return ErrNoDisk
	case m.ActInput == nil:
		return errors.New("formatar indisponível")
	case d.System:
		return errors.New("este é o disco do sistema")
	case len(d.Mounts()) > 0:
		return errors.New("o disco ainda está montado em " + strings.Join(d.Mounts(), ", ") + ": desmonte antes de formatar")
	case !hostcmd.ValidSerial(d.Serial):
		return errors.New("este disco não informa um número de série que o BrunOS consiga conferir: por segurança, formate pelo Terminal")
	case !hostcmd.FormatFS[fs]:
		return errors.New("sistema de arquivos inválido")
	case !hostcmd.ValidLabel(label, fs):
		return errors.New("rótulo inválido: use letras, números, - e _ (até 16; no exFAT, até 11)")
	case formatPassword == "" || len(formatPassword) > 200 || strings.ContainsAny(formatPassword, "\r\n"):
		return errors.New("digite a Senha de formatação")
	}
	m.fmt.mu.Lock()
	if m.fmt.job != nil && m.fmt.job.Running {
		m.fmt.mu.Unlock()
		return ErrFormatBusy
	}
	m.fmt.mu.Unlock()
	cmd := strings.Join([]string{hostcmd.CmdFormat, dev, d.Serial, fs, label}, " ")
	if !hostcmd.FormatAllowed(cmd) {
		return errors.New("pedido inválido")
	}
	qctx, cancel := context.WithTimeout(ctx, formatWait)
	_, err := m.ActInput(qctx, cmd, []byte(formatPassword+"\n"))
	cancel()
	if err != nil {
		return errors.New(friendly(err))
	}
	job := &FormatJob{Dev: dev, Name: title(d), FS: fs, Label: label, Started: m.Now().Unix(), Running: true}
	m.fmt.mu.Lock()
	m.fmt.job = job
	m.fmt.mu.Unlock()
	slog.Info("discos: formatação iniciada", "disco", dev, "fs", fs)
	m.mu.Lock()
	if k := m.st.Known[d.Key]; k != nil {
		k.add(Event{At: job.Started, Kind: "formatar", Level: "aviso", Text: "Formatação pedida pelo BrunOS (" + fs + ", rótulo " + label + ")"})
	}
	m.mu.Unlock()
	m.save()
	go m.followFormat(d.Key)
	return nil
}

// parseStatus lê o "discos-estado".
func parseStatus(out []byte) (running bool, st FormatState, log []string) {
	inLog := false
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if inLog {
			if strings.TrimSpace(line) != "" {
				log = append(log, line)
			}
			continue
		}
		f := strings.Fields(line)
		switch {
		case line == "=== registro":
			inLog = true
		case line == "rodando":
			running = true
		case len(f) == 2 && f[0] == "senha":
			st.HasPassword = f[1] == "sim"
		case len(f) == 3 && f[0] == "trava":
			st.Fails, _ = strconv.Atoi(f[1])
			st.LockedUntil, _ = strconv.ParseInt(f[2], 10, 64)
		}
	}
	return
}

// applyLog atualiza o trabalho pelas linhas do registro (só se o registro for desta formatação).
func applyLog(job *FormatJob, running bool, log []string) {
	if len(log) == 0 {
		return
	}
	f := strings.Fields(log[0])
	if len(f) < 2 || f[0] != "inicio" {
		return
	}
	if t, _ := strconv.ParseInt(f[1], 10, 64); t < job.Started-30 {
		return // registro de uma formatação anterior
	}
	job.Lines = nil
	for _, l := range log[1:] {
		g := strings.Fields(l)
		switch {
		case len(g) == 2 && g[0] == "etapa":
			job.Step = g[1]
		case len(g) == 2 && g[0] == "uuid":
			job.UUID = g[1]
		case len(g) == 2 && g[0] == "fim":
			job.Running, job.OK = false, g[1] == "0"
		default:
			job.Lines = append(job.Lines, l)
		}
	}
	if len(job.Lines) > 12 {
		job.Lines = job.Lines[len(job.Lines)-12:]
	}
	if !running && job.Running { // o serviço parou sem escrever o fim
		job.Running = false
	}
	if !job.Running && !job.OK && job.Err == "" {
		job.Err = "a formatação não terminou"
		for i := len(job.Lines) - 1; i >= 0; i-- {
			if l := strings.TrimSpace(job.Lines[i]); l != "" {
				job.Err = l
				break
			}
		}
	}
}

func (m *Monitor) followFormat(key string) {
	deadline := m.Now().Add(followMax)
	ctx := m.bg()
	for m.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(followStep):
		}
		qctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		out, err := m.Query(qctx, hostcmd.CmdDiskStatus)
		cancel()
		if err != nil {
			continue
		}
		running, _, log := parseStatus(out)
		m.fmt.mu.Lock()
		job := m.fmt.job
		applyLog(job, running, log)
		done := !job.Running
		if done {
			job.Ended = m.Now().Unix()
		}
		cp := *job
		m.fmt.mu.Unlock()
		if !done {
			continue
		}
		ev := events.Event{Kind: "disk-formatted", Level: events.Info, Key: fmt.Sprintf("disk-format:%s:%d", cp.Dev, cp.Started),
			Title: "Disco formatado: " + cp.Label, Body: "O disco " + cp.Name + " foi apagado e formatado em " + cp.FS + ". Ele está pronto para montar na janela Discos."}
		note := Event{At: m.Now().Unix(), Kind: "formatar", Text: "Formatado em " + cp.FS + " (rótulo " + cp.Label + ")"}
		if !cp.OK {
			ev.Level, ev.Title = events.Aviso, "A formatação não terminou: "+cp.Name
			ev.Body = "Motivo: " + cp.Err + ". Se o erro apareceu antes de apagar, o disco continua como estava; confira na janela Discos."
			note.Level, note.Text = "aviso", "A formatação não terminou: "+cp.Err
		}
		slog.Info("discos: formatação terminou", "disco", cp.Dev, "ok", cp.OK, "erro", cp.Err)
		m.mu.Lock()
		if k := m.st.Known[key]; k != nil {
			k.add(note)
		}
		m.mu.Unlock()
		m.save()
		m.emit([]events.Event{ev})
		_ = m.Refresh(ctx)
		return
	}
	m.fmt.mu.Lock()
	if m.fmt.job != nil && m.fmt.job.Running {
		m.fmt.job.Running, m.fmt.job.Err = false, "o BrunOS parou de acompanhar depois de 30 minutos: confira o disco na janela Discos"
	}
	m.fmt.mu.Unlock()
}

// FormatInfo pergunta ao servidor se a Senha de formatação existe e devolve a formatação em andamento.
func (m *Monitor) FormatInfo(ctx context.Context) FormatState {
	var st FormatState
	qctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	out, err := m.Query(qctx, hostcmd.CmdDiskStatus)
	cancel()
	if err != nil {
		st.Err = friendly(err)
	} else {
		_, st, _ = parseStatus(out)
	}
	m.fmt.mu.Lock()
	if m.fmt.job != nil {
		cp := *m.fmt.job
		st.Job = &cp
	}
	m.fmt.mu.Unlock()
	return st
}
