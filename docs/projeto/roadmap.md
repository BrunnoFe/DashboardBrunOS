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
| 0.18.0 | Janela Loja: catálogo das lojas do Runtipi, instalar pela tela do Runtipi, parar/iniciar/desinstalar (com a Senha de formatação), gerenciar lojas (decisão 59) |

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
| depois da etapa 13 | Janelas do sistema (Arquivos, Terminal, Discos...) no mesmo sistema de janelas (era a 0.18.0; a Loja passou na frente e já usa o gerenciador de Janelas) | depois do teste da 0.17.0; detalhes a combinar |

## Etapa 13 (0.18.0 → 0.19.0), combinada em 16/11/2026: Loja

Porquê na decisão 59; termos em `CONTEXT.md` (Loja, Loja de apps do Runtipi).

| Versão | O que | Status |
|---|---|---|
| 0.18.0 | Janela Loja: apps de todas as lojas do Runtipi com categorias, busca, favoritos, página do app; instalar pela tela do Runtipi; parar, iniciar, desinstalar; adicionar, remover e atualizar lojas | feito, lançado em 17/11; aguardando teste no servidor (antes de desinstalar, criar a Senha de formatação) |
| 0.19.0 | Conversor de lojas: ferramenta no GitHub Actions que lê a loja do CasaOS/ZimaOS, converte para o formato do Runtipi e publica numa loja separada chamada **Garimpo** (repositório no GitHub do Bruno), com selo de origem nos apps ("via CasaOS") e recusa de receitas perigosas (`privileged`, socket do Docker, rede do host). Umbrel depois, só os apps simples e depois de conferir a licença | a fazer (nome e selo escolhidos em 17/11) |

Para conferir no primeiro teste da 0.18.0 no servidor: (1) o endereço da tela de instalar do Runtipi
(`<painel>/app-store/<loja>/<app>`); (2) se o painel do Runtipi aceita abrir em Janela de app; (3) o que o
`runtipi-cli` responde ao desinstalar e ao remover uma loja com apps instalados.

## Em aberto

- Depois de testar a 0.13.0 no servidor: tirar a conversão das preferências antigas de widgets (ver
  `ideias-futuras.md`).
- Itens do `ideias-futuras.md` que o Bruno pedir passam para cá, com versão.

## Como lançar uma versão

Está no `CLAUDE.md` (seção "Como lançar uma versão").
