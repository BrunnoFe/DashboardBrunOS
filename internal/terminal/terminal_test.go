package terminal

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"brunos/internal/store"
)

// servidor SSH de teste: aceita teste/segredo, abre um "shell" que ecoa o que recebe
// e responde ao redimensionamento com "size=COLSxROWS".
func testServer(t *testing.T, hostKey ssh.Signer) (host string, port int) {
	t.Helper()
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == "teste" && string(pw) == "segredo" {
			return nil, nil
		}
		return nil, errors.New("negado")
	}, PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if c.User() == "teste" && authorizedKey != nil && string(k.Marshal()) == string(authorizedKey.Marshal()) {
			return nil, nil
		}
		return nil, errors.New("negado")
	}}
	cfg.AddHostKey(hostKey)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					ch, creqs, _ := nch.Accept()
					go func() {
						for r := range creqs {
							switch r.Type {
							case "pty-req", "shell":
								r.Reply(true, nil)
								if r.Type == "shell" {
									io.WriteString(ch, "bem-vindo\r\n")
								}
							case "window-change":
								var wc struct{ Cols, Rows, W, H uint32 }
								ssh.Unmarshal(r.Payload, &wc)
								fmt.Fprintf(ch, "size=%dx%d\r\n", wc.Cols, wc.Rows)
							default:
								r.Reply(false, nil)
							}
						}
					}()
					go func() {
						buf := make([]byte, 1024)
						for {
							n, err := ch.Read(buf)
							if n > 0 {
								s := string(buf[:n])
								if strings.Contains(s, "exit") {
									ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{3}))
									ch.Close()
									return
								}
								io.WriteString(ch, "eco:"+s)
							}
							if err != nil {
								return
							}
						}
					}()
				}
			}()
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

func newKey(t *testing.T) ssh.Signer {
	_, pk, _ := ed25519.GenerateKey(rand.Reader)
	s, err := ssh.NewSignerFromKey(pk)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func readUntil(t *testing.T, s *Session, gen int, want string) string {
	t.Helper()
	var got strings.Builder
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-s.Notify():
		case <-deadline:
			t.Fatalf("esperava %q, chegou %q", want, got.String())
		}
		out, _, _, _ := s.Next(gen)
		got.Write(out)
		if strings.Contains(got.String(), want) {
			return got.String()
		}
	}
}

func TestTerminalTOFUAndIO(t *testing.T) {
	key := newKey(t)
	host, port := testServer(t, key)
	dir := t.TempDir()
	m := NewManager(filepath.Join(dir, "known_hosts.json"))
	req := OpenRequest{Host: host, Port: port, User: "teste", Password: "segredo", Cols: 90, Rows: 25}

	// 1. primeira vez: pede confirmação da impressão digital
	_, err := m.Open(req)
	var unk *UnknownHostError
	if !errors.As(err, &unk) || unk.Fingerprint != ssh.FingerprintSHA256(key.PublicKey()) {
		t.Fatalf("esperava UnknownHostError com a impressão certa, veio %v", err)
	}
	if _, err := os.Stat(m.KnownFile); err == nil {
		t.Fatal("não deveria gravar a chave antes da confirmação")
	}
	// 2. confirmação errada continua recusando
	req.Trust = "SHA256:outra"
	if _, err := m.Open(req); !errors.As(err, &unk) {
		t.Fatalf("impressão errada deveria recusar, veio %v", err)
	}
	// 3. senha errada
	req.Trust = unk.Fingerprint
	bad := req
	bad.Password = "errada"
	if _, err := m.Open(bad); !errors.Is(err, ErrAuth) {
		t.Fatalf("senha errada deveria dar ErrAuth, veio %v", err)
	}
	// 4. conecta, grava a chave com permissão 0600 e troca dados
	s, err := m.Open(req)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(m.KnownFile)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("known_hosts.json deveria existir com 0600: %v %v", st, err)
	}
	raw, _ := os.ReadFile(m.KnownFile)
	if strings.Contains(string(raw), "segredo") {
		t.Fatal("a senha não pode ir para o disco")
	}
	gen, _, detach := s.Attach(false)
	readUntil(t, s, gen, "bem-vindo")
	if err := s.Write("ls -la\r"); err != nil {
		t.Fatal(err)
	}
	readUntil(t, s, gen, "eco:ls -la")
	if err := s.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	readUntil(t, s, gen, "size=120x40")

	// 5. um segundo leitor substitui o primeiro; com replay recebe o fim da tela
	gen2, hist, detach2 := s.Attach(true)
	if _, _, _, stale := s.Next(gen); !stale {
		t.Fatal("o leitor antigo deveria ficar obsoleto")
	}
	if !strings.Contains(string(hist), "bem-vindo") || !strings.Contains(string(hist), "size=120x40") {
		t.Fatalf("replay deveria trazer a tela até agora: %q", hist)
	}
	detach()

	// 6. o shell sai: sessão termina com o código
	s.Write("exit\r")
	deadline := time.After(5 * time.Second)
