# Arquitetura

Mapa de pacotes, rotas e dados, com as regras que não se deduzem rápido do código. Detalhes finos: leia o
código (a versão longa, anterior a 26/11/2026, está no git). Porquês: `decisoes.md`. Termos: `CONTEXT.md`.

## Visão rápida

```text
navegador ──HTTP(S)──> Traefik do Runtipi ──> container "brunos" (Go, :8080)
                                                 ├─ /data            (conta, sessões, preferências, dados)
                                                 ├─ /runtipi  (ro)   (apps instalados, app.env, lojas)
                                                 ├─ /files/home, /files/mnt (+ pasta do Runtipi ro) → Arquivos
                                                 ├─ /host/proc (ro), /host/os-release, /host/hostname
                                                 ├─ /sys             (hwmon, power_supply, block, drm)
                                                 ├─ unix:///sock/docker.sock → socket-proxy → docker.sock (ro)
                                                 └─ SSH → servidor (Terminal e script fixo)
```

Um binário estático serve a API (`/api/*`) e a interface (`/`, `go:embed`).

## Backend (Go 1.27)

### Variáveis de ambiente (`main.go`)

| Variável | Padrão | Uso |
|---|---|---|
| `BRUNOS_ADDR` | `:8080` | endereço HTTP |
| `BRUNOS_DATA` | `/data` | dados persistentes |
| `BRUNOS_FILES` | `/files` | raiz do Arquivos (descoberta automática) |
| `BRUNOS_ROOTS` | — | lista manual `Nome=/caminho[:ro];…` (desliga a descoberta e os discos mapeados) |
| `BRUNOS_RUNTIPI` | `/runtipi` | pasta do Runtipi |
| `BRUNOS_RUNTIPI_HOST` | — | caminho dela **no servidor** (para o script) |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | no Runtipi: `unix:///sock/docker.sock`; `off` desliga |
| `BRUNOS_DOCKER_ACTIONS` | `false` | permite reiniciar apps |
| `BRUNOS_TERMINAL` | `true` | `false`/`off` desliga o terminal |
| `HOST_PROC`, `HOST_PASSWD` | `/host/proc`, `/host/passwd` | processos e rede do host |
| `HOST_OS_RELEASE`, `HOST_HOSTNAME_FILE` | `/host/os-release`, `/host/hostname` | informações do sistema |
| `BRUNOS_MAX_UPLOAD_GB` | `50` | limite por envio |
| `BRUNOS_ALLOW_PUBLIC_SETUP` | `false` | primeira execução só aceita IP privado/Tailscale |
| `BRUNOS_TRUSTED_PROXIES` | Traefik do Runtipi | de quem vale `X-Forwarded-For/-Proto` |
| `BRUNOS_HOSTS` | — | nomes de endereço aceitos a mais |

Subcomando `brunos healthcheck` (usado pelo `HEALTHCHECK` da imagem).

### Arquivos do pacote `main`

- `main.go`: rotas, middleware, `sudoRoutes`. `apps.go`: ações, logs e vigia de apps (`watchApps`/
  `checkApps`, a cada minuto: porta do app, forma de acesso, janela). `watch.go`: bateria, discos
  (desmontado, quente, cheio), avisos de login. `monitors.go`: rotinas (inclui `gpuLoop`). `settings.go`,
  `store.go` (Loja), `install.go` (instalar/Ajustes), `logsnap.go` (app ocupado e "Logs de antes").
- `guard.go`: proxies confiáveis e `hostCheck` (421 para nomes de fora: aceita IPs, nomes sem ponto,
  `.local .lan .home .internal .home.arpa .localdomain .localhost .ts.net`, o domínio do Runtipi, os do app
  `brunos`, a preferência `allowedHosts` e `BRUNOS_HOSTS`).
- `limits.go`: prazo do corpo dos pedidos (anda junto com os dados nos envios), prazo de escrita nos SSE e
  teto de conexões. `web/web.go`: estáticos com gzip e ETag; rota desconhecida cai no SPA.

