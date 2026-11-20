package files

// Revisão de segurança, lote 3: ocultos da pasta pessoal só leitura e segredos fora do Arquivos.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func names(t *testing.T, s *Service, rel string) []string {
	t.Helper()
	es, err := s.List("h", rel)
	if err != nil {
		t.Fatalf("listar %s: %v", rel, err)
	}
	out := []string{}
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

func has(list []string, n string) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

func TestOcultosDaPastaPessoalSoLeitura(t *testing.T) {
	s, dir := newSvc(t)
	put(t, filepath.Join(dir, "usuario/.ssh/authorized_keys"), "ssh-ed25519 AAAA dono")
	put(t, filepath.Join(dir, "usuario/.bashrc"), "alias ll='ls -l'\n")
	put(t, filepath.Join(dir, "usuario/Documentos/nota.txt"), "oi")
	if err := os.Symlink(filepath.Join(dir, "usuario/.ssh"), filepath.Join(dir, "usuario/atalho")); err != nil {
		t.Fatal(err)
	}

	// ler continua funcionando
	f, _, err := s.Open("h", "usuario/.bashrc")
	if err != nil {
		t.Fatalf("ler .bashrc: %v", err)
	}
	f.Close()

	prot := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, ErrProtected) {
			t.Errorf("%s: esperava ErrProtected, veio %v", what, err)
		}
	}
	_, err = s.SaveText("h", "usuario/.bashrc", strings.NewReader("curl mau | sh"), 0)
	prot("salvar .bashrc", err)
	_, err = s.Mkdir("h", "usuario/.ssh", "x")
	prot("criar pasta em .ssh", err)
	_, err = s.Mkdir("h", "usuario/atalho", "x")
	prot("criar pasta em .ssh por um atalho", err)
	_, err = s.NewFile("h", "usuario", ".profile")
	prot("novo arquivo oculto", err)
	prot("renomear oculto", s.Rename("h", "usuario/.bashrc", "bashrc"))
	prot("renomear para oculto", s.Rename("h", "usuario/Documentos", ".Documentos"))
	prot("apagar oculto", s.Delete("h", []string{"usuario/.ssh/authorized_keys"}))
	prot("lixeira de oculto", s.Trash("h", []string{"usuario/.bashrc"}))
	prot("colar em .ssh", s.Paste("h", []string{"usuario/Documentos/nota.txt"}, "h", "usuario/.ssh", true))
	prot("mover oculto para fora", s.Paste("h", []string{"usuario/.bashrc"}, "h", "usuario/Documentos", false))
	_, err = s.StartUpload("h", "usuario/.ssh", "authorized_keys", 3)
	prot("envio em pedaços para .ssh", err)
	_, err = s.StartUpload("h", "usuario", ".ssh/rc", 3)
	prot("envio de pasta para dentro de .ssh", err)

	if b, _ := os.ReadFile(filepath.Join(dir, "usuario/.ssh/authorized_keys")); string(b) != "ssh-ed25519 AAAA dono" {
		t.Fatalf("authorized_keys mudou: %q", b)
	}
	// oculto mais fundo (dentro de Documentos) e cópia para fora continuam liberados
	if _, err := s.Mkdir("h", "usuario/Documentos", ".rascunhos"); err != nil {
		t.Fatalf("oculto dentro de Documentos deveria poder: %v", err)
	}
	if err := s.Paste("h", []string{"usuario/.bashrc"}, "h", "usuario/Documentos", true); err != nil {
		t.Fatalf("copiar um oculto para fora deveria poder: %v", err)
	}
}

