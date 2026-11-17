# Roadmap

O que está **combinado e ainda não terminou**. O que já foi entregue está resumido em `historico.md`; o
que foi adiado ou sugerido sem data está em `ideias-futuras.md`; os porquês das escolhas, em
`decisoes.md`.

Como se trabalha: uma versão por etapa, e o Bruno testa no servidor antes da próxima. Nada de privilégios
no container: o que precisa de root no host vai por SSH com chave restrita. Testar sempre também em tela de
celular.

## Testes no servidor

Em 17/11/2026 o Bruno avisou que testou no servidor **todas** as versões que estavam pendentes, da 0.12.3
à 0.18.0 (inclusive formatar e desinstalar app com a senha, as Janelas de app com apps reais e a Loja). Nada
está aguardando teste agora. Ele vai trazer um retorno com melhorias, que entra aqui com versão.

## Etapa 11 (0.13.1 → 0.16.0), combinada em 12/11/2026: entregue e testada

Resumo de cada versão no `historico.md`; termos em `CONTEXT.md`; porquês nas decisões 52–57. Todas
testadas no servidor (17/11/2026).

## Etapa 12 (0.17.0 → 0.18.0), combinada em 13/11/2026

Plano detalhado em `plano-etapa12.md`; termos em `CONTEXT.md` (seção Janelas); porquê na decisão 58.

| Versão | O que | Status |
|---|---|---|
| 0.17.0 | Janelas de app: abrir apps dentro do BrunOS (Flutuante ou Folha, tela cheia no celular), dock com apps abertos, Só em aba automático, limite de apps abertos | feito, lançado e testado no servidor |
| depois da etapa 13 | Janelas do sistema (Arquivos, Terminal, Discos...) no mesmo sistema de janelas (era a 0.18.0; a Loja passou na frente e já usa o gerenciador de Janelas) | a combinar (o teste da 0.17.0 já foi feito) |

## Etapa 13 (0.18.0 → 0.20.0), combinada em 16/11/2026: Loja

Porquês nas decisões 59 e 60; termos em `CONTEXT.md` (Loja, Loja de apps do Runtipi, Garimpo).

| Versão | O que | Status |
|---|---|---|
| 0.18.0 | Janela Loja: apps de todas as lojas do Runtipi com categorias, busca, favoritos, página do app; instalar pela tela do Runtipi; parar, iniciar, desinstalar; adicionar, remover e atualizar lojas | feito, lançado e testado no servidor (17/11) |
| 0.19.0 | **Garimpo** (decisão 60): repositório `BrunnoFe/Garimpo` com o conversor (GitHub Actions, todo dia) e um ramo por loja de origem, cada um uma loja do Runtipi: `casaos` (loja do CasaOS/ZimaOS) e `linuxserver` (LinuxServer-AppStore). Recusa receitas perigosas, pula as que já existem na loja oficial, roda os testes oficiais da loja do Runtipi antes de publicar. No BrunOS: nome da loja de origem e selo "via CasaOS" (lido do `garimpo.json`), um cartão por loja na Loja, lojas sugeridas com um clique. Junto: a Senha de formatação passa a se chamar **Senha de configuração** (script do servidor 13, comando `senha-config`, o antigo continua valendo) | feito, lançado e testado no servidor (17/11: loja CasaOS e app instalado) |
| 0.20.0 | Instalar e Ajustes dentro do BrunOS (combinado em 17/11/2026, revê a decisão 59 b): formulário de instalação na própria Loja (perguntas do app, porta, domínio, exposto), enviado pelo script do servidor à API do Runtipi com a chave do `runtipi-cli`, que não sai do servidor; editar as respostas depois; se a versão do Runtipi não for conhecida ou o pedido falhar, abre a tela do Runtipi como hoje. Aba Ajustes em cada app instalado (todos, inclusive os da loja oficial): pastas extras de `/mnt` e `/home` (só leitura ou leitura e escrita), limite de memória e de núcleos, variáveis extras e modo avançado de texto, gravados no `user-config` do Runtipi (sobrevive às atualizações); o BrunOS recusa `privileged`, socket do Docker, rede do host e pastas do sistema, pede a Senha de configuração e oferece reiniciar o app. Junto (pedido do Bruno em 17/11): linha **Senha de configuração** em Configurações → Sistema, em destaque no topo enquanto a senha não existe, com "Criar agora" que abre o Terminal com `sudo brunos-host senha-config` já digitado (criar continua só no servidor, com sudo: opção escolhida pelo Bruno); depois de criada, mostra a data e "Trocar"; aviso no sino uma vez (some sozinho quando a senha existir); script 14 diz a data da senha | feito, lançado em 17/11 (decisão 61) e testado pelo Bruno no mesmo dia (o retorno virou a etapa 14) |

