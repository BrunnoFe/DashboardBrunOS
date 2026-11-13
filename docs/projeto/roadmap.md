# Roadmap

O que está **combinado e ainda não terminou**. O que já foi entregue está resumido em `historico.md`; o
que foi adiado ou sugerido sem data está em `ideias-futuras.md`; os porquês das escolhas, em
`decisoes.md`.

Como se trabalha: uma versão por etapa, e o Bruno testa no servidor antes da próxima. Nada de privilégios
no container: o que precisa de root no host vai por SSH com chave restrita. Testar sempre também em tela de
celular.

## Aguardando teste no servidor

| Versão | O que |
|---|---|
| 0.12.3 | Liberação gradual do Ubuntu, "Conferir" por seção, lojas do Runtipi atualizadas antes de conferir (script 9) |
| 0.13.0 | Tela inicial nova: Disposições, Medidas, lugar e Tamanho livres, arrastar novo (decisão 48) |
| 0.13.1 | Ícones com tamanho fixo pela Régua (decisão 52) |

## Etapa 11 (0.13.1 → 0.16.0), combinada em 12/11/2026

Sessão de perguntas uma a uma; resumo aprovado ("Pode seguir."). Plano completo em `plano-etapa11.md`;
termos novos em `CONTEXT.md` (Régua, Tamanho do ícone, Espaço entre os ícones, Histórico, Período, Ponta
viva, Disco, Pasta de montagem, Formatar, Senha de formatação); porquês nas decisões 52–54. Quatro versões,
nesta ordem, cada uma testada no servidor antes da próxima.

| Versão | O que | Status |
|---|---|---|
| 0.13.1 | Ícones com tamanho fixo: Tamanho do ícone (4 degraus pela Régua) e Espaço entre os ícones | feito, aguardando teste no servidor |
| 0.14.0 | Históricos de 30 dias (`internal/tsdb`: servidor, containers, internet, leitura/escrita dos discos, integrações) e gráficos com eixo, valor no mouse, Período e Ponta viva | a fazer |
| 0.15.0 | Janela Discos: informações, SMART explicado, testes, desempenho, espaço, linha do tempo, ejetar | a fazer |
| 0.16.0 | Montar, desmontar e Formatar com Senha de formatação | a fazer |

## Em aberto

- Depois de testar a 0.13.0 no servidor: tirar a conversão das preferências antigas de widgets (ver
  `ideias-futuras.md`).
- Itens do `ideias-futuras.md` que o Bruno pedir passam para cá, com versão.

## Como lançar uma versão

Está no `CLAUDE.md` (seção "Como lançar uma versão").
