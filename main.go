// BrunOS — painel leve para servidor caseiro com Runtipi.
// Um único binário Go (sem dependências externas) que serve a interface e a API.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"brunos/internal/appmon"
	"brunos/internal/auth"
	"brunos/internal/diskhist"
	"brunos/internal/dockerc"
	"brunos/internal/energy"
	"brunos/internal/events"
	"brunos/internal/files"
	"brunos/internal/history"
	"brunos/internal/hostcmd"
	"brunos/internal/integrations"
	"brunos/internal/logins"
	"brunos/internal/netmon"
	"brunos/internal/notes"
	"brunos/internal/power"
	"brunos/internal/procs"
	"brunos/internal/runtipi"
	"brunos/internal/secret"
	"brunos/internal/smart"
	"brunos/internal/sshknown"
	"brunos/internal/sysinfo"
	"brunos/internal/terminal"
	"brunos/internal/termkey"
	"brunos/internal/updates"
	"brunos/web"
)

var version = "dev"

type config struct {
	Addr          string
	DataDir       string
	FilesDir      string
	Roots         string
	RuntipiDir    string
	DockerHost    string
	DockerActions bool
	HostProc      string
	MaxUploadGB   int64
	PublicSetup   bool
	Terminal      bool
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envBool(k string) bool {
	v := strings.ToLower(env(k, ""))
	return v == "1" || v == "true" || v == "yes" || v == "sim"
}

func loadConfig() config {
	c := config{
		Addr:          env("BRUNOS_ADDR", ":8080"),
		DataDir:       env("BRUNOS_DATA", "/data"),
		FilesDir:      env("BRUNOS_FILES", "/files"),
		Roots:         env("BRUNOS_ROOTS", ""),
		RuntipiDir:    env("BRUNOS_RUNTIPI", "/runtipi"),
		DockerHost:    env("DOCKER_HOST", "unix:///var/run/docker.sock"),
		DockerActions: envBool("BRUNOS_DOCKER_ACTIONS"),
		HostProc:      env("HOST_PROC", "/host/proc"),
		PublicSetup:   envBool("BRUNOS_ALLOW_PUBLIC_SETUP"),
		Terminal:      env("BRUNOS_TERMINAL", "true") != "false" && env("BRUNOS_TERMINAL", "true") != "off",
	}
	c.MaxUploadGB, _ = strconv.ParseInt(env("BRUNOS_MAX_UPLOAD_GB", "50"), 10, 64)
	if c.MaxUploadGB <= 0 {
		c.MaxUploadGB = 50
	}
	return c
}

type server struct {
	cfg       config
	auth      *auth.Manager
	stats     *sysinfo.Collector
	tipi      *runtipi.Discoverer
	docker    *dockerc.Client
	files     *files.Service
	term      *terminal.Manager
	events    *events.Store
	power     *power.Monitor
	logins    *logins.Store
	diskHist  *diskhist.Store
	appmon    *appmon.Monitor
	hist      *history.Store
	procs     *procs.Reader
	netmon    *netmon.Monitor
	integ     *integrations.Hub
	tkey      *termkey.Vault
	notes     *notes.Store
	energy    *energy.Monitor
	smart     *smart.Monitor
	updates   *updates.Checker
	settingMu sync.Mutex

	ctMu    sync.Mutex
	ctCache []dockerc.Container
	ctAt    time.Time

	usageMu sync.Mutex
	usage   []appUsage
	usageAt time.Time
	started time.Time
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg := loadConfig()
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "wallpapers"), 0o700); err != nil {
		slog.Error("não foi possível criar a pasta de dados", "dir", cfg.DataDir, "erro", err)
		os.Exit(1)
	}
	am, err := auth.New(cfg.DataDir)
	if err != nil {
		slog.Error("falha ao ler credenciais", "erro", err)
		os.Exit(1)
	}

	s := &server{cfg: cfg, auth: am, tipi: runtipi.New(cfg.RuntipiDir), started: time.Now(), appmon: appmon.New()}

	hostProc := ""
	if st, err := os.Stat(cfg.HostProc); err == nil && st.IsDir() {
		hostProc = cfg.HostProc
	}
	s.stats = sysinfo.NewCollector(sysinfo.Config{
		Proc: "/proc", Sys: "/sys", HostProc: hostProc,
		OSRelease: env("HOST_OS_RELEASE", "/host/os-release"), Hostname: env("HOST_HOSTNAME_FILE", "/host/hostname"),
	})

	if cfg.DockerHost != "off" {
		if dc, err := dockerc.New(cfg.DockerHost); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			if err := dc.Ping(ctx); err != nil {
				slog.Warn("Docker indisponível — status dos apps desligado", "host", cfg.DockerHost, "erro", err)
			}
			cancel()
			s.docker = dc
		} else {
			slog.Warn("DOCKER_HOST inválido", "erro", err)
		}
	}

	fsvc, errs := files.New(func() []files.RootConfig { return files.ParseRoots(cfg.Roots, cfg.FilesDir) })
	for _, e := range errs {
		slog.Warn("pasta ignorada", "erro", e)
	}
	known := sshknown.New(filepath.Join(cfg.DataDir, "known_hosts.json")) // terminal e desligamento usam o mesmo arquivo
	s.tkey = termkey.New(filepath.Join(cfg.DataDir, "terminal_key.json"))
	s.notes = notes.New(filepath.Join(cfg.DataDir, "notes.json"))
	if cfg.Terminal {
		s.term = terminal.NewManagerWith(known)
		s.term.OwnerOf = func(r *http.Request) string { return session(r).ID }
		s.term.KeyFor = func(r *http.Request) (ssh.Signer, error) {
			if k := s.tkey.Signer(session(r).ID); k != nil {
				return k, nil
			}
			if s.tkey.Exists() {
				return nil, terminal.ErrKeyLocked
			}
			return nil, terminal.ErrNoKey
		}
		s.term.Paused = am.SessionLocked
		s.term.Locked = func(r *http.Request) bool { return am.IsLocked(session(r).ID) }
		s.term.IdleTimeout = s.termIdle
		s.term.OnDone = s.termDone
	}
	s.events = events.New(filepath.Join(cfg.DataDir, "events.json"))
	s.logins = logins.New(filepath.Join(cfg.DataDir, "logins.json"))
	s.logins.IsLocal = isPrivate
	if s.logins.Fresh() { // primeira vez com esta versão: quem já está logado não é "novo"
		for _, ss := range am.Sessions() {
			s.logins.Learn(ss.IP, ss.UserAgent, ss.CreatedAt)
		}
	}
	am.OnBlock = s.loginBlocked
	s.diskHist = diskhist.New(filepath.Join(cfg.DataDir, "disk-history.json"))
	s.hist = history.New(filepath.Join(cfg.DataDir, "history.json"))
	s.stats.OnSample = s.hist.Add
	procDir := hostProc
	if procDir == "" {
		procDir = "/proc" // sem o /proc do host: só os processos do próprio container
	}
	s.procs = procs.New(procDir, env("HOST_PASSWD", "/host/passwd"))
	s.procs.Self = procs.SelfID("/proc/self/mountinfo")
	s.netmon = netmon.New(filepath.Join(cfg.DataDir, "internet.json"), s.events)
	s.netmon.Enabled = func() bool { v, ok := s.readSettings()["netMonitor"].(bool); return !ok || v }
	box, err := secret.Load(filepath.Join(cfg.DataDir, "secret.key")) // cifra as senhas/tokens das integrações
	if err != nil {
		slog.Error("não foi possível ler ou criar a chave das integrações", "erro", err)
		os.Exit(1)
	}
	s.integ = integrations.NewHub(integrations.NewStore(filepath.Join(cfg.DataDir, "integrations.json"), box),
		integrations.NewSeriesStore(filepath.Join(cfg.DataDir, "metric-history.json")), s.events)
	s.integ.SetUsage(s.readSettings())
	s.integ.ImageDir = filepath.Join(cfg.DataDir, "img-cache") // fotos do Immich e capas do Plex já mostradas
	// Tailscale: lê pelo script do servidor, pela conexão SSH do desligamento seguro (só este pedido)
	integrations.HostQuery = func(ctx context.Context, cmd string) ([]byte, error) {
		if cmd != hostcmd.CmdTailscale { // integrações só podem pedir o Tailscale
			return nil, errors.New("pedido não permitido")
		}
		return s.power.HostOutput(ctx, cmd)
	}
	power.HostOK = terminal.HostRe.MatchString
	power.RuntipiHost = env("BRUNOS_RUNTIPI_HOST", "") // pasta do Runtipi no servidor (para o runtipi-cli)
	s.power = power.New(filepath.Join(cfg.DataDir, "power.json"), filepath.Join(cfg.DataDir, "power-events.json"), s.events,
		// chave nova (script brunos-host) e a da configuração antiga, só para desligar até atualizar
		hostcmd.New(filepath.Join(cfg.DataDir, "hostcmd_key_v2"), filepath.Join(cfg.DataDir, "hostcmd_key"), known))
	s.power.SuggestHost = func() string { _, info := s.tipi.Apps(); return info.InternalIP }
	s.setupEtapa9(cfg.DataDir)
	s.stats.SetDrain(s.power.DrainW())
	fsvc.MapFile = filepath.Join(cfg.DataDir, "disks.json")
	if err := fsvc.LoadMapping(); err != nil {
		slog.Warn("discos mapeados", "erro", err)
	}
	fsvc.MaxUpload = cfg.MaxUploadGB << 30
	fsvc.LoadUploads(filepath.Join(cfg.DataDir, "uploads.json")) // envios em pedaços que ficaram pela metade
	s.files = fsvc
	for _, r := range fsvc.Roots() {
		slog.Info("pasta disponível", "nome", r.Name, "id", r.ID, "somenteLeitura", r.ReadOnly)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go s.stats.Run(ctx)
	go s.power.Run(ctx, s.batteryReading)
	go s.watchDisks(ctx)
	go s.watchApps(ctx)
	go s.netmon.Run(ctx)
	go s.integ.Run(ctx)
	// etapa 9: faltavam desde a 0.11.0 (só rodavam ao clicar em "Ler agora"/"Conferir agora")
	go s.energy.Run(ctx)  // consumo: uma amostra por minuto
	go s.smart.Run(ctx)   // SMART a cada 6 h e temperatura dos discos USB a cada 20 min
	go s.updates.Run(ctx) // atualizações na frequência escolhida e acompanhamento das que estão rodando

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	slog.Info("BrunOS no ar", "versao", version, "endereco", cfg.Addr, "configurado", !am.SetupRequired())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("servidor parou", "erro", err)
		os.Exit(1)
	}
	s.hist.Save() // guarda o histórico de 24 h / 7 dias antes de desligar
	s.integ.Series.Save()
}

