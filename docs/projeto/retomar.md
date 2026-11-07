# Como retomar o projeto

## Onde está tudo

- Repositório local do Bruno: `%USERPROFILE%\Documents\GitHub\DashboardBrunOS` (clone de
  `github.com/BrunnoFe/DashboardBrunOS`). Loja: `...\GitHub\RuntipiAppStore`.
- Memória pública: `CLAUDE.md` + `docs/projeto/`. Memória privada: `docs/privado/` (só nessa máquina,
  ignorada pelo git, **faça backup** dela por fora, ex.: OneDrive).
- Ferramenta de memória entre sessões: **ai-memory** (akitaonrails/ai-memory), instalada pelo Bruno
  para o Claude Code. Configuração do projeto em `.ai-memory.toml` (workspace `pessoal`, projeto
  `dashboardbrunos`).

## Começando uma sessão nova (Claude Code no VS Code)

1. Abra a pasta do repositório no VS Code e inicie o Claude Code nela.
2. O Claude Code lê o `CLAUDE.md` sozinho. Com o ai-memory instalado, ele também recebe o resumo das
   sessões anteriores no início.
3. Peça algo como: "Leia o CLAUDE.md e o docs/projeto/roadmap.md e vamos planejar a próxima etapa".
4. Ao terminar, peça para atualizar `roadmap.md`, `historico.md` e `docs/privado/sessoes.md`.

## VS Code + Claude Code + ai-memory

- A extensão **Claude Code** do VS Code usa as mesmas configurações do Claude Code de terminal
  (`~/.claude.json` para MCP e `~/.claude/settings.json` para hooks). O que o `install-mcp`/
  `install-hooks` instalou vale para os dois.
- Recomendado: MCP "session-aware" (separa a memória certinha por sessão/projeto):
  `ai-memory install-mcp --client claude-code --session-aware --apply`
- Conferir: `ai-memory doctor` no terminal e `/mcp` dentro do Claude Code (deve listar `ai-memory`).
- Uma vez por projeto, peça ao Claude Code: "Install ai-memory routing into this project." Ele
  acrescenta um bloco marcado (`<!-- ai-memory:start -->`) ao `CLAUDE.md` ensinando quando consultar a memória.
- O início de cada sessão recebe um resumo das anteriores (hook); o fim da sessão grava um resumo.

## ai-memory: passos únicos (no Windows, PowerShell)

Jeito fácil: rode o script que faz tudo abaixo perguntando antes de cada passo (provedor de LLM,
chave, `config.toml`, reinício do servidor, teste, hooks e bootstrap):

```powershell
powershell -ExecutionPolicy Bypass -File scripts\configurar-ai-memory.ps1
```

Provedores de LLM (servem para resumir as sessões e para o `bootstrap`; sem provedor o ai-memory
ainda grava as sessões, mas com resumos simples): **Gemini** (plano grátis, chave em
aistudio.google.com/apikey; no plano grátis o Google pode usar os dados para melhorar os produtos),
**Codex** (login do Codex CLI, assinatura ChatGPT), **LM Studio** (local e privado; modelo sem
"thinking"), **Anthropic** (pago, `claude-haiku-4-5`). A chave fica numa variável de ambiente do
usuário (`GEMINI_API_KEY` / `ANTHROPIC_API_KEY`), nunca em arquivo. Configuração em
`%LOCALAPPDATA%\ai-memory\config.toml` (`llm_provider`, `llm_model`, `llm_base_url`, `language`).

Passo a passo manual, se preferir:

```powershell
# 1. Conferir se o servidor do ai-memory está rodando e ligado ao Claude Code
ai-memory status --json

# 2. (se ainda não fez) ligar MCP e hooks ao Claude Code
ai-memory install-mcp   --client claude-code --apply
ai-memory install-hooks --agent  claude-code --apply

# 3. Semear a memória com este projeto (lê git log, README, docs/ e CLAUDE.md; precisa de um
#    provedor de LLM configurado no servidor do ai-memory)
cd "$env:USERPROFILE\Documents\GitHub\DashboardBrunOS"
ai-memory bootstrap --dry-run
ai-memory bootstrap

# 4. (opcional) dar acesso à memória também no app Claude Desktop (aba de chat):
ai-memory install-mcp --client claude-desktop --apply
#    depois feche e abra o Claude Desktop de novo (precisa do Node.js, usa o mcp-remote)
```

Dica do próprio ai-memory: iniciar o Claude Code com `ai-memory run claude` configura tudo na primeira vez.
No app Claude Desktop (chat), o ai-memory só funciona como MCP: ele não captura as conversas
automaticamente. A captura automática acontece no Claude Code (CLI, VS Code ou aba Code do desktop).

## Rodando localmente no Windows

- Go 1.24+ (`winget install GoLang.Go`) para `go vet`, `go test`, `go build` (o binário roda no
  Windows para testar a interface, mas a coleta de hardware é feita para Linux).
- Interface em modo demonstração: `cd web/static` e `python -m http.server 8765`, abrir
  `http://localhost:8765`.
- Scripts `tests/*.sh` e Docker: use o WSL2 ou o Docker Desktop, ou deixe o GitHub Actions rodar.
- Line endings: `.gitattributes` força LF (scripts `.sh` quebram com CRLF).