### Pacotes (`internal/`)

- `auth`: usuários e sessões (30 dias deslizante, guarda só resumos), limitador por IP (`limiter.go`),
  `OnBlock`. Parâmetros: `docs/privado/seguranca.md`.
- `store`: gravação atômica (`WriteJSON`, `WriteFile`, `ReadJSON`).
- `secret`: chave de arquivo + AES-256-GCM, só para credenciais de integrações.
- `termkey`: Chave do Terminal cifrada pela senha; aberta por Sessão só na memória; `Rekey` ao trocar a senha.
- `sshknown`: TOFU compartilhado (uma instância por arquivo); segue a ordem do OpenSSH (ED25519 primeiro).
- `terminal`: até 4 Abas, 12 aberturas/min, buffers de 256 KB para reabrir (`replay=1`). Aba pertence à
  Sessão que abriu; sem leitor, confere com `ps` se algo roda e fecha após `termIdleMin` (padrão 15, 0 =
  nunca); Bloqueio não fecha; aviso `terminal-done`.
- `hostcmd`: cliente SSH do **script fixo** do servidor (`HostScript`). `Test` não abre sessão; `ReadAllowed`/
  `ActAllowed`/`StoreActAllowed` conferem o pedido antes de mandar (o script confere de novo).
  `ScriptVersion` sobe a cada pedido novo; `SetupVersion` só quando muda o que se cola no servidor. Script
  antigo = `ErrOldScript`. Desenho de segurança: `docs/privado/seguranca.md`.
- `power`: bateria a cada 5 s, quedas, desligamento seguro (off → armed → countdown → running →
  done/cancelled/failed; sair da simulação exige `testedAt`; na bateria tenta a cada 1 min sem limite),
  limite de carga (`charge.go`) e desligar/reiniciar pela interface (`manual.go`, temporizador de 1 min no
  servidor, cancelável).
- `energy`: Medidor (tomada Tuya local → bateria → estimativa), consumo, custo e assistente Tuya.
- `sysinfo`: coleta a cada 2 s com ouvintes, 15 s sem. CPU, memória, temperatura (hwmon), ventoinha,
  bateria, discos (partições reais ≥ 1 GiB), rede sem interfaces virtuais. `drivetemp.go`: no máximo 1×/min e
  HD só de novo se `/sys/block/sdX/stat` mudou (ler pode acordar). `gpu.go`: Intel (frequência, uso pelo RC6)
  e AMD pelo `/sys`; RAPL e NVIDIA pelo pedido `gpu` do script (`SetGPUHost`).
- `history` + `tsdb`: Históricos em três camadas (1 min × 48 h, 5 min × 7 dias, 20 min × 30 dias), até 400
  séries; `tsdb/snapshot.bin` a cada 6 h e no desligamento + `journal.log` a cada 5 min. Séries:
  `sys.cpu|mem|temp|rx|tx|fan|bat`, `app.<projeto>.cpu|mem|up`, `net.lat|up`, `disk.<sda>.rd|wr|busy|temp`,
  `gpu.<id>.busy|freq|temp|power|vram`.
- `diskhist`: um ponto por dia (180 dias); previsão de cheio por regressão dos últimos 30 dias (aviso só com
  `diskForecast` ligada). `disk-full` ≥ 90 %.
- `smart`: um disco por vez (50 s; temperatura 30 s); USB tenta `sat`, depois `auto`, e guarda o modo por
  número de série; travou → `noSmart` e nunca mais lê sozinho. Testes do próprio disco.
- `disks`: janela Discos (`lsblk` + fstab + `by-id` a cada 10 min e quando `/proc/diskstats` muda), Linha do
  tempo, velocidade, ejetar, montar/desmontar/renomear/formatar (formatar pede senha do BrunOS e Senha de
  configuração; `Release` solta a pasta no Arquivos antes de desmontar).