func healthcheck() int {
	addr := env("BRUNOS_ADDR", ":8080")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/api/health")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return 1
	}
	return 0
}

// ── Rotas e middlewares ───────────────────────────────────────────

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	a := s.requireAuth

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"ok": true}) })
	mux.HandleFunc("GET /api/public", s.hPublic)
	mux.HandleFunc("GET /api/public/wallpaper", s.hPublicWallpaper)
	mux.HandleFunc("POST /api/auth/setup", s.csrf(s.hSetup))
	mux.HandleFunc("POST /api/auth/login", s.csrf(s.hLogin))
	mux.HandleFunc("POST /api/auth/logout", a(s.hLogout))
	mux.HandleFunc("GET /api/auth/me", a(s.hMe))
	mux.HandleFunc("PUT /api/auth/me", a(s.hMeUpdate))
	mux.HandleFunc("POST /api/auth/password", a(s.hPassword))
	mux.HandleFunc("GET /api/auth/lock", a(s.hLockState))
	mux.HandleFunc("POST /api/auth/lock", a(s.hLock))
	mux.HandleFunc("POST /api/auth/unlock", a(s.hUnlock))
	s.notes.Register(mux, a)
	if s.term != nil {
		mux.HandleFunc("GET /api/terminal/key", a(s.hTermKey))
		mux.HandleFunc("POST /api/terminal/key", a(s.hTermKeyCreate))
		mux.HandleFunc("POST /api/terminal/key/unlock", a(s.hTermKeyUnlock))
		mux.HandleFunc("DELETE /api/terminal/key", a(s.hTermKeyDelete))
	}

	mux.HandleFunc("GET /api/settings", a(s.hSettingsGet))
	mux.HandleFunc("PUT /api/settings", a(s.hSettingsPut))
	mux.HandleFunc("GET /api/settings/backups", a(s.hSettingsBackups))
	mux.HandleFunc("POST /api/settings/restore", a(s.hSettingsRestore))
	mux.HandleFunc("GET /api/wallpapers", a(s.hWallpapers))
	mux.HandleFunc("PUT /api/wallpapers", a(s.hWallpaperUpload))
	mux.HandleFunc("GET /api/wallpapers/{name}", a(s.hWallpaperFile))
	mux.HandleFunc("DELETE /api/wallpapers/{name}", a(s.hWallpaperDelete))

	mux.HandleFunc("GET /api/stats", a(s.hStats))
	mux.HandleFunc("GET /api/stats/stream", a(s.hStatsStream))
	mux.HandleFunc("GET /api/system", a(s.hSystem))
	mux.HandleFunc("GET /api/apps", a(s.hApps))
	mux.HandleFunc("GET /api/apps/logo", a(s.hAppLogo))
	mux.HandleFunc("GET /api/usage", a(s.hUsage))
	mux.HandleFunc("POST /api/apps/restart", a(s.hAppAction("restart")))
	mux.HandleFunc("POST /api/apps/start", a(s.hAppAction("start")))
	mux.HandleFunc("POST /api/apps/stop", a(s.hAppAction("stop")))
	mux.HandleFunc("GET /api/apps/containers", a(s.hAppContainers))
	mux.HandleFunc("GET /api/apps/history", a(s.hAppHistory))
	mux.HandleFunc("GET /api/apps/logs", a(s.hAppLogs))
	mux.HandleFunc("GET /api/apps/logs/stream", a(s.hAppLogsStream))

	s.files.Register(mux, a)
	s.events.Register(mux, a)
	s.power.Register(mux, a)
	s.diskHist.Register(mux, a)
	s.hist.Register(mux, a)
	s.netmon.Register(mux, a)
	s.integ.Register(mux, a, s.integrationApps)
	s.energy.Register(mux, a)
	s.smart.Register(mux, a)
	s.updates.Register(mux, a)
	mux.HandleFunc("GET /api/processes", a(s.hProcesses))
	if s.term != nil {
		s.term.Register(mux, a)
	}

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]string{"error": "rota desconhecida"})
	})
	mux.Handle("/", web.Handler(version))
	return securityHeaders(mux)
}

