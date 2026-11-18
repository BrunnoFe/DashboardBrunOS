package appcfg

import (
	"strings"
	"testing"
)

func TestRecipeComum(t *testing.T) {
	yml := `services:
  jellyfin:
    image: lscr.io/linuxserver/jellyfin:10.9.1
    restart: unless-stopped
    environment:
      TZ: ${TZ}
      PUID: "1000"
    volumes:
      - ${APP_DATA_DIR}/data/config:/config
      - ${ROOT_FOLDER_HOST}/media/data:/media:ro
      - /etc/localtime:/etc/localtime:ro
      - cache:/cache
    devices:
      - /dev/dri:/dev/dri
    ports:
      - 7359:7359/udp
    x-runtipi:
      is_main: true
`
	r, err := ParseRecipe([]byte(yml), false, "jellyfin")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Privileges) != 0 {
		t.Fatalf("receita comum não requer privilégios: %v", r.Privileges)
	}
	if r.Main != "jellyfin" || r.Image != "lscr.io/linuxserver/jellyfin:10.9.1" || r.Restart != "unless-stopped" || len(r.Env) != 2 ||
		len(r.Mounts) != 3 || len(r.Ports) != 1 || r.Ports[0] != "7359:7359/udp" {
		t.Fatalf("receita: %+v", r)
	}
	if r.Mounts[1].Container != "/media" || !r.Mounts[1].ReadOnly {
		t.Fatalf("pasta: %+v", r.Mounts[1])
	}
}

func TestRecipePrivilegios(t *testing.T) {
	yml := `services:
  vpn:
    image: x/vpn
    cap_add: [NET_ADMIN, CHOWN]
    devices: ["/dev/net/tun:/dev/net/tun", "/dev/dri:/dev/dri"]
  agent:
    image: x/agent
    privileged: true
    network_mode: host
    pid: host
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /etc:/host/etc:ro
`
	r, err := ParseRecipe([]byte(yml), false, "vpn")
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(r.Privileges, "|")
	for _, want := range []string{"NET_ADMIN", "/dev/net/tun", "privileged", "network_mode: host", "pid: host", "socket do Docker", "pasta /etc"} {
		if !strings.Contains(all, want) {
			t.Errorf("faltou %q em %v", want, r.Privileges)
		}
	}
	if strings.Contains(all, "CHOWN") || strings.Contains(all, "/dev/dri") {
		t.Errorf("CHOWN e a placa de vídeo não são privilégios: %v", r.Privileges)
	}
}

func TestRecipeJSON(t *testing.T) {
	js := `{"schemaVersion":2,"services":[
	  {"name":"homeassistant","image":"ghcr.io/home-assistant/home-assistant:2024.1","isMain":true,"internalPort":8123,
	   "networkMode":"host","privileged":true,
	   "environment":[{"key":"TZ","value":"${TZ}"},{"key":"N","value":3}],
	   "volumes":[{"hostPath":"${APP_DATA_DIR}/data/config","containerPath":"/config"},{"hostPath":"/run/dbus","containerPath":"/run/dbus","readOnly":true}],
	   "addPorts":[{"hostPort":1900,"containerPort":1900,"udp":true}]},
	  {"name":"db","image":"postgres:16","environment":{"POSTGRES_PASSWORD":"${DB_PASS}"}}]}`
	r, err := ParseRecipe([]byte(js), true, "homeassistant")
	if err != nil {
		t.Fatal(err)
	}
	if r.Main != "homeassistant" || r.Services != 2 || len(r.Env) != 2 || r.Env[1].Value != "3" || len(r.Mounts) != 2 || !r.Mounts[1].ReadOnly ||
		len(r.Ports) != 1 || r.Ports[0] != "1900:1900/udp" {
		t.Fatalf("receita json: %+v", r)
	}
	all := strings.Join(r.Privileges, "|")
	if !strings.Contains(all, "privileged") || !strings.Contains(all, "network_mode: host") || !strings.Contains(all, "/run/dbus") {
		t.Fatalf("privilégios: %v", r.Privileges)
	}
	if _, err := ParseRecipe([]byte("{x"), true, "a"); err == nil {
		t.Fatal("json quebrado deveria dar erro")
	}
}

func TestRecipeVariavelComDoisPontos(t *testing.T) {
	yml := "services:\n  app:\n    image: x/app:1\n    volumes:\n      - ${BITCOIND_DIR:-${APP_DATA_DIR}/../btc}:/data:ro\n      - ${ROOT_FOLDER_HOST}:/runtipi\n"
	r, err := ParseRecipe([]byte(yml), false, "app")
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(r.Privileges, "|")
	if !strings.Contains(all, "pasta ${BITCOIND_DIR:-${APP_DATA_DIR}/../btc} do servidor") || !strings.Contains(all, "pasta do Runtipi (inteira)") {
		t.Fatalf("privilégios: %v", r.Privileges)
	}
	if len(r.Mounts) != 2 || r.Mounts[0].Container != "/data" || !r.Mounts[0].ReadOnly {
		t.Fatalf("pastas: %+v", r.Mounts)
	}
}

func TestMemoriaCompartilhada(t *testing.T) {
	r, _ := ParseRecipe([]byte("services:\n  brave:\n    image: linuxserver/brave:1.99.57\n"), false, "brave")
	if !r.Desktop || r.ShmMB != 0 {
		t.Fatalf("brave: %+v", r)
	}
	r, _ = ParseRecipe([]byte("services:\n  app:\n    image: x/app:1\n    shm_size: 1gb\n"), false, "app")
	if r.Desktop || r.ShmMB != 1024 {
		t.Fatalf("shm da receita: %+v", r)
	}
	out, err := Apply(nil, "brave", Settings{ShmMB: 1024})
	if err != nil || !strings.Contains(string(out), "shm_size: 1G") {
		t.Fatalf("gravar: %v\n%s", err, out)
	}
	if st, _ := Parse(out, "brave"); st.ShmMB != 1024 || len(st.Manual) != 0 {
		t.Fatalf("ler: %+v", st)
	}
	if out, _ := Apply(out, "brave", Settings{}); strings.Contains(string(out), "shm") {
		t.Fatalf("tirar:\n%s", out)
	}
	if Validate(Settings{ShmMB: 10}) == nil {
		t.Fatal("10 MB deveria ser recusado")
	}
}

func TestAjustesImagemEReinicio(t *testing.T) {
	out, err := Apply([]byte("services:\n  app:\n    labels:\n      a: b\n"), "app", Settings{Image: "x/app:1.2.3", Restart: "always"})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := Parse(out, "app")
	if st.Image != "x/app:1.2.3" || st.Restart != "always" || !strings.Contains(string(out), "a: b") {
		t.Fatalf("imagem/reinício:\n%s", out)
	}
	out, _ = Apply(out, "app", Settings{})
	if strings.Contains(string(out), "image") || strings.Contains(string(out), "restart") {
		t.Fatalf("vazio deveria tirar:\n%s", out)
	}
	for _, s := range []Settings{{Image: "x y"}, {Image: "X/App"}, {Restart: "sempre"}} {
		if Validate(s) == nil {
			t.Errorf("deveria recusar %+v", s)
		}
	}
}
