# Runtipi, loja e publicação

## Como o BrunOS enxerga o Runtipi

- Pasta do Runtipi montada só para leitura em `/runtipi` (padrão do formulário: `/home/ptbruno/runtipi`,
  campo `BRUNOS_RUNTIPI_PATH`).
- Apps instalados: `apps/<loja>/<app>/config.json` (nome, porta, versão, categorias) +
  `app-data/<loja>/<app>/app.env` (porta e domínio escolhidos na instalação; o resto do arquivo tem
  senhas e é ignorado) + `metadata/logo.jpg`.
- Configuração global: `state/settings.json` (`localDomain`, `internalIp`, `port` do painel) e `.env`
  (`NGINX_PORT`, `LOCAL_DOMAIN`, `INTERNAL_IP`).
- URL de um app: domínio local `https://<app>.<localDomain>` quando exposto localmente, senão
  `http://<servidor>:<porta>` (preferência `urlMode`).
- Status: containers do projeto compose `<app>_<loja>` (todos rodando = verde; alguns = amarelo; nenhum = cinza).
- Painel do Runtipi: `http://<servidor>[:porta]`; loja: `<painel>/app-store` (ícone "Loja de apps" no
  dock; endereço configurável em Configurações → Apps → Endereço do Runtipi).

## O app na loja (`runtipi-app/brunos/` → `RuntipiAppStore/apps/brunos/`)

- `config.json`: `id: brunos`, `port: 8790`, `dynamic_config: true`, `exposable: true`, `uid/gid 1000`,
  `supported_architectures: [amd64, arm64]`, `tipi_version` (incrementar a cada versão), `version`,
  `created_at`/`updated_at` em **milissegundos**.
- Campos do formulário: `BRUNOS_RUNTIPI_PATH`, `BRUNOS_PUID` (1000), `BRUNOS_PGID` (1000),
  `BRUNOS_DOCKER_ACTIONS` (booleano, falso), `BRUNOS_TERMINAL` (booleano, verdadeiro).
- `docker-compose.json` (formato dinâmico do Runtipi): serviço `brunos` (principal, porta interna 8080,
  usuário do formulário, `readOnly`, `capDrop: [ALL]`, `no-new-privileges`, 128M, volumes `/data`,
  `/runtipi` ro, `/home`→`/files/home`, `/mnt`→`/files/mnt`, pasta do Runtipi em `/files<caminho>` ro,
  `/proc`→`/host/proc` ro, `/etc/os-release` e `/etc/hostname` ro) e serviço `brunos-docker-proxy`
  (`wollomatic/socket-proxy:1`, usuário 0:0, ro, sem capabilities, 32M, `-allowfrom=brunos`,
  `-allowGET` versão/lista/stats, `-allowPOST` restart, socket do Docker ro).
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

## Publicação (GitHub Actions)

`.github/workflows/docker.yml`:
- job **test** (push na main, PRs, tags): confere o `vendor/` contra os módulos oficiais, `go vet`,
  `go test -race`, build, gofmt, `docker build`, `tests/smoke.sh`.
- job **publish** (main ou tag `v*`, depois do test): lê `VERSION`; se `ghcr.io/brunnofe/brunos:<versão>`
  já existe, pula; senão publica `<versão>` e `latest` para amd64 e arm64 (QEMU + buildx, cache gha).
- O pacote no GHCR precisa estar **público** (o Bruno já deixou), senão o Runtipi não baixa sem login.

## Fluxo de PR na loja

Histórico: PR #1 (0.1.0) mesclado; PR #2 (0.2.0 → 0.3.0, branch `brunos-0.2.0`) mesclado em 07/11/2026.
A cada versão: branch nova a partir da `main` da loja, copiar os dois JSON, `bun test`, PR com
descrição em pt-BR do que mudou e se o formulário/montagens mudaram.

## Acesso do Claude ao GitHub

Na nuvem (claude.ai/code) o Claude tem os dois repositórios anexados à sessão: pode fazer push em
branches, abrir PRs pela API REST e ler os runs do Actions, mas **não** pode criar tags nem usar
GraphQL. No Claude Code local (VS Code), usa as credenciais do Git do Bruno.