const appCSP = "default-src 'self'; img-src 'self' data: blob: https://cdn.jsdelivr.net; media-src 'self' blob:; " +
	"style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; font-src 'self'; frame-src 'self'; " +
	"frame-ancestors 'self'; base-uri 'none'; form-action 'self'; object-src 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", appCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

var privateNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

func isPrivate(ip net.IP) bool {
	for _, n := range privateNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP: atrás do Traefik do Runtipi o endereço remoto é o do proxy (rede privada);
// nesse caso usamos o último IP do X-Forwarded-For, que é o que o Traefik acrescentou.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && isPrivate(ip) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if last := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); last != nil {
				return last.String()
			}
		}
	}
	if ip == nil {
		return host
	}
	return ip.String()
}

func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); ip != nil && isPrivate(ip) {
		return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	return false
}

const cookieName = "brunos_session"

type ctxKey struct{}

type reqSession struct {
	*auth.Session
	token string
}

// csrf: requisições que mudam algo precisam do cabeçalho X-BrunOS (um site de fora não
// consegue enviá-lo sem CORS, que o BrunOS não habilita) e, se houver Origin, do mesmo host.
func (s *server) csrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-BrunOS") != "1" {
				writeJSON(w, 403, map[string]string{"error": "requisição recusada"})
				return
			}
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || !strings.EqualFold(u.Host, r.Host) {
					writeJSON(w, 403, map[string]string{"error": "origem recusada"})
					return
				}
			}
		}
		next(w, r)
	}
}