- `procs`: processos por `/host/proc`, com segredos do comando mascarados (`mask.go`).
- `netmon`: TCP 443 em três provedores a cada minuto; 2 min sem nenhum = `internet-down`.
- `events`: central do sino (500 avisos / 30 dias). `Emit` com `Key` não repete enquanto ativo; `Resolve`
  fecha. `Subscribe` alimenta o SSE.
- `logins`: aparelhos conhecidos (navegador|sistema|lugar). `notes`: texto dos widgets de Notas.
- `dockerc`: cliente mínimo da API Docker (containers, stats, start/stop/restart, logs com `Demux`).
- `appmon`: CPU, memória e disponibilidade por app (24 h na memória); avisa só após 2 min fora.
- `framecheck`: o app aceita Janela de app? (`X-Frame-Options` e `frame-ancestors`; cache 6 h, "não sei" 5 min).
- `access`: o domínio do app responde pelo Traefik? (SNI + Host no IP interno; "up" vale 10 min, "down" 2).
- `icons`: Ícones guardados (envio até 1 MB, formato pelo conteúdo, nome = hash; catálogo baixado do
  jsDelivr uma vez). Servidos com tipo fixo, `nosniff` e CSP `sandbox`.
- `runtipi`: apps instalados (cache 20 s; do `app.env` **só** as chaves de uma lista) e a Loja
  (`catalog.go`: `repos/<loja>/apps/`, sem seguir symlink, tamanhos limitados, `garimpo.json`, cache 2 min;
  `recipe.go`: `Priv`, os privilégios que a receita pede). Projeto compose = `<app>_<loja>`.
- `appcfg`: Ajustes no `user-config` (edita a árvore `yaml.v3`, preserva o resto; `Risks` impede acrescentar
  algo perigoso; `CheckRaw` para o editor) e leitura da receita (`recipe.go`, mesmas frases do Garimpo).
- `files`: raízes com `os.Root`; discos só se mapeados (`disks.json`; sem ele, os pontos de montagem).
  Lixeira em `<raiz>/.brunos-trash/`; edição de texto até 5 MB (409 se mudou); envio em pedaços (até 32 MB,
  pendentes > 48 h apagados); extrair com orçamento de espaço e sem links; duplicados e mapa com limites de
  tempo; tipos de mídia próprios; `media.go` lê o cabeçalho de MKV/WebM/MP4; `protect.go` (ocultos da pasta
  pessoal só leitura, segredos fora do Arquivos); `remote.go` (lugares remotos: Nextcloud; o que não existe
  neles responde `ErrRemoteUnsupported`).
- `updates`: apps (loja × instalado pelo `tipi_version`), Linux e o próprio script. A atualização roda solta
  no servidor e o BrunOS acompanha (até 3 h, retoma ao reiniciar). `DockerErr`: o `runtipi-cli` devolve 0
  mesmo quando a imagem não baixa. Troca do script só se `ScriptSignature()` não estiver vazia (CI).
- `integrations`: um arquivo por Tipo (AdGuard, Speedtest, Immich, Plex, WatchYourLAN, Tailscale,
  Transmission, Syncthing, Nextcloud, clima). `kinds.go` (catálogo de Métricas e Avisos), `store.go`
  (credenciais cifradas; a API só devolve `hasSecret`; `Secret2`), `hub.go` (busca só o que está em uso:
  com alguém no SSE no intervalo; gráfico/aviso a cada 5 min; `Live`/`LiveEvery`), `series.go`,
  `actions.go` (Ações: AdGuard, Transmission), `webdav.go` (Nextcloud como pasta), `ssrf.go` (destinos
  proibidos), `AppWatch` (avisa uma vez de app compatível novo). Métricas com `_` ficam só no cache.

### API

