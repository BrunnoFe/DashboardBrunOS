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
  `Stats` (one-shot; CPU por delta; memória = usage − inactive_file), `Restart`, `Start`, `Stop` (304 não
  é erro), `Logs(id, tail, follow)` + `Demux` (formato multiplexado com cabeçalho de 8 bytes), `Ping`.
- **`internal/appmon`** (0.7.0): por projeto do compose, 1440 pontos (24 h, um por minuto) de CPU,
  memória (MB) e disponibilidade (1 no ar, 0 fora, −1 parado/sem medição), só em memória. `Uptime`,
  `Health` (fora do ar só depois de 2 minutos seguidos), `Alert` (`app-down:<urn>` / `app-up`).
- **`internal/history`** (0.8.0): `Collector.OnSample` → `Add(cpu, mem%, temp, rx, tx, dt)`; acumula a
  média ponderada do minuto e da hora; anéis de 1440 minutos e 168 horas; `Series("1h"|"24h"|"7d")`
  (o período em andamento entra como último ponto); `/data/history.json` a cada 10 min e no desligamento.
- **`internal/procs`** (0.8.0): `Reader.List(kernel)` lê `/host/proc/<pid>/{stat,status,cmdline,cgroup}`;
  CPU = Δticks ÷ Δtotal do `/proc/stat` × núcleos (primeira chamada lê duas vezes com 400 ms); esconde
  threads do kernel; usuário por `/host/passwd` (`HOST_PASSWD`); id do container (64 hex) no cgroup →
  o `main` troca pelo nome do app.
- **`internal/netmon`** (0.8.0): a cada minuto, TCP para `1.1.1.1:443`, `8.8.8.8:443`, `9.9.9.9:443` em
  paralelo (vale o mais rápido); 2 minutos sem nenhum → `internet-down`, e `internet-up` na volta com a
  duração; 24 h na memória, quedas (100) em `/data/internet.json`. Preferência `netMonitor` (padrão ligado).
- **`apps.go`** (pacote main, 0.7.0): `hAppAction` (start/stop/restart de todos os containers do app),
  `hAppContainers`, `hAppHistory`, `hAppLogs` (texto, até 5000 linhas / 8 MB), `hAppLogsStream` (SSE,
  uma linha JSON por evento), `watchApps` (a cada minuto: `collectUsage` + `probe` em
  `InternalIP:porta` — porta fechada ou travada = fora; qualquer resposta HTTP ou protocolo diferente =
  no ar; container `restarting` = fora; parado = não conta). Preferências lidas do `settings.json`:
  `appMonitor` (padrão ligado) e `apps[urn].noMonitor`.
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
  0.6.0: `mediaTypes` (tipos de vídeo/áudio próprios: a imagem `scratch` não tem `/etc/mime.types`);
  `edit.go` (`ReadText`/`SaveText` com mtime em ms, máx. 5 MB, temporário + rename mantendo a
  permissão, confere se dá para abrir o arquivo para escrita antes; `NewFile`); `resumable.go` (envio em
  pedaços: `.brunos-part-<id>` na pasta de destino, estado em `/data/uploads.json`, pedaço até 32 MB,
  posição errada → 409 com a certa, pendentes > 48 h apagados); `extract.go` (zip/tar/tar.gz pelo
  `os.Root`, sem links, orçamento = espaço livre − 1 GB, 200 mil itens, apaga a pasta se falhar);
  `duplicates.go` (tamanho → SHA-256 de começo+fim → SHA-256 inteiro; 300 mil arquivos, 90 s, 50 GB
  lidos no máximo); `usage.go` (soma por pasta numa varredura só, cache de 10 min por raiz, 30 s / 1
  milhão de itens, uma varredura por vez).
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
  forçado. Desde a 0.8.1 o comando forçado é o script `/usr/local/sbin/brunos-host` (`HostScript`, instalado
  pelos comandos colados), que só aceita `desligar` (`systemctl poweroff --check-inhibitors=no --when=+1min`),
  `limite 50..100`, `protecao on|off` e `versao`, e chama a si mesmo pelo sudo (sudoers libera só ele),
  conferindo de novo como root. Duas chaves: `/data/hostcmd_key_v2` (atual) e `/data/hostcmd_key` (da
  0.8.0, que forçava `shutdown -h +1`; usada só para desligar até o servidor ser atualizado). Servidor não
  atualizado recusa a chave nova na autenticação, então nunca roda o comando antigo por engano.
  `SetupVersion` (hoje 2) sobe quando o que se cola no servidor muda. Só aceita servidor já confirmado.