wait:
	for {
		select {
		case <-s.Notify():
		case <-deadline:
			t.Fatal("a sessão não terminou")
		}
		_, done, msg, _ := s.Next(gen2)
		if done {
			if !strings.Contains(msg, "3") {
				t.Fatalf("mensagem de saída sem o código: %q", msg)
			}
			break wait
		}
	}
	detach2()
	m.Close(s.ID)
	if len(m.List()) != 0 {
		t.Fatal("sessão deveria ter saído da lista")
	}

	// 7. sem pedir de novo na segunda conexão (chave já confirmada)
	req.Trust = ""
	s2, err := m.Open(req)
	if err != nil {
		t.Fatalf("segunda conexão deveria usar a chave guardada: %v", err)
	}
	m.CloseAll()
	if _, err := m.Get(s2.ID); !errors.Is(err, ErrNoSession) {
		t.Fatal("CloseAll deveria encerrar tudo")
	}
}

func TestTerminalHostKeyChanged(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(filepath.Join(dir, "known_hosts.json"))
	key1 := newKey(t)
	host, port := testServer(t, key1)
	req := OpenRequest{Host: host, Port: port, User: "teste", Password: "segredo", Trust: ssh.FingerprintSHA256(key1.PublicKey())}
	s, err := m.Open(req)
	if err != nil {
		t.Fatal(err)
	}
	m.Close(s.ID)

	// outro servidor com outra chave, mas registrado com a chave antiga: simula um
	// servidor trocado (ou alguém se passando por ele)
	key2 := newKey(t)
	host2, port2 := testServer(t, key2)
	k, _ := m.loadKnown()
	k.Hosts[net.JoinHostPort(host2, strconv.Itoa(port2))] = k.Hosts[net.JoinHostPort(host, strconv.Itoa(port))]
	if err := store.WriteJSON(m.KnownFile, k, 0o600); err != nil {
		t.Fatal(err)
	}
	req2 := OpenRequest{Host: host2, Port: port2, User: "teste", Password: "segredo", Trust: ssh.FingerprintSHA256(key2.PublicKey())}
	_, err = m.Open(req2)
	var mis *HostKeyMismatchError
	if !errors.As(err, &mis) {
		t.Fatalf("chave trocada deveria ser recusada mesmo com trust, veio %v", err)
	}
	// depois de "esquecer", aceita a nova com confirmação
	if err := m.Forget(host2, port2); err != nil {
		t.Fatal(err)
	}
	s2, err := m.Open(req2)
	if err != nil {
		t.Fatalf("depois de esquecer, deveria aceitar a nova chave confirmada: %v", err)
	}
	m.Close(s2.ID)
}

// authorizedKey é a chave pública que o servidor de teste aceita (Chave do Terminal).
var authorizedKey ssh.PublicKey

func TestTerminalWithKey(t *testing.T) {
	host, port := testServer(t, newKey(t))
	tk := newKey(t)
	authorizedKey = tk.PublicKey()
	t.Cleanup(func() { authorizedKey = nil })
	m := NewManager(filepath.Join(t.TempDir(), "known_hosts.json"))
	// confirma o servidor com senha e depois entra só com a chave
	_, err := m.Open(OpenRequest{Host: host, Port: port, User: "teste", Signer: tk})
	var unk *UnknownHostError
	if !errors.As(err, &unk) {
		t.Fatalf("esperava pedir confirmação: %v", err)
	}
	s, err := m.Open(OpenRequest{Host: host, Port: port, User: "teste", Signer: tk, Trust: unk.Fingerprint, Owner: "sessaoA"})
	if err != nil {
		t.Fatalf("a Chave do Terminal deveria entrar: %v", err)
	}
	if _, err := m.Open(OpenRequest{Host: host, Port: port, User: "teste", Signer: newKey(t)}); !errors.Is(err, ErrAuth) {
		t.Fatalf("outra chave deveria ser recusada: %v", err)
	}
	s2, _ := m.Open(OpenRequest{Host: host, Port: port, User: "teste", Signer: tk, Owner: "sessaoB"})
	m.CloseOwner("sessaoA")
	if _, err := m.Get(s.ID); !errors.Is(err, ErrNoSession) {
		t.Fatal("CloseOwner deveria fechar as Abas daquela Sessão")
	}
	if _, err := m.Get(s2.ID); err != nil {
		t.Fatal("as Abas de outra Sessão continuam")
	}
	m.CloseAll()
}