Fatos do Runtipi 4.12 levantados no código-fonte (17/11/2026) para a 0.20.0: instalar é `POST
/api/app-lifecycle/<app>:<loja>/install` (formulário + porta, domínio, exposto; o estado fica no banco do
Runtipi, então gravar arquivos não basta); a API aceita `Authorization: Bearer` com um JWT assinado pelo
segredo do CLI (`/data/.secrets/cli-secret` dentro do container `runtipi`); editar respostas é `PATCH
.../update-config` (não reinicia); ajustes do usuário em `user-config/<loja>/<app>/docker-compose.yml` e
`app.env`, juntados pelo docker compose em todo início, sem nenhuma conferência do conteúdo (por isso o
BrunOS confere).

## Etapa 14 (0.21.0 → 0.22.0), combinada em 17/11/2026: janela de instalação completa

Retorno do Bruno depois de testar a 0.20.0 (prints do Runtipi: nome do endereço local, perguntas como
usuário e senha, a tela de instalação com várias opções). Porquê na decisão 62; termos em `CONTEXT.md`
(Requer privilégios, Apps com Privilégios).

| Versão | O que | Status |
|---|---|---|
| 0.21.0 | **Janela única de Instalar e editar** (Instalação + Ajustes juntos): seções que abrem e fecham (O app pergunta e Acesso abertas; Pastas e recursos e Opcionais fechadas, com resumo), "?" que abre a explicação embaixo, recomendado em cada opção, tela cheia com botões presos embaixo no celular e em janela estreita. **Acesso** como no Runtipi: nome do endereço local (`https://[nome].server.lan`), autenticação do Runtipi, app público no painel de visitantes; limite de backups ao editar (o Runtipi ignora na instalação). **Ajustes já na instalação**, preenchidos com o que a receita traz (variáveis, pastas, portas), gravando só o que mudou; o que a receita traz e não dá para apagar aparece com cadeado; versão da imagem e política de reinício. Ajustes de uma instalação anterior (o Runtipi não apaga ao desinstalar) aparecem na janela. Senha de configuração na instalação só com Ajustes ou privilégios. **Selo "Requer privilégios"** para apps de qualquer loja (régua do Garimpo), página do app com a lista, guia **Apps com Privilégios**, instalar com "Entendo os riscos" + Senha de configuração; aviso também na tela inicial e no monitor de apps. Script do servidor 15: instalar com senha e Ajustes, desfazer Ajustes de instalação que falhou. **Garimpo** junto: apps que pedem privilégios (inclusive socket do Docker) entram na loja normal; perguntas de instalação por regra (senha com valor padrão conhecido, campo marcado pelo CasaOS) + arquivo de exceções app a app | feito e lançado em 17/11; aguardando teste no servidor (instalar um app comum com Ajustes, um app com privilégios, mudar o endereço local, Configurar o Plex) |
| 0.22.0 | **Avançado**: editor de texto do arquivo de Ajustes (CodeMirror, sob demanda), conferido antes de salvar; acrescentar privilégios só por ele, com a lista do que foi pedido, "Entendo os riscos" + Senha de configuração, registro nos Eventos e o selo no app | a fazer |

Fatos do Runtipi levantados no código-fonte (17/11/2026) para a etapa 14: a instalação aceita
`localSubdomain` (`^[a-zA-Z0-9-]{1,63}$`), `enableAuth`, `isVisibleOnGuestDashboard` e `skipRun`;
`maxBackups` é aceito mas só vale pelo `update-config`. O `user-config/<loja>/<app>/docker-compose.yml` é
lido em todo `docker compose` do app (inclusive o primeiro, da instalação) enquanto `userConfigEnabled` não
for desligado no Runtipi; desinstalar apaga a pasta do app e o `app-data`, mas **não** o `user-config`.

## Em aberto

- A 0.13.0 já foi testada: dá para tirar a conversão das preferências antigas de widgets (ver
  `ideias-futuras.md`) quando o Bruno quiser.
- Senha também para parar app e para adicionar ou remover lojas: o Bruno disse que não precisa por
  enquanto (17/11/2026).
- Itens do `ideias-futuras.md` que o Bruno pedir passam para cá, com versão.

## Como lançar uma versão

Está no `CLAUDE.md` (seção "Como lançar uma versão").