- **`internal/power`** (0.4.0): `Monitor` lê a bateria a cada 5 s (`Reading` montado em `watch.go`).
  Quedas em `/data/power-events.json` (`{drainW, outages:[{start,end,startPct,endPct,minPct,shutdown,seen,off}]}`,
  100 quedas); na bateria grava a cada minuto (`seen`), e se o servidor apagar a queda fecha em `seen`
  com `off: true`. Configuração em `/data/power.json` (`enabled, threshold, graceSec, simulate, host, port,
  user, testedAt, hostSetup`). Fases: off → armed → countdown → running → done/cancelled/failed; a energia
  voltando rearma. Sair da simulação exige `testedAt` (zera se host/porta/usuário mudarem). Tenta desligar
  a cada 1 min enquanto estiver na bateria, sem limite (avisa na 1ª falha e a cada 10); bateria ≤ metade
  do limite (ou ≤ 5%) desliga sem esperar a contagem. `hostSetup` < `SetupVersion` com `testedAt` =
  servidor com configuração antiga: a janela Energia mostra os comandos de atualização (`updateCommands`
  em `GET /api/power/setup`); o teste de conexão (chave nova) passando grava `hostSetup` atual.
  **Limite de carga** (`charge.go`): `detectCharge` lê o `/sys` uma vez ao ligar (e em
  `POST /api/power/charge {detect}`): `range`, `toggle` ou `none`. `POST /api/power/charge {limit}` ou
  `{protect}` aplica pelo script e grava `chargeLimit`/`chargeProtect` em `power.json`; a cada 10 min,
  só se houver suporte e pedido, relê um arquivo e reaplica (no máximo a cada 30 min).
- **`internal/sysinfo/drivetemp.go`** (0.5.0): temperatura por disco (`Disk.temp`, `tempAt`). Liga o
  hwmon `drivetemp` ao `sdX` (`hwmonN/device/block/*`) e o hwmon `nvme` ao controlador (`nvme0`). Mede
  junto com os discos (a cada 20 s), mas no máximo 1 vez por minuto, e um HD só de novo se
  `/sys/block/sdX/stat` mudou (ler pode acordar disco parado). `readHwmon` ignora o `drivetemp`.
- **`internal/diskhist`** (0.5.0): `/data/disk-history.json` (`{"version":1,"disks":{"nome":[{d,u,f}]}}`,
  180 dias, um ponto por dia). `Forecasts()`: regressão linear dos últimos 30 dias (mín. 5 pontos em
  6 dias) → `perDay`, `daysLeft` (espaço livre ÷ ritmo; nil se não cresce). `Check` emite
  `disk-full:<nome>` (≥ 90%, fecha < 85%) e, só se a preferência `diskForecast` estiver ligada
  (padrão: desligada; o `watch.go` lê o `settings.json`), `disk-full-soon:<nome>` (≤ 30 dias, fecha > 45).
- **`internal/logins`** (0.5.0): `/data/logins.json` (`{"version":1,"devices":[{key,browser,os,local,ip,first,last,count}]}`,
  100 aparelhos). Chave = navegador|sistema|lugar (lugar = "local" para IPs privados/Tailscale, ou a
  faixa /24 ou /48). `ParseUA` simples. `BlockAlertDue` limita o aviso de bloqueio (6 h por IP, em memória).
  `auth.Manager.OnBlock` é chamado quando o limitador bloqueia um IP; `auth.Manager.Sessions()` lista as sessões.
- **`watch.go`** (pacote main): `batteryReading` e `watchDisks` (a cada 1 min: disco mapeado e não
  montado → evento crítico `disk-unmounted:<nome>`; disco quente `disk-hot:<nome>` (HD ≥ 55 °C, fecha
  ≤ 50; SSD ≥ 70, fecha ≤ 65); a cada hora grava o uso no `diskhist` e confere a previsão).
  `loginSeen`/`loginBlocked` emitem `login-new` e `login-blocked`.
- **`internal/secret`** (0.9.0): `Load(path)` cria/lê `/data/secret.key` (32 bytes aleatórios, 0600);
  `Box.Seal/Open` com AES-256-GCM (nonce aleatório, saída em base64). Só cifra as credenciais das
  integrações (decisão 33).