Tudo em `/api/*` (menos health e public) exige o cookie de sessão **e o código da aba** (`key` do login,
guardado em `localStorage` `brunos.k`; vai em `X-BrunOS`, ou `?k=` em imagem/download/SSE; logos, ícones e
papéis de parede dispensam). Métodos que alteram exigem `X-BrunOS` e mesma origem. Ações destrutivas
(`sudoRoutes`) respondem `403 {"reauth": true}` sem senha confirmada nos últimos 5 min
(`POST /api/auth/reauth`). Bloqueio com senha: o resto responde 423. Erros: `{"error": "…em pt-BR"}`.

```text
/api/health · /api/public[/wallpaper]
/api/auth/ setup login logout password me lock unlock reauth
/api/settings (JSON livre, até 512 KB) · /api/wallpapers[/{name}] · /api/icons[/{name} | /di/{nome}]
/api/stats · /api/stats/stream (SSE: init, dados, event notify|metrics|note) · /api/stats/history?range=
/api/history?serie=&periodo=5m|1h|24h|48h|7d|30d (prefixos sys. app. net. disk.) · /api/history/series
/api/system · /api/system/restart (o próprio container)
/api/apps[/logo] · /api/usage · /api/apps/ restart start stop containers history logs logs/stream logs/prev
/api/processes · /api/processes/cmd?pid= (sudo) · /api/internet
/api/files/ roots disks list raw zip search media mkdir newfile rename paste delete text
   upload upload/{start,chunk,status,finish,cancel} extract duplicates usage
   trash[/restore|delete|empty]
/api/terminal/ sessions stream open input resize close forget busy adopt key key/unlock
/api/notes/{id} · /api/events[/read|clear]
/api/power[/setup|config|cancel|test|charge|manual|manual/cancel]
/api/disks[/forecast|refresh|timeline|state|errors|speed|eject|mount|unmount|label|format]
/api/smart[/refresh|retry|selftest]
/api/updates[/check|config|run|backup-size|script]
/api/store · /api/store/ app logo form install reconfig status settings settings/raw stores
/api/integrations[/{id}] · /api/integrations/ kinds suggest test geocode plex/find
/api/integrations/{id}/ action image · /api/metrics/series
/api/tuya/ cloud scan apply (sudo)
```

Primeira conexão SSH (terminal ou energia) devolve 409 com a impressão digital; o cliente reenvia com
`trust`. Instalar app com privilégios exige `acceptRisks` e a Senha de configuração; app ocupado no
Runtipi → 409 `busy`.

### Dados em `/data` (todos 0600)

`auth.json`, `sessions.json`, chaves e segredos (`docs/privado/seguranca.md`), `terminal_key.json`,
`known_hosts.json`, `settings.json` + `settings-backup/`, `wallpapers/`, `icons/` (+ `icons/di/`),
`img-cache/`, `disks.json` (discos mapeados), `disk-events.json` (Linha do tempo, não confundir),
`disk-history.json`, `smart.json` (`v: 2`), `events.json`, `logins.json`, `power.json`,
`power-events.json`, `energy.json`, `updates.json`, `internet.json`, `uploads.json`, `tsdb/`,
`integrations.json`, `integrations-seen.json`, `metric-history.json`, `tuya-cloud.json`, `notes.json`.

## Frontend (`web/static`)

- `index.html`; `css/app.css` (base), `extra.css` (componentes), `store.css` (só com a Loja). Zoom, escala
  (`--hs`/`--ss`) e container queries: o arranjo das janelas segue a largura da janela.
- `assets/`: fontes Inter, papéis de parede + miniaturas, ícones, `demo-app.html`. `vendor/`: xterm.js e
  CodeMirror, sob demanda.
- **Base**: `app.js` (estado `S`, `DEFAULTS`, tela inicial, dock `SYS`, Configurações, Ctrl+K, Área de
  widgets, arrastar), `api.js` (`call` lança erro com `.status`/`.data`; modo demonstração quando não há
  `/api`; envios > 16 MB em pedaços retomáveis), `ui.js` (utilitários, ícones, diálogos, menus), `safe.js` e
  `access.js` (funções puras testadas).
