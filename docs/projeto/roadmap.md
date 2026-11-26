# Roadmap

O que está **combinado e ainda não terminou**. O que já foi entregue está resumido em `historico.md`; o
que foi adiado ou sugerido sem data, em `ideias-futuras.md`; os porquês das escolhas, em `decisoes.md`.

Como se trabalha: uma versão por etapa, e o Bruno testa no servidor antes da próxima. Nada de privilégios
no container: o que precisa de root no host vai por SSH com chave restrita. Testar sempre também em tela de
celular (390 px), com prints.

## Estado atual (versão no arquivo `VERSION`)

Entregue e testado no servidor: tudo até a 0.39.0 (o Bruno confirmou em 26/11/2026: "Deu certo!"). Os planos
de 24 a 26/11 e a revisão de segurança de 19/11 estão completos. Nenhuma versão planejada.

## Próxima conversa (combinada em 24/11/2026, ainda sem versão)

- **Admin em casa / convidado fora**: pelo domínio público só convidado; em casa ou pelo Tailscale, admin com
  senha nas ações perigosas.
- **Funções novas pelo script do servidor** (rede, sensores, discos, Tailscale, tarefas agendadas,
  registros). Sem root no BrunOS e sem substituir o Runtipi.

## Em aberto

- Senha também para parar app e para remover lojas: o Bruno disse que não precisa por enquanto
  (17/11/2026). Adicionar loja passou a pedir na revisão de segurança de 19/11.
- Nextcloud na janela Arquivos ficou sem: zip, busca, editor, extrair, duplicados, mapa do espaço e envio em
  pedaços com retomada.
- Itens do `ideias-futuras.md` que o Bruno pedir passam para cá, com versão.

## Como lançar uma versão

Está no `CLAUDE.md` (seção "Como lançar uma versão").
