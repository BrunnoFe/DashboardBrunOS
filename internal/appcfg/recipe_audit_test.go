package appcfg

// Receitas hostis da auditoria de 19/11/2026: todas precisam ganhar o selo "Requer privilégios". E o que
// os apps comuns usam continua sem selo.

import "testing"

func TestRecipeHostileGetsBadge(t *testing.T) {
	casos := map[string]string{
		"caminho relativo com ..":          "services:\n  a:\n    image: x\n    volumes: ['../../../../../../../../:/host']\n",
		"til (pasta pessoal)":              "services:\n  a:\n    image: x\n    volumes: ['~/.ssh:/k']\n",
		"âncora + mescla <<":               "x: &p\n  privileged: true\nservices:\n  a:\n    <<: *p\n    image: x\n",
		"alias no valor":                   "x: &t true\nservices:\n  a:\n    image: x\n    privileged: *t\n",
		"serviço inteiro como alias":       "x: &s\n  image: x\n  privileged: true\nservices:\n  a: *s\n",
		"lista de cap_add como alias":      "c: &c [SYS_ADMIN]\nservices:\n  a:\n    image: x\n    cap_add: *c\n",
		"volume de 1º nível bind device:/": "volumes:\n  r:\n    driver_opts: {type: none, o: bind, device: /}\nservices:\n  a:\n    image: x\n    volumes: ['r:/host']\n",
		"volume com driver de fora":        "volumes:\n  r:\n    driver: rclone\nservices:\n  a:\n    image: x\n    volumes: ['r:/host']\n",
		"secrets file:/etc/shadow":         "secrets:\n  s:\n    file: /etc/shadow\nservices:\n  a:\n    image: x\n    secrets: [s]\n",
		"configs file com ..":              "configs:\n  c:\n    file: ../../../.env\nservices:\n  a:\n    image: x\n",
		"env_file de outro app":            "services:\n  a:\n    image: x\n    env_file: ['../../app-data/migrated/vaultwarden/app.env']\n",
		"env_file absoluto (mapa)":         "services:\n  a:\n    image: x\n    env_file:\n      - path: /etc/brunos-host.conf\n",
		"include":                          "include: ['../outro/docker-compose.yml']\nservices:\n  a:\n    image: x\n",
		"extends":                          "services:\n  a:\n    image: x\n    extends: {file: ../../x.yml, service: b}\n",
		"/home/<usuário> inteira":          "services:\n  a:\n    image: x\n    volumes: ['/home/usuario:/h']\n",
		"/home/<usuário>/.ssh":             "services:\n  a:\n    image: x\n    volumes: ['/home/usuario/.ssh:/k']\n",
		"app-data inteiro do Runtipi":      "services:\n  a:\n    image: x\n    volumes: ['${ROOT_FOLDER_HOST}/app-data:/d']\n",
		"app-data de outro app":            "services:\n  a:\n    image: x\n    volumes: ['${ROOT_FOLDER_HOST}/app-data/migrated/brunos/data:/d']\n",
		"alias de rede brunos":             "services:\n  a:\n    image: x\n    networks:\n      tipi_main_network:\n        aliases: [brunos]\n",
		"hostname brunos-docker-proxy":     "services:\n  a:\n    image: x\n    hostname: brunos-docker-proxy\n",
	}
	for nome, y := range casos {
		r, err := ParseRecipe([]byte(y), false, "a")
		if err != nil {
			t.Errorf("%s: erro ao ler: %v", nome, err)
			continue
		}
		if len(r.Privileges) == 0 {
			t.Errorf("%s: passou sem o selo", nome)
		}
	}
}

func TestRecipeCommonNoBadge(t *testing.T) {
	casos := map[string]string{
		"dados do próprio app":      "services:\n  a:\n    image: x\n    volumes: ['${APP_DATA_DIR}/data:/config']\n",
		"relativo dentro do app":    "services:\n  a:\n    image: x\n    volumes: ['./data:/x']\n",
		"volume com nome comum":     "volumes:\n  db: {}\nservices:\n  a:\n    image: x\n    volumes: ['db:/var/lib/db']\n",
		"mídia do Runtipi":          "services:\n  a:\n    image: x\n    volumes: ['${ROOT_FOLDER_HOST}/media/data/movies:/movies']\n",
		"pasta pessoal comum":       "services:\n  a:\n    image: x\n    volumes: ['/home/usuario/Fotos:/fotos']\n",
		"disco em /mnt":             "services:\n  a:\n    image: x\n    volumes: ['/mnt/disco1/Filmes:/filmes']\n",
		"hora certa":                "services:\n  a:\n    image: x\n    volumes: ['/etc/localtime:/etc/localtime:ro']\n",
		"env_file do próprio app":   "services:\n  a:\n    image: x\n    env_file: [.env]\n",
		"âncora inofensiva":         "x: &env\n  TZ: America/Sao_Paulo\nservices:\n  a:\n    image: x\n    environment: *env\n",
		"secret do próprio app":     "secrets:\n  s:\n    file: ./senha.txt\nservices:\n  a:\n    image: x\n",
		"placa de vídeo (/dev/dri)": "services:\n  a:\n    image: x\n    devices: ['/dev/dri:/dev/dri']\n",
	}
	for nome, y := range casos {
		r, err := ParseRecipe([]byte(y), false, "a")
		if err != nil {
			t.Errorf("%s: erro ao ler: %v", nome, err)
			continue
		}
		if len(r.Privileges) != 0 {
			t.Errorf("%s: ganhou selo sem motivo: %v", nome, r.Privileges)
		}
	}
	// a âncora inofensiva continua preenchendo as variáveis da janela de instalação
	r, _ := ParseRecipe([]byte(casos["âncora inofensiva"]), false, "a")
	if len(r.Env) != 1 || r.Env[0].Key != "TZ" {
		t.Logf("variáveis vindas de âncora: %+v (só informativo)", r.Env)
	}
}

// "Bomba" de aliases (cada nível dobra): não pode travar nem estourar a memória; ganha o selo.
func TestRecipeAliasBomb(t *testing.T) {
	y := "a: &a [x,x,x,x,x,x,x,x,x,x]\nb: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a,*a]\nc: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b,*b]\n" +
		"d: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c,*c]\ne: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d,*d]\nf: &f [*e,*e,*e,*e,*e,*e,*e,*e,*e,*e]\n" +
		"services:\n  a:\n    image: x\n    labels: *f\n"
	r, err := ParseRecipe([]byte(y), false, "a")
	if err == nil && len(r.Privileges) == 0 {
		t.Fatal("receita com bomba de aliases passou sem selo")
	}
}