- **Janelas**: `windows.js` (float, sheet, full; Janelas `sys:<nome>` minimizam para o ícone do dock; Voltar
  minimiza) + `winmath.js` (puro), `appwindow.js` (iframe do app ou "Só em aba").
- **Telas**: `files.js` (+ `filetabs.js`, um estado por janela/guia; `filetools.js`; `editor.js`),
  `terminal.js`, `store.js` (+ `storeforms.js`), `disks.js` (resumo) e `disksapp.js`, `widgetsapp.js`,
  `integapp.js`, `appinfo.js`, `updates.js` (+ `updscreen.js`: só recarrega quando responde a versão de
  destino), `power.js`, `powermenu.js`, `energy.js`, `tuya.js`, `battery.js`, `internet.js`,
  `notifications.js` (+ `notifdetails.js`: todo aviso nível Aviso precisa de explicação, há teste),
  `adguard.js`, `lockscreen.js`, `notes.js`, `icons.js`, `scale.js`, `novidades.js` (**acrescentar a cada
  versão**).
- **Widgets**: `widgets.js` (Tamanhos, Formatos, Métricas da fonte `brunos` calculadas no navegador,
  Modelos, Torrents), `layout.js` (encaixe puro: lugares, arrastar, compactar, Quadro), `charts.js` (SVG
  puro, `mountChart`).
- Atalhos globais ignoram eventos dentro de `.xterm`.

### Preferências (`/api/settings`)

**Nunca** guardar senhas aqui.

- Aparência: `wallpaper` (`builtin:N` | `upload:<nome>` | `local`), `wallpaperMode` (`single`|`schedule`),
  `wallpaperSchedule`, `accentMode`, `manualHue`, `glass`, `blur`, `reduceMotion`.
- Ícones e tela inicial: `iconSize` (`p|m|g|e`), `iconGap`, `lastRow`, `homeFit`, `mobileSplit`, `order`
  (ids e `folder:<id>`), `folders`, `appPages`, `appPlace`, `pageCfg` (`{"<página>":{disp, wm, am}}`),
  `apps[id]` (nome, ícone, url, hidden, noMonitor, urlMode, forceTab), `links`, `icon`
  (`auto|letter|builtin:|di:|up:`).
- Widgets: `widgets2` (`v: 3`, `areas:{"<página>":[{id,x,y}]}`, `items:{id:{format, w, h, title, slots,
  look, tor…}}`).
- Comportamento: `openIn` (`tab|same|brunos`), `urlMode` (`auto|domain|port`), `serverHost`, `runtipiUrl`,
  `winStyle`, `dockWithMax`, `winLimitPc`, `winLimitMobile`, `dock`, `dockHide`, `showStatus`, `welcomed`,
  `allowedHosts`, `appMonitor`, `netMonitor`, `diskForecast`, `storeFavs`.
- Arquivos e terminal: `favorites`, `filesView`, `filesHidden`, `filesDisksOpen`, `sshHost`, `sshPort`,
  `sshUser`, `sshMethod`, `termFontSize`, `termIdleMin`.
- Por aparelho, no `localStorage`: `brunos.scale`, `brunos.win.<id>`, `brunos.seenVersion`, guias do Arquivos.

## Testes

- `go test -race ./...` (inclui servidor SSH em processo para o terminal).
- `tests/*.test.mjs` (`node --test`): encaixe, janelas, acesso, ícones, segurança, detalhes dos avisos.
- `tests/apitest.py` contra o servidor de `tests/fixture.sh <dir>` (precisa ser novo: faz o setup).
- `tests/script-*.sh`: o script do servidor de verdade, como root, com ferramentas falsas.
- `tests/smoke.sh <imagem>`: socket-proxy + OpenSSH + BrunOS com as restrições do Runtipi.
- CI (`.github/workflows/docker.yml`): confere `vendor/`, `govulncheck`, vet, testes, gofmt, imagem, smoke,
  assinatura do script e publicação quando `VERSION` é nova. `pages.yml` publica a demonstração.