func (s *server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return s.csrf(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			writeJSON(w, 401, map[string]string{"error": "faça login"})
			return
		}
		sess, ok := s.auth.Validate(c.Value)
		if !ok {
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			writeJSON(w, 401, map[string]string{"error": "sessão expirada"})
			return
		}
		// Bloqueio com senha (decisão 39): só dá para ver o estado, desbloquear ou sair
		if sess.Locked && !lockAllowed[r.Method+" "+r.URL.Path] {
			writeJSON(w, 423, map[string]any{"error": "a tela está bloqueada: digite a senha", "locked": true})
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, reqSession{sess, c.Value})))
	})
}

func session(r *http.Request) reqSession { return r.Context().Value(ctxKey{}).(reqSession) }

// lockAllowed: rotas que funcionam com a Sessão bloqueada.
var lockAllowed = map[string]bool{
	"GET /api/auth/lock": true, "POST /api/auth/lock": true, "POST /api/auth/unlock": true, "POST /api/auth/logout": true,
}

// ── Bloqueio (J1) e Chave do Terminal (K3) ────────────────────────

func (s *server) hLockState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{"locked": session(r).Locked})
}

// hLock bloqueia a Sessão (modo com senha). A Chave do Terminal fica trancada até desbloquear.
func (s *server) hLock(w http.ResponseWriter, r *http.Request) {
	sess := session(r)
	if err := s.auth.Lock(sess.ID); err != nil {
		writeJSON(w, 500, map[string]string{"error": "não foi possível bloquear"})
		return
	}
	s.tkey.Forget(sess.ID)
	writeJSON(w, 200, map[string]bool{"locked": true})
}

func (s *server) hUnlock(w http.ResponseWriter, r *http.Request) {
	var req credReq
	if err := decode(r, &req, 8<<10); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	sess, ip := session(r), clientIP(r)
	if err := s.auth.Unlock(sess.ID, req.Password, ip); err != nil {
		switch {
		case errors.Is(err, auth.ErrSessionEnded):
			if s.term != nil {
				s.term.CloseOwner(sess.ID)
			}
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			writeJSON(w, 401, map[string]string{"error": err.Error()})
		case errors.Is(err, auth.ErrRateLimited):
			writeJSON(w, 429, map[string]string{"error": err.Error()})
		default:
			slog.Warn("desbloqueio recusado", "ip", ip)
			writeJSON(w, 403, map[string]string{"error": "senha incorreta"})
		}
		return
	}
	s.openTermKey(sess.ID, req.Password)
	writeJSON(w, 200, map[string]bool{"locked": false})
}

