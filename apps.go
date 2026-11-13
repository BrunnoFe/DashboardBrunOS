package main

// Apps: ligar/parar/reiniciar, containers, logs, histórico de uso e disponibilidade.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"

	"brunos/internal/appmon"
	"brunos/internal/dockerc"
	"brunos/internal/runtipi"
	"brunos/internal/tsdb"
)

func healthName(h int8) string {
	switch h {
	case appmon.Up:
		return "up"
	case appmon.Down:
		return "down"
	}
	return "unknown"
}

func (s *server) appByURN(urn string) (runtipi.App, bool) {
	apps, _ := s.tipi.Apps()
	for _, a := range apps {
		if a.URN == urn {
			return a, true
		}
	}
	return runtipi.App{}, false
}

// appContainers devolve os containers do app (pelo projeto do compose).
func (s *server) appContainers(ctx context.Context, a runtipi.App) ([]dockerc.Container, error) {
	cts, err := s.containers(ctx)
	if err != nil {
		return nil, err
	}
	var out []dockerc.Container
	for _, c := range cts {
		if c.Project() == a.Project {
			out = append(out, c)
		}
	}
	return out, nil
}

// hAppAction liga, para ou reinicia todos os containers do app (exige BRUNOS_DOCKER_ACTIONS).
// Pelo Docker o Runtipi não fica sabendo: se o servidor ou o Runtipi reiniciar, o app volta ao
// estado que o Runtipi conhece (a interface avisa isso).
func (s *server) hAppAction(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.DockerActions || s.docker == nil {
			writeJSON(w, 403, map[string]string{"error": "ações no Docker estão desligadas (BRUNOS_DOCKER_ACTIONS)"})
			return
		}
		var req struct {
			URN string `json:"urn"`
		}
		if err := decode(r, &req, 4<<10); err != nil {
			writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
			return
		}
		a, ok := s.appByURN(req.URN)
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "app não encontrado"})
			return
		}
		cts, err := s.appContainers(r.Context(), a)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": "Docker indisponível"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		n := 0
		for _, c := range cts {
			var err error
			switch kind {
			case "restart":
				if c.State != "running" {
					continue
				}
				err = s.docker.Restart(ctx, c.ID)
			case "start":
				if c.State == "running" {
					continue
				}
				err = s.docker.Start(ctx, c.ID)
			case "stop":
				if c.State != "running" && c.State != "restarting" {
					continue
				}
				err = s.docker.Stop(ctx, c.ID)
			}
			if err != nil {
				slog.Warn("ação no app", "acao", kind, "container", c.Name(), "erro", err)
				writeJSON(w, 502, map[string]string{"error": "não deu para " + map[string]string{"restart": "reiniciar", "start": "ligar", "stop": "parar"}[kind] + " " + c.Name()})
				return
			}
			n++
		}
		s.ctMu.Lock()
		s.ctAt = time.Time{}
		s.ctMu.Unlock()
		if kind == "stop" {
			appmon.Resolve(s.events, a.URN) // parado de propósito: não é queda
		}
		slog.Info("ação no app", "acao", kind, "app", a.URN, "containers", n, "por", session(r).Username)
		writeJSON(w, 200, map[string]int{"containers": n, "restarted": n})
	}
}

