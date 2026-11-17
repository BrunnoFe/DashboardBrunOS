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
| 0.20.0 | Instalar e Ajustes dentro do BrunOS (combinado em 17/11/2026, revê a decisão 59 b): formulário de instalação na própria Loja (perguntas do app, porta, domínio, exposto), enviado pelo script do servidor à API do Runtipi com a chave do `runtipi-cli`, que não sai do servidor; editar as respostas depois; se a versão do Runtipi não for conhecida ou o pedido falhar, abre a tela do Runtipi como hoje. Aba Ajustes em cada app instalado (todos, inclusive os da loja oficial): pastas extras de `/mnt` e `/home` (só leitura ou leitura e escrita), limite de memória e de núcleos, variáveis extras e modo avançado de texto, gravados no `user-config` do Runtipi (sobrevive às atualizações); o BrunOS recusa `privileged`, socket do Docker, rede do host e pastas do sistema, pede a Senha de configuração e oferece reiniciar o app. Junto (pedido do Bruno em 17/11): linha **Senha de configuração** em Configurações → Sistema, em destaque no topo enquanto a senha não existe, com "Criar agora" que abre o Terminal com `sudo brunos-host senha-config` já digitado (criar continua só no servidor, com sudo: opção escolhida pelo Bruno); depois de criada, mostra a data e "Trocar"; aviso no sino uma vez (some sozinho quando a senha existir); script 14 diz a data da senha | feito, lançado em 17/11 (decisão 61); aguardando teste no servidor (instalar um app pequeno pela Loja; Ajustes do Plex com as pastas feitas à mão) |

Fatos do Runtipi 4.12 levantados no código-fonte (17/11/2026) para a 0.20.0: instalar é `POST
/api/app-lifecycle/<app>:<loja>/install` (formulário + porta, domínio, exposto; o estado fica no banco do
Runtipi, então gravar arquivos não basta); a API aceita `Authorization: Bearer` com um JWT assinado pelo
segredo do CLI (`/data/.secrets/cli-secret` dentro do container `runtipi`); editar respostas é `PATCH
.../update-config` (não reinicia); ajustes do usuário em `user-config/<loja>/<app>/docker-compose.yml` e
`app.env`, juntados pelo docker compose em todo início, sem nenhuma conferência do conteúdo (por isso o
BrunOS confere).

## Em aberto

- A 0.13.0 já foi testada: dá para tirar a conversão das preferências antigas de widgets (ver
  `ideias-futuras.md`) quando o Bruno quiser.
- Senha também para parar app e para adicionar ou remover lojas: o Bruno disse que não precisa por
  enquanto (17/11/2026).
- Itens do `ideias-futuras.md` que o Bruno pedir passam para cá, com versão.

## Como lançar uma versão

Está no `CLAUDE.md` (seção "Como lançar uma versão").
