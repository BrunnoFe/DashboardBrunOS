# Runtipi, loja e publicação

## Como o BrunOS enxerga o Runtipi

- Pasta do Runtipi montada só para leitura em `/runtipi` (campo `BRUNOS_RUNTIPI_PATH` do formulário; o
  padrão sugerido está em `runtipi-app/brunos/config.json`).
- Apps instalados: `apps/<loja>/<app>/config.json` (nome, porta, versão, categorias) +
  `app-data/<loja>/<app>/app.env` (porta e domínio escolhidos na instalação; o resto do arquivo tem
  senhas e é ignorado) + `metadata/logo.jpg`.
- Configuração global: `state/settings.json` (`localDomain`, `internalIp`, `port` do painel) e `.env`
  (`NGINX_PORT`, `LOCAL_DOMAIN`, `INTERNAL_IP`).
- URL de um app: domínio local `https://<app>.<localDomain>` quando exposto localmente, senão
  `http://<servidor>:<porta>` (preferência `urlMode`).
- Status: containers do projeto compose `<app>_<loja>` (todos rodando = verde; alguns = amarelo; nenhum = cinza).
- Painel do Runtipi: `http://<servidor>[:porta]` (endereço configurável em Configurações → Apps →
  Endereço do Runtipi). Página de um app instalado: `<painel>/apps/<loja>/<app>`; tela de instalar:
  `<painel>/app-store/<loja>/<app>` (a Loja do BrunOS abre essa tela).
- Lojas (Runtipi 4): cada loja é um repositório baixado em `repos/<loja>/` (`apps/<app>/config.json`,
  `docker-compose.json` ou `docker-compose.yml` com `x-runtipi`, `metadata/logo.jpg` e
  `metadata/description.md`). A loja oficial fica na pasta `migrated`. O `runtipi-cli` lista, adiciona,
  remove e atualiza lojas, e inicia, para, atualiza e desinstala apps; **não instala** (a instalação tem
  formulário e só existe no painel). A janela Loja do BrunOS lê `repos/` (decisão 59).
- Uma loja pode ser um **ramo** de um repositório: endereço `https://github.com/<usuário>/<repo>/tree/<ramo>`
  (o Runtipi clona só aquele ramo; pasta dentro do repositório não é aceita). O nome técnico da loja é o
  nome dado ao adicionar (`runtipi-cli appstore add <nome> <endereço>`, até 16 letras) e aparece em todos
  os caminhos dos apps instalados, por isso não deve mudar depois.

## Garimpo (lojas convertidas, 0.19.0, decisão 60)

- Repositório `BrunnoFe/Garimpo`: o conversor (`garimpo/converter.py`, Python + PyYAML; Pillow e
  `rsvg-convert` para os logos) no ramo `main`, e uma loja do Runtipi por ramo: `casaos` (loja do
  CasaOS/ZimaOS, Apache-2.0) e `linuxserver` (LinuxServer-AppStore, GPL-3.0). Origens em `origens.json`.
- GitHub Actions todo dia às 03:17 (Brasília), quando o código muda e pelo botão: testes do conversor →
  converte → `confere.py` (segunda conferência de segurança) → testes oficiais da loja do Runtipi
  (`apps/__tests__` com `bun test`) → publica no ramo só se algo mudou.
- Formato gerado: `docker-compose.yml` com `x-runtipi` (`schema_version: 2`; serviço principal com o
  nome do app e a versão igual à tag da imagem; `is_main`/`internal_port` no serviço da página web, que
  pode ser um nginx na frente), `min_tipi_version` 4.7.0, porta única na loja (quem já foi publicado
  guarda a sua), `tipi_version` sobe quando a receita muda (resumo em `.resumos/` no ramo).
- `garimpo.json` na raiz do ramo: `nome`, `selo`, `fonte`, `licenca`, `sobre`, contagens. O BrunOS lê.
- Primeira conversão (17/11/2026): CasaOS 86 apps (26 recusados, 71 já na oficial), LinuxServer 188
  (12 recusados, 49 já na oficial); os dois ramos passaram nos testes oficiais (1121 e 2447 testes).
- Pegadinhas: os nomes da LinuxServer vêm com prefixo `linuxserver-` (o conversor tira, campo
  `tira_prefixo`); apps com vários serviços mantêm o nome do serviço principal como id (renomear quebraria
  os outros serviços que falam com ele pelo nome); os testes oficiais pedem `bun install --ignore-scripts`
  (o gancho de git da loja oficial falha fora de um repositório).

## O app na loja (`runtipi-app/brunos/` → `RuntipiAppStore/apps/brunos/`)

- `config.json`: `id: brunos`, `port: 8790`, `dynamic_config: true`, `exposable: true`, `uid/gid 1000`,
  `supported_architectures: [amd64, arm64]`, `tipi_version` (incrementar a cada versão), `version`,
  `created_at`/`updated_at` em **milissegundos**.
- Campos do formulário: `BRUNOS_RUNTIPI_PATH`, `BRUNOS_PUID` (1000), `BRUNOS_PGID` (1000),
  `BRUNOS_DOCKER_ACTIONS` (booleano, falso), `BRUNOS_TERMINAL` (booleano, verdadeiro).