type containerOut struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Service string `json:"service"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Image   string `json:"image"`
}

func (s *server) hAppContainers(w http.ResponseWriter, r *http.Request) {
	a, ok := s.appByURN(r.URL.Query().Get("urn"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "app não encontrado"})
		return
	}
	cts, err := s.appContainers(r.Context(), a)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "Docker indisponível"})
		return
	}
	out := make([]containerOut, 0, len(cts))
	for _, c := range cts {
		out = append(out, containerOut{ID: c.ID, Name: c.Name(), Service: c.Service(), State: c.State, Status: c.Status, Image: c.Image})
	}
	writeJSON(w, 200, out)
}

func (s *server) hAppHistory(w http.ResponseWriter, r *http.Request) {
	a, ok := s.appByURN(r.URL.Query().Get("urn"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "app não encontrado"})
		return
	}
	rng := r.URL.Query().Get("range")
	if _, ok := tsdb.Periods[rng]; !ok {
		rng = "24h"
	}
	step, pts, uptime := s.appSeries(a.Project, rng)
	res := map[string]any{"points": pts, "health": healthName(s.appmon.Health(a.Project)), "step": step, "range": rng}
	if uptime != nil {
		res["uptime"] = *uptime
	} else if pct, ok := s.appmon.Uptime(a.Project); ok {
		res["uptime"] = pct
	}
	writeJSON(w, 200, res)
}

// appSeries junta CPU, memória e disponibilidade do app (tsdb) pelo horário. up: 1 no ar, 0 fora (nos
// períodos longos, a fração do tempo no ar). uptime = % no ar no período (nil sem medição).
func (s *server) appSeries(project, rng string) (int64, []map[string]any, *float64) {
	if s.tsdb == nil {
		return 60, []map[string]any{}, nil
	}
	pref := "app." + project + "."
	by := map[int64]map[string]any{}
	var order []int64
	var step int64
	var upSum float64
	upN := 0
	for _, k := range []string{"cpu", "mem", "up"} {
		st, list := s.tsdb.Query(pref+k, rng)
		step = st
		for _, p := range list {
			m := by[p.T]
			if m == nil {
				m = map[string]any{"t": p.T}
				by[p.T] = m
				order = append(order, p.T)
			}
			m[k] = round2(float64(p.V))
			if st > tsdb.FineStep && k != "up" {
				m[k+"Max"] = round2(float64(p.Max))
			}
			if k == "up" {
				upSum += float64(p.V)
				upN++
			}
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	pts := make([]map[string]any, 0, len(order))
	for _, t := range order {
		pts = append(pts, by[t])
	}
	if upN == 0 {
		return step, pts, nil
	}
	pct := math.Round(upSum/float64(upN)*1000) / 10
	return step, pts, &pct
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// logTarget confere que o container pedido é mesmo do app (ninguém lê logs de outro container).
func (s *server) logTarget(r *http.Request) (dockerc.Container, error) {
	q := r.URL.Query()
	if s.docker == nil {
		return dockerc.Container{}, errors.New("Docker não conectado")
	}
	a, ok := s.appByURN(q.Get("urn"))
	if !ok {
		return dockerc.Container{}, errors.New("app não encontrado")
	}
	cts, err := s.appContainers(r.Context(), a)
	if err != nil {
		return dockerc.Container{}, errors.New("Docker indisponível")
	}
	for _, c := range cts {
		if c.ID == q.Get("id") || (q.Get("id") == "" && len(cts) > 0) {
			return c, nil
		}
	}
	return dockerc.Container{}, errors.New("container não encontrado neste app")
}

func logTail(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if n <= 0 {
		n = 300
	}
	return min(n, 5000)
}

// hAppLogs devolve as últimas linhas dos logs como texto.
func (s *server) hAppLogs(w http.ResponseWriter, r *http.Request) {
	c, err := s.logTarget(r)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	body, mux, err := s.docker.Logs(ctx, c.ID, logTail(r), false)
	if err != nil {
		slog.Warn("logs", "container", c.Name(), "erro", err)
		writeJSON(w, 502, map[string]string{"error": "não deu para ler os logs (o docker-compose do BrunOS libera os logs no proxy?)"})
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	lw := &limitWriter{w: w, n: 8 << 20}
	if mux {
		_ = dockerc.Demux(body, lw)
	} else {
		_, _ = io.Copy(lw, body)
	}
}

type limitWriter struct {
	w io.Writer
	n int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errors.New("limite")
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.w.Write(p)
	l.n -= int64(n)
	return n, err
}

// hAppLogsStream manda as linhas novas por SSE (cada evento é uma linha em JSON).
func (s *server) hAppLogsStream(w http.ResponseWriter, r *http.Request) {
	c, err := s.logTarget(r)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming indisponível"})
		return
	}
	body, mux, err := s.docker.Logs(r.Context(), c.ID, 0, true)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "não deu para acompanhar os logs"})
		return
	}
	defer body.Close()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, "retry: 5000\nevent: ready\ndata: {}\n\n")
	flusher.Flush()

	pr, pw := io.Pipe()
	go func() {
		if mux {
			pw.CloseWithError(dockerc.Demux(body, pw))
		} else {
			_, err := io.Copy(pw, body)
			pw.CloseWithError(err)
		}
	}()
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-r.Context().Done():
				return
			}
		}
	}()
	keep := time.NewTicker(25 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case l, ok := <-lines:
			if !ok {
				fmt.Fprint(w, "event: end\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			b, _ := json.Marshal(l)
			fmt.Fprintf(w, "data: %s\n\n", b)
			// junta as linhas que chegaram juntas num flush só
			for n := 0; n < 200; n++ {
				select {
				case l, ok := <-lines:
					if !ok {
						break
					}
					b, _ := json.Marshal(l)
					fmt.Fprintf(w, "data: %s\n\n", b)
					continue
				default:
				}
				break
			}
			flusher.Flush()
		case <-keep.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// hProcesses lista os processos do servidor, com o nome do app quando o processo é de um container.
func (s *server) hProcesses(w http.ResponseWriter, r *http.Request) {
	list, sum := s.procs.List(r.URL.Query().Get("kernel") == "1")
	if s.docker != nil {
		if cts, err := s.containers(r.Context()); err == nil {
			apps, _ := s.tipi.Apps()
			byProject := map[string]string{}
			for _, a := range apps {
				byProject[a.Project] = a.Name
			}
			names := map[string]string{}
			for _, c := range cts {
				n := byProject[c.Project()]
				if n == "" {
					n = c.Name()
				}
				names[c.ID] = n
			}
			for i := range list {
				if list[i].Container != "" {
					list[i].App = names[list[i].Container]
					if list[i].App == "" {
						list[i].App = "container " + list[i].Container[:12]
					}
				}
			}
		}
	}
	if len(list) > 300 {
		list = list[:300]
	}
	writeJSON(w, 200, map[string]any{"procs": list, "summary": sum})
}

// ── Vigia de apps: uso a cada minuto (histórico de 24 h) e disponibilidade ──

// monitorPrefs lê das preferências se o monitor está ligado e quais apps ficam de fora.
func (s *server) monitorPrefs() (on bool, skip map[string]bool) {
	set := s.readSettings()
	on = true
	if v, ok := set["appMonitor"].(bool); ok {
		on = v
	}
	skip = map[string]bool{}
	if apps, ok := set["apps"].(map[string]any); ok {
		for id, v := range apps {
			if m, ok := v.(map[string]any); ok {
				if no, _ := m["noMonitor"].(bool); no {
					skip[id] = true
				}
			}
		}
	}
	return on, skip
}

func (s *server) watchApps(ctx context.Context) {
	if s.docker == nil {
		return
	}
	time.Sleep(30 * time.Second) // deixa os apps subirem depois de ligar o servidor
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.checkApps(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// probe confere se o app responde: abre a porta e faz um GET. Qualquer resposta HTTP (até 401 ou
// 404) conta como "no ar"; porta fechada ou sem resposta em 8 s conta como fora do ar. Um serviço
// que aceita a conexão mas não fala HTTP também conta como no ar.
var probeClient = &http.Client{
	Timeout:       8 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Transport:     &http.Transport{DisableKeepAlives: true, ResponseHeaderTimeout: 8 * time.Second},
}

func probe(ctx context.Context, host string, port int) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	d := net.Dialer{Timeout: 4 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	conn.Close()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/", nil)
	req.Header.Set("User-Agent", "BrunOS-monitor")
	resp, err := probeClient.Do(req)
	if err != nil {
		var ne net.Error
		return !(errors.As(err, &ne) && ne.Timeout()) // travado = fora do ar; protocolo diferente = no ar
	}
	resp.Body.Close()
	return true
}

func (s *server) checkApps(ctx context.Context) {
	apps, info := s.tipi.Apps()
	cts, err := s.containers(ctx)
	if err != nil {
		return
	}
	usage, _ := s.collectUsage(ctx)
	byProject := map[string]appUsage{}
	for _, u := range usage {
		byProject[u.Project] = u
	}
	on, skip := s.monitorPrefs()
	alive := map[string]bool{}
	for _, a := range apps {
		alive[a.Project] = true
		total, running, restarting := 0, 0, 0
		for _, c := range cts {
			if c.Project() != a.Project {
				continue
			}
			total++
			switch c.State {
			case "running":
				running++
			case "restarting":
				restarting++
			}
		}
		u := byProject[a.Project]
		sample := appmon.Sample{CPU: u.CPU, MemMB: float64(u.Memory) / (1 << 20), Up: appmon.Unknown}
		watch := on && !skip[a.URN]
		switch {
		case total == 0 || (running == 0 && restarting == 0):
			// parado (pelo Runtipi ou de propósito): não é queda
		case restarting > 0:
			sample.Up = appmon.Down // reiniciando sem parar: está quebrando
		case a.Port > 0 && info.InternalIP != "" && watch:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if probe(pctx, info.InternalIP, a.Port) {
				sample.Up = appmon.Up
			} else {
				sample.Up = appmon.Down
			}
			cancel()
		default:
			sample.Up = appmon.Up
		}
		s.appmon.Add(a.Project, sample)
		if s.tsdb != nil && total > 0 { // Histórico de 30 dias (0.14.0); app sem container não grava zeros
			pref := "app." + a.Project + "."
			s.tsdb.Add(pref+"cpu", sample.CPU)
			s.tsdb.Add(pref+"mem", sample.MemMB)
			if sample.Up != appmon.Unknown {
				s.tsdb.Add(pref+"up", float64(sample.Up))
			}
		}
		if watch && sample.Up != appmon.Unknown {
			s.appmon.Alert(s.events, a.Project, a.URN, a.Name)
		} else if !watch || sample.Up == appmon.Unknown {
			appmon.Resolve(s.events, a.URN)
		}
	}
	s.appmon.Forget(alive)
}