- **`internal/integrations`** (0.9.0; termos em `CONTEXT.md`, decisões 31–33):
  - `kinds.go`: `Kind` (`Info()` + `Fetch(ctx, client, Config)`), `KindInfo` (campos do formulário,
    frequência padrão, catálogo de Métricas `MetricDef{id,name,kind num|text|list|series,unit,icon,
    ranges,history}`, Avisos prontos `AlertDef{id,metric,op < | > | down,default,fixed}`, ids de app do
    Runtipi para sugerir), `Value{n,t,l,s}`, `SeriesData{step,start,points}` (NaN vira null). Métricas
    que começam com `_` ficam só no cache (ex.: `_id` do último teste do Speedtest). `getJSON` limita a
    resposta a 2 MB e traduz os erros para pt-BR. Um arquivo por Tipo: `adguard.go`
    (`/control/stats` + `/control/status`, autenticação básica), `speedtest.go` (`/api/v1/results/latest`
    e a lista de 7 dias, token Bearer; a lista só é baixada de novo quando aparece um teste novo).
  - `store.go`: `/data/integrations.json` (`{"version":1,"integrations":[{id,kind,name,url,user,secret
    (cifrado),interval,alerts:{id:{on,value}}}]}`); a API só devolve `Public` (`hasSecret`); editar sem
    mandar `secret` mantém o atual. Frequência de 10 s a 1 h; até 50 integrações.
  - `hub.go`: cache por integração e agendamento (volta a cada 5 s, mas só busca o que venceu): com
    alguém no SSE e algum widget usando → no intervalo da integração; aviso ligado ou número num gráfico
    sem histórico do app → a cada 5 min; senão, nada. `SetUsage(settings)` lê `widgets2` ao salvar as
    preferências. Até 3 buscas simultâneas, timeout 15 s, sem seguir redirecionamentos. Avisos prontos
    pela central (`integ:<id>:<aviso>`; "sem responder" só depois de 5 min).
  - `series.go`: histórico próprio das métricas coletadas em segundo plano, `/data/metric-history.json`
    (24 h a cada 5 min + 7 dias por hora; métrica sem amostra há 7 dias é descartada).
  - `handlers.go`: rotas abaixo e `Suggest` (apps do Runtipi de tipo conhecido → `http://IP:porta`).
    0.9.2: `GET /api/integrations/{id}/image?ref=` (o Tipo confere a referência via `ImageKind`; só
    imagens até 8 MB; cache de 60 em `/data/img-cache`, nome = sha256 de id+ref).
  - 0.9.2: `immich.go` (`/api/assets/statistics`, `/api/server/statistics` se admin, `/api/memories`,
    `/api/search/random`, foto `size=preview`; listas de fotos uma vez por dia), `plex.go`
    (`/status/sessions`, `/library/sections` uma vez por hora, capa por `/photo/:/transcode`),
    `watchyourlan.go` (`/api/all`; aviso `Op: "new"` por item com `Item.ID` = MAC), `tailscale.go`
    (`NoURL`; `HostQuery("tailscale")` → `power.Monitor.HostOutput` → `hostcmd.Runner.Output`). Métricas
    do tipo `media` (lista de `Item{img}`, `MetricDef.Look` = photo | poster). Campos extras do
    formulário (ex.: álbum) ficam em `Integration.Options`.
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
GET  /api/apps (com health/uptime) · /api/apps/logo?urn= · /api/usage   POST /api/apps/restart · start · stop {urn}
GET  /api/apps/containers?urn · /api/apps/history?urn ({points,step,health,uptime?}) · /api/apps/logs?urn&id&tail
GET  /api/apps/logs/stream?urn&id     (SSE: ready, data = linha em JSON, end)
GET  /api/stats/history?range=1h|24h|7d ({range,step,points:[{t,cpu,mem,temp?,rx,tx}]})
GET  /api/processes[?kernel=1]        ({procs:[{pid,ppid,name,cmd,user,state,threads,cpu,mem,memPct,app?}],summary})
GET  /api/internet                    ({up,checked,ms,avg,uptime?,points:[{t,ms}],outages:[{start,end}],enabled})
GET  /api/files/roots · disks · list · raw · zip · search · trash
PUT  /api/files/disks · upload        POST /api/files/mkdir · rename · paste · trash · delete · trash/restore · trash/delete · trash/empty
GET  /api/terminal/sessions · stream?id= (SSE base64; eventos ready/exit/replaced)
POST /api/terminal/open · input · resize · close · forget
GET  /api/events                      POST /api/events/read {ids:[]} (vazio = todos) · /api/events/clear
GET  /api/power (config, status, outages, drainW, keyReady, suggestedHost) · /api/power/setup
PUT  /api/power/config                POST /api/power/cancel · /api/power/test {host,port,user,trust}
GET  /api/disks/forecast              ({disks:[{name,perDay,daysLeft?,enough,points:[{d,u,f}]}]})
GET  /api/files/text?root&path        ({text,mtime(ms),size,readOnly}; 413 grande, 415 binário/não-UTF-8)
PUT  /api/files/text?root&path&mtime  (corpo = texto; 409 se o arquivo mudou)   POST /api/files/newfile
POST /api/files/upload/start {root,path,name,size} → {id} · PUT upload/chunk?id&offset (409 {offset})
GET  /api/files/upload/status?id      POST /api/files/upload/finish {id} · upload/cancel {id}
POST /api/files/extract {root,path}   GET /api/files/duplicates?root&path&min · /api/files/usage?root&path[&refresh=1]
GET  /api/integrations/kinds · /api/integrations ({integrations:[Public],views}) · /api/integrations/suggest
POST /api/integrations {kind,name,url,user,secret?,interval,alerts}   PUT/DELETE /api/integrations/{id}
POST /api/integrations/test {…, id?}  (busca uma vez sem salvar; com id usa o token guardado; 502 {error})
GET  /api/metrics/series?id=<integração>.<métrica>&range=1h|24h|7d ({range,series:{step,start,points}})
```

O stream `/api/stats/stream` também manda `event: notify` com cada aviso novo da central e
`event: metrics` (`{id,at,err,values}` ou `{id,removed}`) quando uma integração é buscada; o `init`
traz `metrics` com o cache atual. Abrir o stream é o que faz o servidor começar a buscar.
`POST /api/power/test` responde 409 `{unknownHost, fingerprint}` na primeira vez, igual ao terminal.

`POST /api/terminal/open` devolve 409 com `{unknownHost, addr, keyType, fingerprint}` na primeira
conexão ou `{hostKeyChanged, known, fingerprint}` se a chave mudou; o cliente reenvia com `trust`.

### Dados em `/data` (todos 0600)

`auth.json`, `sessions.json`, `settings.json`, `wallpapers/`, `disks.json`, `known_hosts.json`,
`events.json`, `power.json`, `power-events.json`, `hostcmd_key` (chave privada do desligamento),
`logins.json` (acessos conhecidos), `disk-history.json` (uso diário dos discos), `uploads.json`
(envios em pedaços pendentes), `history.json` (uso por minuto/hora), `internet.json` (quedas de internet),
`secret.key` (chave das credenciais), `integrations.json` (integrações, credencial cifrada),
`metric-history.json` (séries coletadas em segundo plano).

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
- `js/disks.js` (0.5.0): `diskRowsHTML(disks, forecast)` (lista de discos do Uso ao vivo, com
  temperatura e previsão), `forecastText(f)`, `driveTempHelp()` (comandos do drivetemp).
- `js/editor.js` (0.6.0): `openEditor({root, path, name, onSaved})`; importa
  `vendor/codemirror/codemirror.mjs` (bundle gerado por `tools/codemirror`, ver o README de lá) só ao abrir.
- `js/filetools.js` (0.6.0): `spaceDialog` (treemap "squarified" em divs + lista) e `duplicatesDialog`.
- `js/api.js`: arquivos > 16 MB vão por `chunkedUpload` (pedaços de 8 MB, até 8 tentativas com espera
  crescente, id guardado no `localStorage` por raiz|pasta|nome|tamanho|lastModified para retomar).
- `js/ui.js`: `openDialog(..., {beforeClose})` (devolver false cancela o fechamento).
- `js/internet.js` (0.8.0): `internetWidgetHTML`, `paintInternet(root, status)`, `internetDialog()`.
  O Uso ao vivo (`app.js`) ganhou o seletor 5 min / 24 h / 7 dias e a aba Processos (com filtro).
- `js/appinfo.js` (0.7.0): `appDetails(a, ctx)` (estado, linha do tempo de 96 blocos de 15 min, gráfico
  SVG de 24 h, logs com "Ao vivo", ligar/parar/reiniciar, "Abrir no Runtipi", "Desinstalar…" que abre
  `/apps/<loja>/<app>` no painel do Runtipi, "Avisar se este app cair").
- `js/app.js` (0.7.0): `homeItems()` (apps fora de pastas + pastas, na ordem de `order`, que agora guarda
  também `folder:<id>`), `perPage()` (colunas × linhas que cabem abaixo dos widgets, mínimo 2 linhas),
  páginas com `scroll-snap` + pontinhos + setas + roda do mouse; arrastar no meio de um ícone junta numa
  pasta, na borda reordena; pasta com 1 app se desfaz.
- **Widgets (0.9.0)**:
  - `js/widgets.js`: `SIZES` (s, w, t, l, f), `FORMATS` (stat, grid, list, chart, shortcuts),
    `BRUNOS_METRICS` + `brunosValue(id, {snapshot, internet, apps})` (fonte BrunOS calculada no navegador,
    sem coleta nova), `fmtVal`/`levelOf` (unidade, casas, compacto, cores por faixa `{warn, crit}`),
    `modelGroups(integrações)`, `normalize`, `migrate(widgets antigos)`, `innerHTML(item, ctx)` (miolo,
    redesenhado só quando o texto muda) e `widgetHTML` (moldura `wfm-<formato>` + rótulo).
  - `js/charts.js`: `lineSVG`, `barSVG`, `donutSVG`, `sparkSVG`, `stats` (SVG puro, sem biblioteca).
  - `js/widgetsapp.js`: janela Widgets (`mountWidgets(body, {ctx, edit, add})`, `focusWidgets`):
    Meus widgets, Modelos, Integrações (formulário com Testar, frequência e Avisos prontos) e editor com
    prévia ao vivo.
  - `js/app.js`: `W2()` (preferência `widgets2`, converte a antiga na primeira vez), `widgetCtx()`
    (`value`, `def`, `spark`, `series` com cache de `statsHistory` e `metricSeries`, redução para 240
    pontos), Área de widgets no topo de cada página (`pagePer` mede quantos ícones cabem embaixo),
    `paintWidgets()` a cada amostra/valor novo (nada com a aba escondida), menu rápido (botão direito ou
    segurar), modo "Organizar widgets" (arrastar entre Áreas, `−` remove, `+` adiciona).
  - 0.9.1 (`app.js`): Páginas reais montadas por `renderApps` a partir de `appPagesPlan(itens)` (apps
    colocados + soltos) e `widgetsOn(página)`; páginas de continuação (`data-cont`) quando os apps não
    cabem (`pagePer` mede); `data-real` em cada `.app-page` (`realOf`, `currentReal`, `goReal`);
    `fixLoosePlaces` antes de mover, `compactPages` depois. Arrastar widget vale sempre (alvo = Área ou
    página); alça `[data-wresize]` com `pointer*` + `sizeFor(colunas, linhas)`; barra `.w-area-bar`
    (− N colunas + / Automático) no modo Organizar, que também desenha uma página nova vazia no fim.
    `widgets.js`: `spanStyle(tamanho, colunas)` limita a largura às colunas da Área.

### Preferências (`/api/settings`, objeto do frontend)

`wallpaper` (`builtin:24` | `upload:<nome>` | `local`), `accentMode` (`auto`|`vibrant`|`manual`),
`manualHue`, `glass`, `blur`, `iconSize`, `widgets` (`files`, `storage`, `system`, `network`, `battery`),
`showStatus`, `reduceMotion`, `openIn` (`tab`|`same`), `urlMode` (`auto`|`domain`|`port`), `serverHost`,
`runtipiUrl`, `dock` (ids), `order`, `apps` (personalizações por id: nome, ícone, url, hidden), `links`
(atalhos), `favorites` (pastas), `welcomed`, `filesView`, `filesHidden`, `filesDisksOpen`, `sshHost`,
`sshPort`, `sshUser`, `sshMethod`, `termFontSize`, `diskForecast` (0.5.0, previsão de disco cheio),
`netMonitor` (0.8.0, padrão true), widget `internet` (0.8.0), `appMonitor` (0.7.0, padrão true), `appPages` (0.7.0, padrão true), `folders` (0.7.0,
`[{id,name,apps:[ids]}]`), `apps[id].noMonitor`, `widgets2` (0.9.0: `{v:1, areas:{"<página>":[ids]},
items:{id:{model?, format, size, title, caption, slots:[{metric, label?, unit?, decimals?, compact?, icon?,
spark?, max?, colors?:{warn,crit}|null}], layout, list:{max,bars}, chart:{type,range,stats}, bar,
click:{kind:auto|app|window|url|none, target}, look:{follow, opacity?, hue?, hideLabel?}}}}`; a antiga
`widgets` fica só para voltar de versão). 0.9.1: `widgets2.cols` (`{"<página>": 1–6}`, ausente =
automático), no formato Atalhos `folders:[{root, path, name?, icon?, color?(matiz)}]` (vazio = favoritas)
e `files:{layout: grid|list, showFree}`; `appPlace` (`{"<id do app ou folder:id>": página}`; sem entrada =
solto, vai para a última página com apps). 0.9.2: no formato Mídia `media:{rotate (min), caption}`;
`wallpaperMode` (`single` | `schedule`) e `wallpaperSchedule` (`[{at:"HH:MM", wp}]`). **Nunca** guardar
senhas aqui.

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
