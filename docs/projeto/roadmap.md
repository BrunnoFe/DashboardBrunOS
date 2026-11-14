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
| 0.13.2 | Área útil, escala única, correções da tela inicial e submenu Organização (decisão 55) |
| 0.14.0 | Históricos de 30 dias (tsdb) e gráficos com eixo, valor no mouse, Período e Ponta viva (decisão 53) |
| 0.15.0 | Janela Discos: informações, SMART explicado, testes do disco, desempenho, espaço, Linha do tempo, ejetar (decisão 56) |
| 0.15.1 | Teste do disco: andamento a cada 30 s e progressivo, resultado que não some, estimativa real, disco "só no fim"; dados genéricos |
| 0.16.0 | Montar, desmontar, rótulo e Formatar com Senha de formatação; trava de container no ejetar (decisões 54 e 57) |
| 0.17.0 | Janelas de app: apps dentro do BrunOS, Flutuante/Folha, dock com apps abertos, Só em aba automático (decisão 58) |

## Etapa 11 (0.13.1 → 0.16.0), combinada em 12/11/2026: entregue

Resumo de cada versão no `historico.md` (o plano detalhado foi apagado ao terminar a etapa); termos em
`CONTEXT.md`; porquês nas decisões 52–57. Falta o Bruno testar no servidor as versões abaixo.

| Versão | O que | Status |
|---|---|---|
| 0.13.1 | Ícones com tamanho fixo: Tamanho do ícone (4 degraus pela Régua) e Espaço entre os ícones | feito; testado, virou a 0.13.2 |
| 0.13.2 | Área útil e correções da tela inicial (widgets repetidos, fileira fora do centro, apps passando do dock) | feito, aguardando teste no servidor |
| 0.14.0 | Históricos de 30 dias (`internal/tsdb`: servidor, containers, internet, leitura/escrita dos discos, integrações) e gráficos com eixo, valor no mouse, Período e Ponta viva | feito, aguardando teste no servidor |
| 0.15.0 | Janela Discos: informações, SMART explicado, testes, desempenho, espaço, linha do tempo, ejetar | feito; testado, ajustes na 0.15.1 (teste do disco), aguardando teste no servidor |
| 0.16.0 | Montar, desmontar e Formatar com Senha de formatação | feito, aguardando teste no servidor (primeiro com um pendrive) |

## Etapa 12 (0.17.0 → 0.18.0), combinada em 13/11/2026

Plano detalhado em `plano-etapa12.md`; termos em `CONTEXT.md` (seção Janelas); porquê na decisão 58.

| Versão | O que | Status |
|---|---|---|
| 0.17.0 | Janelas de app: abrir apps dentro do BrunOS (Flutuante ou Folha, tela cheia no celular), dock com apps abertos, Só em aba automático, limite de apps abertos | feito, lançado; aguardando teste no servidor com apps reais |
| 0.18.0 | Janelas do sistema (Arquivos, Terminal, Discos...) no mesmo sistema de janelas | depois do teste da 0.17.0; detalhes a combinar |

## Em aberto

- Depois de testar a 0.13.0 no servidor: tirar a conversão das preferências antigas de widgets (ver
  `ideias-futuras.md`).
- Itens do `ideias-futuras.md` que o Bruno pedir passam para cá, com versão.

## Como lançar uma versão

Está no `CLAUDE.md` (seção "Como lançar uma versão").
