# Arquitetura

## Visão rápida

```
navegador ──HTTP(S)──> Traefik do Runtipi ──> container "brunos" (Go, :8080)
                                                 ├─ /data            (volume: conta, sessões, preferências, papéis de parede, discos, known_hosts)
                                                 ├─ /runtipi  (ro)   (pasta do Runtipi: apps instalados, app.env, state/settings.json)
                                                 ├─ /files/home, /files/mnt (+ pasta do Runtipi em ro)  → gerenciador de arquivos
                                                 ├─ /host/proc (ro), /host/os-release, /host/hostname
                                                 ├─ /sys (sysfs padrão do Docker: hwmon, power_supply, block)
                                                 ├─ tcp://brunos-docker-proxy:2375  → socket-proxy → docker.sock (ro)
                                                 └─ SSH (rede) → servidor:22  (janela Terminal)
```

Um único binário estático serve a API (`/api/*`) e a interface (`/`, embutida com `go:embed`).

## Backend (Go 1.24)

### Configuração por variáveis de ambiente (`main.go`)

| Variável | Padrão | Uso |
|---|---|---|
| `BRUNOS_ADDR` | `:8080` | endereço HTTP |
| `BRUNOS_DATA` | `/data` | dados persistentes |
| `BRUNOS_FILES` | `/files` | raiz do gerenciador (descoberta automática) |
| `BRUNOS_ROOTS` | — | lista manual `Nome=/caminho[:ro];…` (desliga a descoberta e os discos mapeados) |
| `BRUNOS_RUNTIPI` | `/runtipi` | pasta do Runtipi |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | no Runtipi: `tcp://brunos-docker-proxy:2375`; `off` desliga |
| `BRUNOS_DOCKER_ACTIONS` | `false` | permite reiniciar apps |
| `BRUNOS_TERMINAL` | `true` | `false`/`off` desliga o terminal SSH |
| `HOST_PROC` | `/host/proc` | `/proc` do host (rede usa `HOST_PROC/1/net/dev`) |
| `HOST_OS_RELEASE`, `HOST_HOSTNAME_FILE` | `/host/os-release`, `/host/hostname` | informações do sistema |
| `BRUNOS_MAX_UPLOAD_GB` | `50` | limite por envio |
| `BRUNOS_ALLOW_PUBLIC_SETUP` | `false` | primeira execução aceita só IP privado/Tailscale |

Subcomando `brunos healthcheck` (usado pelo `HEALTHCHECK` da imagem).

### Pacotes

- **`internal/auth`**: `/data/auth.json` (`{"version":1,"users":[{username,displayName,salt,hash,iterations,createdAt,passwordChangedAt}]}`)
  e `/data/sessions.json` (`{sha256(token): {username,createdAt,expiresAt,ip,userAgent}}`). PBKDF2-SHA256
  600 mil iterações; sessão de 30 dias deslizante; limitador de tentativas em `limiter.go`.
- **`internal/store`**: `WriteJSON(path, v, perm)`, `WriteFile`, `ReadJSON(path, &v) (ok, err)`, gravação atômica.
- **`internal/sysinfo`**: `Collector` amostra a cada 2 s com ouvintes (SSE) e 15 s sem; histórico de 150
  pontos. Lê `/proc/stat`, `/proc/meminfo`, hwmon (ordem de preferência: coretemp "Package id 0", k10temp,
  …, dell_smm "CPU", acpitz), ventoinha, `/sys/class/power_supply` (bateria + fonte "Mains"),
  `/proc/self/mounts` (discos: só sistemas de arquivos reais em `/dev/*`, nome do dispositivo via
  `st_dev` → `/sys/dev/block/M:m/uevent`, ignora `/dev/loop` e partições < 1 GiB), rede filtrando
  interfaces virtuais. Bateria: potência suavizada por média móvel, `minutesLeft`/`minutesFull`,
  `estimate` (autonomia na tomada pelo último consumo medido descarregando; só em memória), histórico
  de carga a cada 5 min por 24 h (enviado no stream só a cada ~30 amostras).
- **`internal/dockerc`**: cliente HTTP mínimo para a API Docker v1.41 (unix ou tcp): `Containers`,
  `Stats` (one-shot; CPU por delta; memória = usage − inactive_file), `Restart`, `Ping`.
- **`internal/runtipi`**: `Discoverer` com cache de 20 s. Lê `apps/<loja>/<app>/config.json`,
  `app-data/<loja>/<app>/app.env` (**só** as chaves da lista permitida: `APP_PORT`, `APP_DOMAIN`,
  `APP_LOCAL_DOMAIN`, `APP_PROTOCOL`, `APP_EXPOSED`, `APP_EXPOSED_DOMAIN`, `LOCAL_DOMAIN`, `INTERNAL_IP`,
  `NGINX_PORT`) e `state/settings.json` (`localDomain`, `internalIp`, `port`). Projeto compose =
  `<app>_<loja>`. Logo em `apps/<loja>/<app>/metadata/logo.jpg`.