func TestSegredosSomemDoArquivos(t *testing.T) {
	s, dir := newSvc(t)
	tipi := filepath.Join(dir, "runtipi")
	data := filepath.Join(tipi, "app-data/brunos/brunos/data")
	put(t, filepath.Join(data, "hostcmd_key_v2"), "CHAVE-DO-SCRIPT")
	put(t, filepath.Join(tipi, ".env"), "SENHA_DO_RUNTIPI=x")
	put(t, filepath.Join(tipi, "app-data/migrated/syncthing/app.env"), "SENHA=x")
	put(t, filepath.Join(tipi, "app-data/migrated/syncthing/data/ok.txt"), "dados do app")
	put(t, filepath.Join(tipi, "user-config/migrated/syncthing/app.env"), "SENHA=y")
	put(t, filepath.Join(dir, "usuario/.ssh/id_ed25519"), "CHAVE-PRIVADA")
	put(t, filepath.Join(dir, "usuario/.ssh/id_ed25519.pub"), "ssh-ed25519 AAAA")
	if err := os.Symlink(data, filepath.Join(dir, "usuario/dados")); err != nil {
		t.Fatal(err)
	}
	s.Protect(data, tipi)

	if n := names(t, s, "usuario/.ssh"); has(n, "id_ed25519") || !has(n, "id_ed25519.pub") {
		t.Errorf("lista de .ssh: %v", n)
	}
	if n := names(t, s, "runtipi"); has(n, ".env") {
		t.Errorf("o .env do Runtipi apareceu: %v", n)
	}
	if n := names(t, s, "runtipi/app-data/brunos/brunos"); has(n, "data") {
		t.Errorf("a pasta de dados do BrunOS apareceu: %v", n)
	}
	if n := names(t, s, "runtipi/app-data/migrated/syncthing"); has(n, "app.env") || !has(n, "data") {
		t.Errorf("pasta do app: %v", n)
	}
	for _, rel := range []string{"runtipi/app-data/brunos/brunos/data", "usuario/dados"} {
		if _, err := s.List("h", rel); !errors.Is(err, ErrNotFound) {
			t.Errorf("listar %s: esperava ErrNotFound, veio %v", rel, err)
		}
	}
	for _, rel := range []string{"usuario/.ssh/id_ed25519", "runtipi/.env", "runtipi/app-data/migrated/syncthing/app.env",
		"runtipi/user-config/migrated/syncthing/app.env", "usuario/dados/hostcmd_key_v2", "runtipi/app-data/brunos/brunos/data/hostcmd_key_v2"} {
		if f, _, err := s.Open("h", rel); !errors.Is(err, ErrNotFound) {
			if f != nil {
				f.Close()
			}
			t.Errorf("abrir %s: esperava ErrNotFound, veio %v", rel, err)
		}
	}
	if f, _, err := s.Open("h", "runtipi/app-data/migrated/syncthing/data/ok.txt"); err != nil {
		t.Errorf("os dados comuns do app deveriam abrir: %v", err)
	} else {
		f.Close()
	}

	hits, _, err := s.Search("h", "", "e", 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Name == "app.env" || h.Name == ".env" || h.Name == "id_ed25519" || strings.Contains(h.Path, "brunos/brunos/data") {
			t.Errorf("a busca achou um segredo: %s", h.Path)
		}
	}

	// copiar a pasta do app deixa o app.env para trás; copiar o segredo direto é recusado
	if err := s.Paste("h", []string{"runtipi/app-data/migrated"}, "h", "usuario", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "usuario/migrated/syncthing/data/ok.txt")); err != nil {
		t.Errorf("a cópia não trouxe os dados comuns: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "usuario/migrated/syncthing/app.env")); err == nil {
		t.Error("a cópia trouxe o app.env (senhas)")
	}
	if err := s.Paste("h", []string{"usuario/.ssh/id_ed25519"}, "h", "usuario", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("copiar a chave privada: esperava ErrNotFound, veio %v", err)
	}
	if err := s.Paste("h", []string{"usuario/dados"}, "h", "usuario/Documentos", true); err == nil {
		if _, err := os.Stat(filepath.Join(dir, "usuario/Documentos/dados/hostcmd_key_v2")); err == nil {
			t.Error("copiar o atalho para a pasta de dados trouxe a chave do script")
		}
	}

	u, err := s.Usage("h", "runtipi/app-data/brunos", true)
	if err != nil {
		t.Fatal(err)
	}
	if u.Size != 0 {
		t.Errorf("o mapa do espaço contou a pasta de dados do BrunOS: %d bytes", u.Size)
	}
}
