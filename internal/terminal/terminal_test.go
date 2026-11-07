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
	gen, detach := s.Attach()
	readUntil(t, s, gen, "bem-vindo")
	if err := s.Write("ls -la\r"); err != nil {
		t.Fatal(err)
	}
	readUntil(t, s, gen, "eco:ls -la")
	if err := s.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	readUntil(t, s, gen, "size=120x40")

	// 5. um segundo leitor substitui o primeiro
	gen2, detach2 := s.Attach()
	if _, _, _, stale := s.Next(gen); !stale {
		t.Fatal("o leitor antigo deveria ficar obsoleto")
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