func TestParseBusy(t *testing.T) {
	for _, c := range []struct {
		out   string
		known bool
		names string
	}{
		{"", false, ""},               // ps falhou
		{"OK\n", false, ""},           // não achou o shell
		{"OK\nS 120 120\n", true, ""}, // só o prompt
		{"OK\nS 120 455\nF apt\nF dpkg\nF apt\n", true, "apt,dpkg"}, // comando rodando
		{"OK\nS 120 120\nB rsync\n", true, "rsync"},                 // segundo plano (comando &)
		{"OK\nS 120 300\nF sudo\nF -bash\n", true, ""},              // sudo -i parado no prompt
	} {
		b := parseBusy(c.out)
		if b.Known != c.known || strings.Join(b.Names, ",") != c.names {
			t.Errorf("%q: veio %+v", c.out, b)
		}
	}
}

// Sem ninguém olhando: com algo rodando não fecha; quando termina avisa e, com o tempo esgotado, fecha.
// Sessão do BrunOS bloqueada: não fecha.
func TestTerminalIdleAndBusy(t *testing.T) {
	key := newKey(t)
	host, port := testServer(t, key)
	m := NewManager(filepath.Join(t.TempDir(), "known_hosts.json"))
	close(m.stop) // as varreduras deste teste são chamadas à mão
	running := []string{"apt"}
	var mu = make(chan struct{}, 1)
	mu <- struct{}{}
	m.busyFn = func(*Session) Busy {
		<-mu
		defer func() { mu <- struct{}{} }()
		return Busy{Known: true, Names: running}
	}
	var doneNames []string
	doneCh := make(chan struct{}, 4)
	m.OnDone = func(addr, user string, tab int, names []string) { doneNames = names; doneCh <- struct{}{} }
	var paused atomic.Bool
	m.Paused = func(string) bool { return paused.Load() }
	m.IdleTimeout = func() time.Duration { return time.Minute }

	s, err := m.Open(OpenRequest{Host: host, Port: port, User: "teste", Password: "segredo", Trust: ssh.FingerprintSHA256(key.PublicKey())})
	if err != nil {
		t.Fatal(err)
	}
	_, _, detach := s.Attach(false)
	detach() // "minimizou"
	waitCheck := func() {
		t.Helper()
		for range 200 {
			s.mu.Lock()
			c := s.checking
			s.mu.Unlock()
			if !c {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("a conferência não terminou")
	}
	base := time.Now()
	// 1. passou o tempo de "recarregar a página": confere e descobre o apt
	m.sweep(base.Add(detachGrace + time.Second))
	waitCheck()
	if l := m.List(); len(l) != 1 || strings.Join(l[0].Running, ",") != "apt" {
		t.Fatalf("deveria saber que o apt roda: %+v", l)
	}
	// 2. tempo esgotado, mas o apt roda: não fecha
	s.mu.Lock()
	s.lastSeen, s.lastCheck = base.Add(-2*time.Minute), base.Add(-2*time.Minute)
	s.mu.Unlock()
	m.sweep(base)
	waitCheck()
	if _, err := m.Get(s.ID); err != nil {
		t.Fatal("com algo rodando a Aba não pode fechar")
	}
	// 3. Sessão bloqueada e o apt terminou: avisa, mas não fecha
	paused.Store(true)
	<-mu
	running = nil
	mu <- struct{}{}
	s.mu.Lock()
	s.lastCheck = base.Add(-2 * time.Minute)
	s.mu.Unlock()
	m.sweep(base)
	waitCheck()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("deveria avisar que o apt terminou")
	}
	if strings.Join(doneNames, ",") != "apt" {
		t.Fatalf("aviso com %v", doneNames)
	}
	if _, err := m.Get(s.ID); err != nil {
		t.Fatal("Sessão bloqueada: a Aba não fecha")
	}
	// 4. desbloqueou: tempo esgotado e nada rodando → fecha
	paused.Store(false)
	s.mu.Lock()
	s.lastCheck = base.Add(-2 * time.Minute)
	s.mu.Unlock()
	m.sweep(base)
	waitCheck()
	for range 100 {
		if _, err := m.Get(s.ID); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("deveria fechar por inatividade")
}

func TestTerminalValidation(t *testing.T) {
	m := NewManager("")
	for _, req := range []OpenRequest{
		{Host: "", User: "a", Password: "x"},
		{Host: "host;rm -rf", User: "a", Password: "x"},
		{Host: "127.0.0.1", User: "", Password: "x"},
		{Host: "127.0.0.1", User: "a b", Password: "x"},
		{Host: "127.0.0.1", User: "a", Port: 70000, Password: "x"},
		{Host: "127.0.0.1", User: "a"}, // sem senha nem chave
	} {
		if _, err := m.Open(req); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v deveria ser recusado como inválido, veio %v", req, err)
		}
	}
	if _, err := m.Open(OpenRequest{Host: "127.0.0.1", User: "a", Key: "não é uma chave"}); !errors.Is(err, ErrBadKey) {
		t.Errorf("chave inválida deveria dar ErrBadKey, veio %v", err)
	}
}