- **`internal/files`**: raízes (`Root` com `os.Root`); `Kind` "place" (Home) ou "disk" (pastas de
  `/mnt` e `/media`). Discos só ficam acessíveis se mapeados (`/data/disks.json`:
  `{"version":1,"mapped":["1TB_HDD",…]}`); sem arquivo, valem as pastas que são ponto de montagem
  (`st_dev` diferente da pasta pai). `Refresh()` redescobre pastas e reabre raízes cujo dispositivo
  mudou. Operações: List, Open, Mkdir, Rename, Paste (copia entre discos em EXDEV), Delete, Upload
  (temporário + rename, nome único "x (2).ext"), Search (3 s / 50 mil entradas), lixeira em
  `<raiz>/.brunos-trash/<id>` + `<id>.json`.
- **`internal/terminal`**: `Manager` (até 4 sessões, 12 aberturas/min, ociosa fecha em 3 min, buffer
  de 512 KB enquanto ninguém lê). TOFU em `/data/known_hosts.json`
  (`{"version":1,"hosts":{"host:porta":{type,fingerprint,key,addedAt}}}`). Prefere chave ED25519 como
  o OpenSSH. Erro depois da chave aceita = recusa de login (403 com `sshAuth`).
- **`internal/sshknown`** (0.4.0): TOFU compartilhado (`Store.Callback`, `Forget`, `Algorithms`,
  `FriendlyDialError`). Uma única instância por arquivo, usada pelo terminal e pelo hostcmd.
- **`internal/events`** (0.4.0): central de notificações. `/data/events.json`
  (`{"version":1,"events":[{id,time,kind,level,title,body,read,key,meta}],"active":[...]}`), 500 avisos /
  30 dias. `Emit` com `Key` não repete enquanto o problema está em `active`; `Resolve(key, &ev)` fecha e
  registra a volta ao normal. `Subscribe` alimenta o SSE das estatísticas (`event: notify`).
- **`internal/hostcmd`** (0.4.0): chave ED25519 própria em `/data/hostcmd_key` (0600). `Test` conecta e
  autentica **sem abrir sessão** (o `command=` forçado executaria); `Run` abre a sessão e roda o comando
  forçado (`sudo -n /usr/sbin/shutdown -h +1`). Só aceita servidor já confirmado no `known_hosts.json`.
- **`internal/power`** (0.4.0): `Monitor` lê a bateria a cada 5 s (`Reading` montado em `watch.go`).
  Quedas em `/data/power-events.json` (`{drainW, outages:[{start,end,startPct,endPct,minPct,shutdown}]}`,
  100 quedas); configuração em `/data/power.json` (`enabled, threshold, graceSec, simulate, host, port,
  user, testedAt`). Fases: off → armed → countdown → running → done/cancelled/failed; a energia voltando
  rearma. Sair da simulação exige `testedAt` (zera se host/porta/usuário mudarem). 3 tentativas, 1 min.
- **`watch.go`** (pacote main): `batteryReading` e `watchDisks` (a cada 1 min: disco mapeado e não
  montado → evento crítico `disk-unmounted:<nome>`).
- **`web/web.go`**: arquivos estáticos com gzip e ETag em memória; rotas desconhecidas caem no SPA.

### API

Todas as rotas `/api/*` exceto health/public exigem sessão (cookie). Métodos que alteram exigem o
cabeçalho `X-BrunOS: 1` (e origem igual). Erros: `{"error": "mensagem em pt-BR"}`.

```
GET  /api/health                      GET  /api/public · /api/public/wallpaper
POST /api/auth/setup · login · logout · password      GET/PUT /api/auth/me
GET/PUT /api/settings                 (JSON livre do frontend, até 512 KB, /data/settings.json)
GET/PUT /api/wallpapers · GET/DELETE /api/wallpapers/{name}   (nome ^[a-f0-9]{16}\.(jpg|png|webp|avif|gif)$)
GET  /api/stats · /api/stats/stream (SSE: event init {snapshot,history}; data {snapshot,point})
GET  /api/system                      (info, runtipi, docker, dockerActions, terminal, version...)
GET  /api/apps · /api/apps/logo?urn= · /api/usage   POST /api/apps/restart
GET  /api/files/roots · disks · list · raw · zip · search · trash
PUT  /api/files/disks · upload        POST /api/files/mkdir · rename · paste · trash · delete · trash/restore · trash/delete · trash/empty
GET  /api/terminal/sessions · stream?id= (SSE base64; eventos ready/exit/replaced)
POST /api/terminal/open · input · resize · close · forget
GET  /api/events                      POST /api/events/read {ids:[]} (vazio = todos) · /api/events/clear
GET  /api/power (config, status, outages, drainW, keyReady, suggestedHost) · /api/power/setup
PUT  /api/power/config                POST /api/power/cancel · /api/power/test {host,port,user,trust}
```

O stream `/api/stats/stream` também manda `event: notify` com cada aviso novo da central.
`POST /api/power/test` responde 409 `{unknownHost, fingerprint}` na primeira vez, igual ao terminal.

