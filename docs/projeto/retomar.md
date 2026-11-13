# Como retomar o projeto

## Onde está tudo

- Repositório local do Bruno: `%USERPROFILE%\Documents\GitHub\DashboardBrunOS` (clone de
  `github.com/BrunnoFe/DashboardBrunOS`). Loja: `...\GitHub\RuntipiAppStore`.
- Memória pública: `CLAUDE.md` + `docs/projeto/`. Memória privada: `docs/privado/` (só nessa máquina,
  ignorada pelo git, **faça backup** dela por fora, ex.: OneDrive). Ela guarda os detalhes de segurança e
  do servidor e o script de configuração do ai-memory.
- Memória entre sessões: **ai-memory** (akitaonrails/ai-memory), com configuração do projeto em
  `.ai-memory.toml` (workspace `pessoal`, projeto `dashboardbrunos`). O bloco do `CLAUDE.md` marcado
  `ai-memory` explica quando consultar; as skills e o servidor do ai-memory ficam na conta do Bruno
  (`~/.claude`, `%LOCALAPPDATA%\ai-memory`), não neste repositório.

## Começando uma sessão nova (Claude Code no VS Code)

1. Abra a pasta do repositório no VS Code e inicie o Claude Code nela.
2. O Claude Code lê o `CLAUDE.md` sozinho e, com o ai-memory ligado, recebe o resumo das sessões
   anteriores.
3. Peça algo como: "Leia o CLAUDE.md e o docs/projeto/roadmap.md e vamos planejar a próxima etapa".
4. Ao terminar, peça para atualizar `roadmap.md`, `historico.md` e `docs/privado/sessoes.md`.

Se o ai-memory não estiver respondendo: `ai-memory doctor` no terminal e `/mcp` dentro do Claude Code (deve
listar `ai-memory`).

## Rodando localmente no Windows

- Tudo roda pelo WSL (Ubuntu-24.04), chamado do PowerShell: veja "Comandos do dia a dia" no `CLAUDE.md`.
- Interface em modo demonstração: `cd web/static` e `python -m http.server 8765`, abrir
  `http://localhost:8765`.
- Scripts `tests/*.sh` e Docker: WSL2 com a integração do Docker Desktop, ou deixe o GitHub Actions rodar.
- Fim de linha: `.gitattributes` força LF (scripts `.sh` quebram com CRLF).