// openTermKey abre a Chave do Terminal para a Sessão com a senha que acabou de ser digitada.
func (s *server) openTermKey(sessionID, password string) {
	if s.term == nil || !s.tkey.Exists() {
		return
	}
	if err := s.tkey.Unlock(sessionID, password); err != nil {
		slog.Warn("Chave do Terminal não abriu com a senha do BrunOS", "erro", err)
	}
}

// termIdle: tempo para encerrar uma Aba sem ninguém olhando (Configurações → Terminal).
func (s *server) termIdle() time.Duration {
	v, ok := s.readSettings()["termIdleMin"].(float64)
	if !ok {
		return 15 * time.Minute
	}
	if v <= 0 {
		return 0 // nunca
	}
	return time.Duration(min(v, 24*60)) * time.Minute
}

// termDone avisa no sino que um comando terminou numa Aba minimizada.
func (s *server) termDone(addr, user string, tab int, names []string) {
	what := strings.Join(names, ", ")
	where := strings.TrimSuffix(addr, ":22")
	title := fmt.Sprintf("%s terminou", what)
	if tab > 0 {
		title = fmt.Sprintf("%s terminou na aba %d", what, tab)
	}
	s.events.Emit(events.Event{Kind: "terminal-done", Level: "info", Title: title, Body: fmt.Sprintf("Terminal: %s@%s", user, where)})
}

func (s *server) hTermKey(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.tkey.Info(session(r).ID))
}