`POST /api/terminal/open` devolve 409 com `{unknownHost, addr, keyType, fingerprint}` na primeira
conexão ou `{hostKeyChanged, known, fingerprint}` se a chave mudou; o cliente reenvia com `trust`.

### Dados em `/data` (todos 0600)

`auth.json`, `sessions.json`, `settings.json`, `wallpapers/`, `disks.json`, `known_hosts.json`,
`events.json`, `power.json`, `power-events.json`, `hostcmd_key` (chave privada do desligamento).

## Frontend (`web/static`)

- `index.html` + `css/app.css` (base adaptada do umbrelOS) + `css/extra.css` (componentes novos,
  `@font-face` Inter). Fontes Inter (latin e latin-ext, OFL) em `assets/fonts`.
- `assets/wallpapers` (12 AVIF do Umbrel + miniaturas), `assets/icons` (16 webp do dashboard-icons),
  `favicon.svg`.
- `js/api.js`: `call()` lança `Error` com `.status` e `.data`; `export let api` + `initApi()`; **modo
  demonstração** quando não há `/api` (dados simulados do servidor do Bruno, shell de mentira no terminal).
- `js/app.js`: estado global `S`, preferências `DEFAULTS`, render da área de trabalho, widgets
  (`WIDGETS`, até `MAX_WIDGETS = 6`), grade de apps, dock (`SYS`: files, runtipi, store, live, settings),
  folhas (`openSheet('files'|'settings')`, classe `sheet-<tipo>`), configurações (`settingsRows()`),
  paleta `Ctrl+K`, diálogos (Uso ao vivo, dispositivo, senha, atalho, personalizar app), lista suspensa
  do dock (`dockPicker`). O atalho de teclado global ignora eventos dentro de `.xterm`.
- `js/ui.js`: `$`, `$$`, `esc`, `html`, `fmt*`, `svg(nome)` (ícones inline), `contextMenu`,
  `openDialog(html, {width, cls, closeBtn, backdropClose, onClose})`, `confirmDialog`, `promptDialog`, `toast`.
- `js/files.js`: janela Arquivos; exporta `mountFiles`, `navigate`, `mapDisksDialog(ctx, onDone)`.
- `js/battery.js`: `batteryWidgetHTML`, `paintBattery(root, battery)`, `batteryDialog`.
- `js/terminal.js`: `terminalWindow(ctx)`; carrega `vendor/xterm/xterm.mjs` e `addon-fit.mjs` sob demanda.
- `js/notifications.js` (0.4.0): sino `#bell` (no `index.html`), painel, `onNotify(e)` (toast),
  `refreshNotifications()` (chamado no `init` do stream, ou seja, também a cada reconexão).
- `js/power.js` (0.4.0): `powerDialog({openTerminal})` (Configurações → Sistema → Energia),
  `paintBanner(status)`/`refreshPowerBanner()` (faixa da contagem; usa `status.remaining` do servidor),
  `outagesHTML(lista)` (também na janela da bateria). Modo demonstração: `#contagem` no endereço
  mostra a faixa de contagem.

### Preferências (`/api/settings`, objeto do frontend)

`wallpaper` (`builtin:24` | `upload:<nome>` | `local`), `accentMode` (`auto`|`vibrant`|`manual`),
`manualHue`, `glass`, `blur`, `iconSize`, `widgets` (`files`, `storage`, `system`, `network`, `battery`),
`showStatus`, `reduceMotion`, `openIn` (`tab`|`same`), `urlMode` (`auto`|`domain`|`port`), `serverHost`,
`runtipiUrl`, `dock` (ids), `order`, `apps` (personalizações por id: nome, ícone, url, hidden), `links`
(atalhos), `favorites` (pastas), `welcomed`, `filesView`, `filesHidden`, `filesDisksOpen`, `sshHost`,
`sshPort`, `sshUser`, `sshMethod`, `termFontSize`. **Nunca** guardar senhas aqui.

## Testes

- `internal/terminal/terminal_test.go`: servidor SSH em processo (TOFU, senha errada, eco, resize,
  saída com código, troca de chave, validação).
- `tests/fixture.sh <dir>`: cria servidor fictício (home, `/mnt/1TB_HDD`, `/mnt/1TB_HDD2`, link
  simbólico de fuga, Runtipi falso com apps, JPEG real).
- `tests/apitest.py`: 63 checagens da API (auth, CSRF, limites, arquivos, fuga por links, discos,
  terminal, lixeira, wallpapers...). Precisa de servidor novo (faz o setup).
- `tests/smoke.sh <imagem>`: sobe socket-proxy + OpenSSH (linuxserver/openssh-server) + BrunOS com as
  restrições do Runtipi; roda o apitest; confere Docker, uso por container, sistema, estatísticas;
  terminal SSH real; proxy recusando outros containers; memória e tamanho da imagem.
- CI (`.github/workflows/docker.yml`): confere `vendor/` contra os módulos oficiais, `go vet`, `go test
  -race`, build, gofmt (fora do vendor), build da imagem, smoke; publica no GHCR quando `VERSION` é nova.
