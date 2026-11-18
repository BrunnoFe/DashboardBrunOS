# Roadmap

O que está **combinado e ainda não terminou**. O que já foi entregue está resumido em `historico.md`; o
que foi adiado ou sugerido sem data, em `ideias-futuras.md`; os porquês das escolhas, em `decisoes.md`.

Como se trabalha: uma versão por etapa, e o Bruno testa no servidor antes da próxima. Nada de privilégios
no container: o que precisa de root no host vai por SSH com chave restrita. Testar sempre também em tela de
celular.

## Estado atual (versão no arquivo `VERSION`)

- Entregue e testada no servidor: tudo até a 0.21.2 (inclusive 0.23.0, cujo retorno virou a 0.23.1).
- **Aguardando teste do Bruno**: 0.23.1 (guias no Arquivos; duas janelas independentes) e a 0.22.0 (editor
  do arquivo de Ajustes em Configurar → Avançado), que não tem confirmação explícita.
- O Bruno vai trazer um retorno com melhorias; ele entra aqui com versão.

## Em aberto

- Tirar a conversão das preferências antigas de widgets (`widgets2` v1/v2 → v3, em `app.js`) quando o Bruno
  quiser; a 0.13.0 já foi testada.
- Senha também para parar app e para adicionar ou remover lojas: o Bruno disse que não precisa por
  enquanto (17/11/2026).
- Itens do `ideias-futuras.md` que o Bruno pedir passam para cá, com versão.

## Como lançar uma versão

Está no `CLAUDE.md` (seção "Como lançar uma versão").