// hTermKeyCreate cria a Chave do Terminal. Pede a senha do BrunOS: é ela que tranca a chave.
func (s *server) hTermKeyCreate(w http.ResponseWriter, r *http.Request) {
	var req credReq
	if err := decode(r, &req, 8<<10); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	sess := session(r)
	if !s.verifyPassword(w, r, req.Password) {
		return
	}
	info, err := s.tkey.Create(sess.ID, req.Password)
	if err != nil {
		code := 500
		if errors.Is(err, termkey.ErrExists) {
			code = 409
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("Chave do Terminal criada", "impressao", info.Fingerprint)
	writeJSON(w, 200, info)
}

func (s *server) hTermKeyUnlock(w http.ResponseWriter, r *http.Request) {
	var req credReq
	if err := decode(r, &req, 8<<10); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	if !s.verifyPassword(w, r, req.Password) {
		return
	}
	if err := s.tkey.Unlock(session(r).ID, req.Password); err != nil {
		code := 500
		if errors.Is(err, termkey.ErrNoKey) {
			code = 404
		} else if errors.Is(err, termkey.ErrWrongPassword) {
			// a senha do BrunOS confere, mas não abre a chave: ela foi trancada com outra senha
			err = errors.New("a Chave do Terminal foi trancada com outra senha; apague e crie outra")
			code = 409
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, s.tkey.Info(session(r).ID))
}

func (s *server) hTermKeyDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.tkey.Delete(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "não foi possível apagar a chave"})
		return
	}
	slog.Info("Chave do Terminal apagada")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// verifyPassword confere a senha do BrunOS (com o limite de tentativas) e responde o erro.
func (s *server) verifyPassword(w http.ResponseWriter, r *http.Request, password string) bool {
	err := s.auth.Verify(session(r).Username, password, clientIP(r))
	switch {
	case err == nil:
		return true
	case errors.Is(err, auth.ErrRateLimited):
		writeJSON(w, 429, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, 403, map[string]string{"error": "senha do BrunOS incorreta"})
	}
	return false
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: isHTTPS(r),
		SameSite: http.SameSiteLaxMode, MaxAge: int((30 * 24 * time.Hour).Seconds()),
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request, v any, max int64) error {
	return json.NewDecoder(io.LimitReader(r.Body, max)).Decode(v)
}

// ── Autenticação ─────────────────────────────────────────────────

func (s *server) hPublic(w http.ResponseWriter, r *http.Request) {
	set := s.readSettings()
	pub := map[string]any{"setupRequired": s.auth.SetupRequired(), "displayName": s.auth.DisplayName(), "version": version}
	for _, k := range []string{"wallpaper", "accentMode", "manualHue"} {
		if v, ok := set[k]; ok {
			pub[k] = v
		}
	}
	writeJSON(w, 200, pub)
}

func (s *server) hPublicWallpaper(w http.ResponseWriter, r *http.Request) {
	wp, _ := s.readSettings()["wallpaper"].(string)
	name, ok := strings.CutPrefix(wp, "upload:")
	if !ok || !wallpaperName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	s.serveWallpaper(w, r, name)
}

type credReq struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

func (s *server) hSetup(w http.ResponseWriter, r *http.Request) {
	if !s.auth.SetupRequired() {
		writeJSON(w, 409, map[string]string{"error": auth.ErrSetupDone.Error()})
		return
	}
	ip := clientIP(r)
	if p := net.ParseIP(ip); !s.cfg.PublicSetup && (p == nil || !isPrivate(p)) {
		writeJSON(w, 403, map[string]string{"error": "a configuração inicial só pode ser feita pela rede local"})
		return
	}
	var req credReq
	if err := decode(r, &req, 8<<10); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	token, err := s.auth.Setup(req.Username, req.DisplayName, req.Password, ip, r.UserAgent())
	if err != nil {
		code := 400
		if errors.Is(err, auth.ErrSetupDone) {
			code = 409
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("usuário criado na primeira execução", "usuario", req.Username, "ip", ip)
	s.logins.Learn(ip, r.UserAgent(), time.Now())
	setSessionCookie(w, r, token)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) hLogin(w http.ResponseWriter, r *http.Request) {
	var req credReq
	if err := decode(r, &req, 8<<10); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	ip := clientIP(r)
	token, err := s.auth.Login(req.Username, req.Password, ip, r.UserAgent())
	if err != nil {
		code := 401
		if errors.Is(err, auth.ErrRateLimited) {
			code = 429
		}
		slog.Warn("login recusado", "ip", ip, "motivo", err.Error())
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	s.loginSeen(ip, r.UserAgent())
	s.openTermKey(auth.SessionID(token), req.Password)
	setSessionCookie(w, r, token)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) hLogout(w http.ResponseWriter, r *http.Request) {
	sess := session(r)
	s.auth.Logout(sess.token)
	s.tkey.Forget(sess.ID)
	if s.term != nil {
		s.term.CloseOwner(sess.ID)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) hMe(w http.ResponseWriter, r *http.Request) {
	u, _ := s.auth.User(session(r).Username)
	writeJSON(w, 200, map[string]any{"username": u.Username, "displayName": u.DisplayName, "createdAt": u.CreatedAt, "passwordChangedAt": u.PasswordAt})
}

func (s *server) hMeUpdate(w http.ResponseWriter, r *http.Request) {
	var req credReq
	if err := decode(r, &req, 8<<10); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	if err := s.auth.SetDisplayName(session(r).Username, req.DisplayName); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) hPassword(w http.ResponseWriter, r *http.Request) {
	var req struct{ Current, Next string }
	if err := decode(r, &req, 8<<10); err != nil {
		writeJSON(w, 400, map[string]string{"error": "requisição inválida"})
		return
	}
	sess := session(r)
	if err := s.auth.ChangePassword(sess.Username, req.Current, req.Next, sess.token); err != nil {
		code := 400
		if errors.Is(err, auth.ErrInvalidCredentials) {
			code = 403
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	// a Chave do Terminal passa a abrir com a senha nova; as outras Sessões acabaram
	if err := s.tkey.Rekey(req.Current, req.Next); err != nil {
		slog.Warn("não deu para trancar a Chave do Terminal com a senha nova", "erro", err)
	}
	s.tkey.Keep(sess.ID)
	if s.term != nil {
		s.term.CloseAll()
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ── Preferências (iguais em todos os dispositivos) ────────────────

// integrationApps lista os apps do Runtipi para sugerir integrações já com endereço.
func (s *server) integrationApps() ([]integrations.AppRef, string) {
	apps, info := s.tipi.Apps()
	out := make([]integrations.AppRef, 0, len(apps))
	for _, a := range apps {
		out = append(out, integrations.AppRef{ID: a.ID, Name: a.Name, Port: a.Port})
	}
	return out, info.InternalIP
}

// ── Papéis de parede enviados ─────────────────────────────────────

var wallpaperName = regexp.MustCompile(`^[a-f0-9]{16}\.(jpg|png|webp|avif|gif)$`)

func (s *server) wallpaperDir() string { return filepath.Join(s.cfg.DataDir, "wallpapers") }

func (s *server) hWallpapers(w http.ResponseWriter, r *http.Request) {
	entries, _ := os.ReadDir(s.wallpaperDir())
	type wp struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		Created int64  `json:"created"`
	}
	out := []wp{}
	for _, e := range entries {
		if !wallpaperName.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, wp{e.Name(), info.Size(), info.ModTime().Unix()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	writeJSON(w, 200, out)
}

func sniffImage(head []byte) string {
	switch ct := http.DetectContentType(head); ct {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	}
	if len(head) >= 12 && string(head[4:8]) == "ftyp" && (string(head[8:12]) == "avif" || string(head[8:12]) == "avis") {
		return "avif"
	}
	return ""
}

func (s *server) hWallpaperUpload(w http.ResponseWriter, r *http.Request) {
	const max = 25 << 20
	body := http.MaxBytesReader(w, r.Body, max)
	head := make([]byte, 512)
	n, _ := io.ReadFull(body, head)
	ext := sniffImage(head[:n])
	if ext == "" {
		writeJSON(w, 400, map[string]string{"error": "envie uma imagem JPG, PNG, WebP, AVIF ou GIF"})
		return
	}
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	name := hex.EncodeToString(id) + "." + ext
	tmp, err := os.CreateTemp(s.wallpaperDir(), ".up-*")
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "não foi possível salvar"})
		return
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(head[:n])
	if err == nil {
		_, err = io.Copy(tmp, body)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "imagem grande demais (máx. 25 MB) ou envio interrompido"})
		return
	}
	if err := os.Rename(tmp.Name(), filepath.Join(s.wallpaperDir(), name)); err != nil {
		writeJSON(w, 500, map[string]string{"error": "não foi possível salvar"})
		return
	}
	writeJSON(w, 200, map[string]string{"name": name})
}

func (s *server) serveWallpaper(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, filepath.Join(s.wallpaperDir(), name))
}

func (s *server) hWallpaperFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !wallpaperName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	s.serveWallpaper(w, r, name)
}

func (s *server) hWallpaperDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !wallpaperName.MatchString(name) {
		writeJSON(w, 400, map[string]string{"error": "nome inválido"})
		return
	}
	if err := os.Remove(filepath.Join(s.wallpaperDir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		writeJSON(w, 500, map[string]string{"error": "não foi possível apagar"})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ── Estatísticas ao vivo ──────────────────────────────────────────

func (s *server) hStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"snapshot": s.stats.Latest(), "history": s.stats.History()})
}

// hStatsStream envia uma amostra a cada ~2 s via Server-Sent Events enquanto a aba estiver aberta.
func (s *server) hStatsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming indisponível"})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	ch, unsub := s.stats.Subscribe()
	defer unsub()
	evs, unsubEv := s.events.Subscribe() // avisos novos vão pela mesma conexão (event: notify)
	defer unsubEv()
	mets, unsubMet := s.integ.Subscribe() // valores das integrações (event: metrics); aba aberta = buscar
	defer unsubMet()
	nts, unsubNotes := s.notes.Subscribe() // nota mudou em outro aparelho (event: note)
	defer unsubNotes()
	sid := session(r).ID
	first, _ := json.Marshal(map[string]any{"snapshot": s.stats.Latest(), "history": s.stats.History(), "metrics": s.integ.Snapshot()})
	fmt.Fprintf(w, "retry: 3000\nevent: init\ndata: %s\n\n", first)
	flusher.Flush()
	keep := time.NewTicker(25 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if s.auth.IsLocked(sid) { // bloqueou (ou saiu) com a conexão aberta: para de enviar
				fmt.Fprint(w, "event: locked\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case n := <-nts:
			b, _ := json.Marshal(n)
			fmt.Fprintf(w, "event: note\ndata: %s\n\n", b)
			flusher.Flush()
		case e := <-evs:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "event: notify\ndata: %s\n\n", b)
			flusher.Flush()
		case b := <-mets:
			fmt.Fprintf(w, "event: metrics\ndata: %s\n\n", b)
			flusher.Flush()
		case <-keep.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *server) hSystem(w http.ResponseWriter, r *http.Request) {
	_, tipi := s.tipi.Apps()
	dockerOK := false
	if s.docker != nil {
		_, err := s.containers(r.Context())
		dockerOK = err == nil
	}
	writeJSON(w, 200, map[string]any{
		"info": s.stats.Info(), "runtipi": tipi, "docker": dockerOK, "dockerActions": s.cfg.DockerActions && dockerOK, "terminal": s.term != nil,
		"version": version, "startedAt": s.started.Unix(), "maxUploadGB": s.cfg.MaxUploadGB,
	})
}

// ── Apps do Runtipi + status dos containers ───────────────────────

func (s *server) containers(ctx context.Context) ([]dockerc.Container, error) {
	if s.docker == nil {
		return nil, errors.New("docker desligado")
	}
	s.ctMu.Lock()
	defer s.ctMu.Unlock()
	if time.Since(s.ctAt) < 3*time.Second && s.ctCache != nil {
		return s.ctCache, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cts, err := s.docker.Containers(ctx)
	if err != nil {
		return nil, err
	}
	s.ctCache, s.ctAt = cts, time.Now()
	return cts, nil
}

type appOut struct {
	runtipi.App
	State      string          `json:"state"` // running | stopped | partial | unknown
	Containers int             `json:"containers"`
	Running    int             `json:"running"`
	Health     string          `json:"health"`           // up | down | unknown (monitor de disponibilidade)
	Uptime     *float64        `json:"uptime,omitempty"` // % das últimas 24 h
	Update     *runtipi.Update `json:"update,omitempty"` // atualização na loja (conferida 1×/dia)
}

func (s *server) hApps(w http.ResponseWriter, r *http.Request) {
	apps, info := s.tipi.Apps()
	cts, err := s.containers(r.Context())
	byProject := map[string][2]int{}
	if err == nil {
		for _, c := range cts {
			p := c.Project()
			v := byProject[p]
			v[0]++
			if c.State == "running" {
				v[1]++
			}
			byProject[p] = v
		}
	}
	out := make([]appOut, 0, len(apps))
	for _, a := range apps {
		o := appOut{App: a, State: "unknown"}
		if err == nil {
			v := byProject[a.Project]
			o.Containers, o.Running = v[0], v[1]
			switch {
			case v[0] == 0 || v[1] == 0:
				o.State = "stopped"
			case v[1] < v[0]:
				o.State = "partial"
			default:
				o.State = "running"
			}
		}
		o.Health = healthName(s.appmon.Health(a.Project))
		if pct, ok := s.appmon.Uptime(a.Project); ok {
			o.Uptime = &pct
		}
		if u, ok := s.updates.For(a.URN); ok {
			o.Update = &u
		}
		out = append(out, o)
	}
	writeJSON(w, 200, map[string]any{"apps": out, "runtipi": info, "docker": err == nil, "dockerActions": s.cfg.DockerActions && err == nil})
}

func (s *server) hAppLogo(w http.ResponseWriter, r *http.Request) {
	p, ok := s.tipi.LogoPath(r.URL.Query().Get("urn"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, p)
}

type appUsage struct {
	URN     string  `json:"urn"`
	Name    string  `json:"name"`
	Project string  `json:"project"`
	CPU     float64 `json:"cpu"`
	Memory  uint64  `json:"memory"`
}

// hUsage soma CPU e memória dos containers de cada app (calcula quando alguém pede, e o vigia de
// apps também chama a cada minuto para o histórico de 24 h).
func (s *server) hUsage(w http.ResponseWriter, r *http.Request) {
	out, err := s.collectUsage(r.Context())
	if err != nil {
		out = []appUsage{}
	}
	writeJSON(w, 200, out)
}

func (s *server) collectUsage(ctx context.Context) ([]appUsage, error) {
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	if time.Since(s.usageAt) < 2500*time.Millisecond && s.usage != nil {
		return s.usage, nil
	}
	cts, err := s.containers(ctx)
	if err != nil {
		return nil, err
	}
	apps, _ := s.tipi.Apps()
	names := map[string]runtipi.App{}
	for _, a := range apps {
		names[a.Project] = a
	}
	type res struct {
		project string
		st      dockerc.Stats
	}
	var running []dockerc.Container
	alive := map[string]bool{}
	for _, c := range cts {
		if c.State == "running" {
			running = append(running, c)
			alive[c.ID] = true
		}
	}
	s.docker.Forget(alive)
	results := make(chan res, len(running))
	sem := make(chan struct{}, 4) // no máximo 4 consultas ao Docker ao mesmo tempo
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	for _, c := range running {
		wg.Add(1)
		go func(c dockerc.Container) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if st, err := s.docker.Stats(ctx, c.ID); err == nil {
				p := c.Project()
				if p == "" {
					p = c.Name()
				}
				results <- res{p, st}
			}
		}(c)
	}
	wg.Wait()
	close(results)
	agg := map[string]*appUsage{}
	for rr := range results {
		u, ok := agg[rr.project]
		if !ok {
			name := rr.project
			urn := ""
			if a, ok := names[rr.project]; ok {
				name, urn = a.Name, a.URN
			} else if rr.project == "runtipi" {
				name = "Runtipi (sistema)"
			}
			u = &appUsage{URN: urn, Name: name, Project: rr.project}
			agg[rr.project] = u
		}
		u.CPU += rr.st.CPU
		u.Memory += rr.st.Memory
	}
	out := make([]appUsage, 0, len(agg))
	for _, u := range agg {
		u.CPU = float64(int64(u.CPU*100+0.5)) / 100
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CPU != out[j].CPU {
			return out[i].CPU > out[j].CPU
		}
		return out[i].Memory > out[j].Memory
	})
	s.usage, s.usageAt = out, time.Now()
	return out, nil
}