- Variável `BRUNOS_RUNTIPI_HOST` (0.12.0) = `${BRUNOS_RUNTIPI_PATH}`: o BrunOS fica sabendo o caminho da
  pasta do Runtipi **no servidor** e o põe em `/etc/brunos-host.conf` nos comandos do script, para o
  `runtipi-cli app update` (atualizar apps pela interface).
- `docker-compose.json` (formato dinâmico do Runtipi): serviço `brunos` (principal, porta interna 8080,
  usuário do formulário, `readOnly`, `capDrop: [ALL]`, `no-new-privileges`, 128M, volumes `/data`,
  `/runtipi` ro, `/home`→`/files/home`, `/mnt`→`/files/mnt`, pasta do Runtipi em `/files<caminho>` ro,
  `/proc`→`/host/proc` ro, `/etc/os-release` e `/etc/hostname` ro) e serviço `brunos-docker-proxy`
  (`wollomatic/socket-proxy:<versão>@sha256:<digest>`, congelada desde a 0.29.0, usuário 0:0, ro, sem
  capabilities, 32M, `-allowfrom=brunos`, `-allowGET` versão/lista/stats, `-allowPOST` restart, socket do
  Docker ro). Para subir a versão do proxy: escolher uma com pelo menos 2 semanas, pegar o digest com
  `docker buildx imagetools inspect wollomatic/socket-proxy:<versão>` (linha `Digest:` do índice) e trocar
  só no compose; o `tests/smoke.sh` lê a imagem de lá e falha se faltar o digest.
- `metadata/description.md` e `metadata/logo.jpg` (512 px, marca original: órbita com lua; não usar a
  marca do Umbrel).

### Pegadinhas do formato (validado por `@runtipi/common` 0.8.0, zod)

- Não existe `restart` nem `propagation` de bind no JSON dinâmico. Por isso discos montados **depois**
  que o container subiu não aparecem dentro dele (sem `rslave`); reiniciar o app resolve.
- `environment` é objeto (`{"CHAVE": "valor"}`), volumes são objetos `{hostPath, containerPath, readOnly}`.
- `deploy.resources.limits.memory` funciona; `user` aceita `${VAR}:${VAR}`.
- Ao adicionar um campo de formulário, apps já instalados recebem string vazia: o backend precisa
  tratar vazio como padrão (ex.: `BRUNOS_TERMINAL` vazio = ligado).
- A loja tem testes próprios: `bun install && bun test` (18 testes de schema). Não commitar `node_modules`
  nem a alteração que o `bun` faz no `bun.lockb`.

### Fatos do Runtipi 4.12 (lidos no código-fonte dele em 17/11/2026)

- Instalar é `POST /api/app-lifecycle/<app>:<loja>/install` (formulário + porta, domínio, exposto,
  `localSubdomain` `^[a-zA-Z0-9-]{1,63}$`, `enableAuth`, `isVisibleOnGuestDashboard`, `skipRun`); o estado
  fica no banco do Runtipi, então gravar arquivos não basta. Editar as respostas é `PATCH .../update-config`
  (não reinicia); `maxBackups` só vale por ele (na instalação é ignorado).
- A API aceita `Authorization: Bearer` com um JWT assinado pelo segredo do CLI
  (`/data/.secrets/cli-secret` dentro do container `runtipi`).
- Ajustes do usuário: `user-config/<loja>/<app>/docker-compose.yml` e `app.env`, juntados pelo docker
  compose em todo início (inclusive o da instalação) enquanto `userConfigEnabled` não for desligado, sem
  conferir nada (por isso o BrunOS confere). Desinstalar apaga a pasta do app e o `app-data`, mas **não** o
  `user-config`.

## Publicação (GitHub Actions)

`.github/workflows/docker.yml`:
- job **test** (push na main, PRs, tags): confere o `vendor/` contra os módulos oficiais, `go vet`,
  `go test -race`, build, gofmt, `docker build`, `tests/smoke.sh`.
- job **publish** (main ou tag `v*`, depois do test): lê `VERSION`; se `ghcr.io/brunnofe/brunos:<versão>`
  já existe, pula; senão publica `<versão>` e `latest` para amd64 e arm64 (QEMU + buildx, cache gha).
- O pacote no GHCR precisa estar **público** (o Bruno já deixou), senão o Runtipi não baixa sem login.

## Fluxo de PR na loja

A cada versão: branch nova a partir da `main` da loja, copiar os dois JSON, `bun test`, PR com
descrição em pt-BR do que mudou e se o formulário/montagens mudaram. Com o check `test` verde, o Claude
mescla sozinho (regra no `CLAUDE.md`).

## Acesso do Claude ao GitHub

Na nuvem (claude.ai/code) o Claude tem os dois repositórios anexados à sessão: pode fazer push em
branches, abrir PRs pela API REST e ler os runs do Actions, mas **não** pode criar tags nem usar
GraphQL. No Claude Code local (VS Code), usa as credenciais do Git do Bruno.
