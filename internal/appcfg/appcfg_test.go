package appcfg

import (
	"strings"
	"testing"
)

// arquivo de ajustes "feito à mão" no editor do Runtipi (nomes genéricos)
const manual = `# pastas dos filmes (feito à mão)
services:
  plex:
    volumes:
      - /mnt/disco1/Filmes:/movies # filmes
      - /mnt/disco2/Series:/tv:ro
      - cache:/transcode
    environment:
      PLEX_CLAIM: claim-xyz
    labels:
      minha.etiqueta: "1"
    devices:
      - /dev/dri:/dev/dri
  outro:
    image: exemplo/outro:1
`

func TestParseManual(t *testing.T) {
	st, err := Parse([]byte(manual), "plex")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Exists || len(st.Mounts) != 2 || st.Mounts[0] != (Mount{"/mnt/disco1/Filmes", "/movies", false}) || !st.Mounts[1].ReadOnly {
		t.Fatalf("pastas: %+v", st.Mounts)
	}
	if !st.GPU || len(st.Env) != 1 || st.Env[0].Key != "PLEX_CLAIM" {
		t.Fatalf("gpu/env: %+v", st)
	}
	want := map[string]bool{"labels": true, "volume cache:/transcode": true, "ajustes do serviço outro": true}
	for _, m := range st.Manual {
		delete(want, m)
	}
	if len(want) != 0 || len(st.Risks) != 0 {
		t.Fatalf("feito à mão: %v riscos: %v", st.Manual, st.Risks)
	}
}

func TestApplyPreservaOQueFoiFeitoAMao(t *testing.T) {
	st, _ := Parse([]byte(manual), "plex")
	s := st.Settings
	s.Mounts = append(s.Mounts, Mount{Host: "/mnt/disco1/Musicas", Container: "/music", ReadOnly: true})
	s.MemoryMB, s.CPUs = 2048, 1.5
	out, err := Apply([]byte(manual), "plex", s)
	if err != nil {
		t.Fatal(err)
	}
	o := string(out)
	for _, must := range []string{"# pastas dos filmes (feito à mão)", "/mnt/disco1/Filmes:/movies # filmes", "/mnt/disco2/Series:/tv:ro",
		"cache:/transcode", "PLEX_CLAIM: claim-xyz", "minha.etiqueta", "outro:", "image: exemplo/outro:1", "/mnt/disco1/Musicas:/music:ro",
		"memory: 2G", "cpus: \"1.5\"", "/dev/dri:/dev/dri"} {
		if !strings.Contains(o, must) {
			t.Errorf("faltou %q em:\n%s", must, o)
		}
	}
	// e lendo de novo dá o mesmo
	st2, err := Parse(out, "plex")
	if err != nil || len(st2.Mounts) != 3 || st2.MemoryMB != 2048 || st2.CPUs != 1.5 || !st2.GPU {
		t.Fatalf("relido: %+v %v", st2, err)
	}
}

func TestApplyTiraLimitesEApagaSeVazio(t *testing.T) {
	out, err := Apply(nil, "app", Settings{Mounts: []Mount{{"/mnt/disco1/x", "/x", false}}, MemoryMB: 512, GPU: true, Env: []Var{{"TZ", "America/Sao_Paulo"}}})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := Parse(out, "app")
	if st.MemoryMB != 512 || !st.GPU || len(st.Env) != 1 || len(st.Mounts) != 1 {
		t.Fatalf("novo: %+v\n%s", st, out)
	}
	vazio, err := Apply(out, "app", Settings{})
	if err != nil || len(vazio) != 0 {
		t.Fatalf("tudo tirado deveria dar arquivo vazio: %q %v", vazio, err)
	}
	// mem_limit escrito à mão continua sendo onde o limite fica
	out, _ = Apply([]byte("services:\n  app:\n    mem_limit: 1g\n"), "app", Settings{MemoryMB: 4096})
	if !strings.Contains(string(out), "mem_limit: 4G") || strings.Contains(string(out), "deploy") {
		t.Fatalf("mem_limit: %s", out)
	}
}

func TestSeguranca(t *testing.T) {
	ruins := []Mount{{"/", "/x", false}, {"/etc", "/x", false}, {"/var/run/docker.sock", "/x", false}, {"/mnt/../etc", "/x", false},
		{"/mnt/disco1/", "/x", false}, {"/root", "/x", false}, {"relativo", "/x", false}, {"/mnt/a:b", "/x", false}, {"${HOME}/x", "/x", false}}
	for _, m := range ruins {
		if _, err := Apply(nil, "app", Settings{Mounts: []Mount{m}}); err == nil {
			t.Errorf("pasta %q deveria ser recusada", m.Host)
		}
	}
	for _, dst := range []string{"/", "x", "/a/../b", "/a b"} {
		if _, err := Apply(nil, "app", Settings{Mounts: []Mount{{"/mnt/disco1/x", dst, false}}}); err == nil {
			t.Errorf("destino %q deveria ser recusado", dst)
		}
	}
	if _, err := Apply(nil, "app", Settings{Env: []Var{{"A B", "x"}}}); err == nil {
		t.Error("nome de variável estranho deveria ser recusado")
	}
	if _, err := Apply(nil, "app", Settings{Env: []Var{{"A", "linha\nnova"}}}); err == nil {
		t.Error("valor com quebra de linha deveria ser recusado")
	}
	for _, ok := range []string{"/mnt/disco1/Filmes", "/home/usuario/Fotos", "${ROOT_FOLDER_HOST}/media/data/movies", "${APP_DATA_DIR}/data/extra"} {
		if _, err := Apply(nil, "app", Settings{Mounts: []Mount{{ok, "/x", false}}}); err != nil {
			t.Errorf("pasta %q deveria passar: %v", ok, err)
		}
	}
	// coisa perigosa escrita à mão continua (é do Bruno), mas os Ajustes não acrescentam outra
	feito := "services:\n  app:\n    network_mode: host\n    volumes:\n      - /etc/minha:/cfg\n"
	st, _ := Parse([]byte(feito), "app")
	if len(st.Risks) != 2 {
		t.Fatalf("riscos: %v", st.Risks)
	}
	s := st.Settings
	s.MemoryMB = 1024
	out, err := Apply([]byte(feito), "app", s)
	if err != nil || !strings.Contains(string(out), "network_mode: host") || !strings.Contains(string(out), "/etc/minha:/cfg") {
		t.Fatalf("o feito à mão deveria continuar: %v\n%s", err, out)
	}
	s.Mounts = append(s.Mounts, Mount{"/etc/outra", "/c2", false})
	if _, err := Apply([]byte(feito), "app", s); err == nil {
		t.Fatal("pasta perigosa nova não pode entrar")
	}
	if _, err := Parse([]byte("services: [1, 2"), "app"); err == nil {
		t.Fatal("YAML quebrado deveria dar erro")
	}
	if _, err := Parse([]byte("- uma lista"), "app"); err == nil {
		t.Fatal("arquivo que não é compose deveria dar erro")
	}
}

func TestParseMemory(t *testing.T) {
	for in, want := range map[string]int{"2G": 2048, "512m": 512, "1.5GB": 1536, "1073741824": 1024, "1gi": 1024, "x": 0} {
		if got := ParseMemory(in); got != want {
			t.Errorf("ParseMemory(%q) = %d, queria %d", in, got, want)
		}
	}
}
