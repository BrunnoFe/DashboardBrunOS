# Histórico de versões e problemas resolvidos

Resumo do que cada versão trouxe e das lições que ainda valem. Detalhes de código: `arquitetura.md`;
porquês: `decisoes.md`; o que falta: `roadmap.md` e `ideias-futuras.md`. Detalhes de acesso ao servidor
e de segurança ficam em `docs/privado/` (não versionado).

## v0 e 0.1.0 (07/11/2026): primeira versão no Runtipi
- Protótipo estático inspirado no umbrelOS; depois o backend Go (auth, sessões, preferências, papéis de
  parede, estatísticas por SSE, apps do Runtipi, Docker por proxy, Arquivos com lixeira), CI com teste de
  fumaça em Docker real, imagem no GHCR e app na loja. O Bruno confirmou: "Funcionou no Runtipi!"
- Lições: fuga por link simbólico em upload/colar (escritas passaram a validar o caminho real); lista de
  discos vazia no runner do CI (nome do disco pelo `st_dev`); ícone sem logo vira gradiente; o ambiente da
  nuvem não cria tags, então a publicação é pelo arquivo `VERSION`.

## 0.2.0 (07/11): ajustes depois de usar
Bolinha de status inteira; "X" da janela Arquivos alinhado; seção **Discos** com "Mapear"; widget e janela
de **bateria**; "Apps no dock" em lista suspensa; ícone da loja do Runtipi; linhas de configuração que
quebram os controles para baixo.

## 0.3.0 (07/11): terminal SSH
xterm.js, SSH com senha ou chave, TOFU, SSE, `golang.org/x/crypto` vendorizado e conferido no CI. Lição: a
biblioteca preferia a chave ECDSA do servidor; passou a seguir a ordem do OpenSSH (ED25519 primeiro).
Senha errada vira recusa de login, não erro genérico.

## 0.4.0 (07/11): energia, discos e central de notificações
Sino (`internal/events`), quedas de energia (`internal/power`), **desligamento seguro** pela bateria (modo
simulação por padrão, SSH com chave restrita), aviso de disco mapeado desmontado, TOFU compartilhado
(`internal/sshknown`). Lição: um `bytes.Buffer` compartilhado entre stdout e stderr da sessão SSH perdia
saída; buffers separados.

## 0.5.0 (08/11): login novo, temperatura dos discos, previsão de disco cheio
Aviso de acesso novo por navegador+sistema+rede; temperatura por `drivetemp`/`nvme` sem acordar HD parado;
previsão de disco cheio **opcional** (desligada por padrão, a pedido do Bruno).

## 0.6.0 (08/11): arquivos
Tipos de mídia próprios (a imagem `scratch` não tem `/etc/mime.types`), editor CodeMirror 6 vendorizado,
envio com retomada, extrair zip/tar, duplicados, mapa do espaço. As etapas de segurança (dois passos,
sessões ativas, link público) foram adiadas (`ideias-futuras.md`).

## 0.7.0 (08/11): apps
Detalhes do app (disponibilidade 24 h, CPU/memória, logs ao vivo, ligar/parar/reiniciar), desinstalar pelo
painel do Runtipi, monitor de disponibilidade com aviso, pastas e páginas na tela inicial.

## 0.8.0 e 0.8.1 (08/11): monitoramento; correções depois do primeiro apagão de verdade
- 0.8.0: histórico de 24 h/7 dias, processos do servidor (como o htop), internet (latência TCP e quedas).
- 0.8.1: **o desligamento seguro falhava** num Ubuntu novo: uma trava do systemd (inhibitor) valia até para o
  root e o comando foi recusado até a bateria acabar. Corrigido com um script no servidor que ignora a
  trava, tentativas sem limite na bateria e desligamento imediato se a bateria despencar. Duração das
  quedas passou a fechar no último minuto visto. Limite de carga agnóstico ao notebook. Ajustes de celular.

## 0.9.0 a 0.9.3 (08/11): widgets personalizáveis e integrações
Janela Widgets (Modelos, Formatos, gráficos, prévia), Área de widgets por Página, colunas com encaixe,
alça de tamanho, Páginas montadas pelo Bruno, Arquivos personalizável. Integrações (credenciais cifradas,
nunca vão ao navegador): AdGuard, Speedtest Tracker, Immich, Plex, WatchYourLAN, Tailscale, Transmission,
Syncthing, Nextcloud. Papel de parede por horário. Lição: apagar uma integração durante uma busca dela
derrubava o BrunOS (nil pointer).

## 0.10.0 e 0.10.1 (10/11): bloqueio, relógio, clima, notas e Terminal com abas
Tela de bloqueio (com senha valendo no servidor, ou só visual), widgets Relógio, Clima (Open-Meteo) e
Notas, Terminal com Abas, Chave do Terminal, Conexões e Comandos salvos. Lições: o login montado atrás da
Tela de bloqueio roubava o foco; a imagem `scratch` não tinha certificados de autoridades (todo HTTPS
para fora falhava com `x509`; a imagem agora leva `ca-certificates.crt`); listas vazias vão como `[]`,
não `null`.

## 0.11.0 a 0.11.2 (10 e 11/11): consumo, SMART, atualizações; preferências protegidas
- 0.11.0: consumo e custo (Medidor, Tomada Tuya local sem nuvem), saúde dos discos (SMART pelo script do
  servidor), aviso de atualização de apps e do Linux.
- 0.11.1: **as preferências zeravam** quando a leitura de `/api/settings` falhava (a tela seguia com o
  padrão e salvava por cima). Agora a tela nunca entra nem salva sem ler, o servidor recusa gravar o
  padrão por cima (409) e guarda uma cópia por dia (14 dias). A Tomada Tuya atende uma conexão por vez:
  fila por tomada.
- 0.11.2: o SMART estourava o tempo por causa de certos adaptadores USB (o `smartctl` adivinhava NVMe e o
  kernel reiniciava o disco). Agora lê um disco por vez, tenta SATA primeiro nos USB, guarda o modo pelo
  número de série e marca "sem SMART" quando um disco trava uma vez.

## 0.12.0 a 0.12.3 (12/11): atualizar pela interface
- 0.12.0: botão de atualizações ao lado do sino; atualizar Linux e apps sem abrir o terminal (roda solto
  no servidor e o BrunOS retoma o andamento); Terminal sempre no dock; Configurações sem a aba "Tudo".
  Bug antigo corrigido: as rotinas de consumo, SMART e atualizações nunca eram ligadas.
- 0.12.1: o script do servidor passa a se atualizar sozinho, só com assinatura do CI; backup do Runtipi
  antes de atualizar app; `apt` tolerante a repositório extra quebrado.
- 0.12.2: o andamento de atualizações compridas perdia a primeira linha do registro e concluía "não
  começou" (marca de "visto rodando" + script novo). Pegadinha: um segredo salvo pelo PowerShell veio com
  CRLF e o `ssh-keygen` recusou; o CI limpa CR/BOM antes de assinar.
- 0.12.3: "5 atualizações que não somem" eram pacotes segurados pela liberação gradual do Ubuntu; agora
  contam à parte. "Conferir" por seção; lojas do Runtipi atualizadas antes de conferir os apps.

## 0.13.0 (12/11): tela inicial nova
Widgets com lugar e Tamanho livres numa grade de Quadros (Medida até 6 × 4), Disposições (widgets em
cima, apps em cima, à esquerda, à direita, só widgets, só apps), Área de apps com Medida própria, arrastar
feito pelo BrunOS (sombra, vizinhos que abrem espaço, "Desfazer"), celular com Área compacta e telas
separadas. Encaixe em `web/static/js/layout.js` (funções puras) com `tests/layout.test.mjs` no CI.
Lições dos testes com Playwright: `justify-content: center` antigo da `.app-page` centralizava as telas na
vertical; a grade de apps com `auto-fill` alargava a tela no lado a lado (`min-width: 0`); soltar fora da
tela inicial deixava o fantasma preso (ouvir os eventos de soltar na janela inteira).

## 0.13.1 (12/11): ícones com tamanho fixo (Régua)
O ícone encolhia conforme a Medida da Área de apps (a área era limitada a 8 casas e o `appsPer` diminuía o
ícone até 44 px). Agora: Tamanho do ícone em 4 degraus (Pequeno, Médio, Grande, Enorme) medidos pela
Régua (largura dos widgets com 6 colunas; no Grande, 16 colunas vão de uma ponta à outra), com piso em px
no celular (6 / 5 / 4 / 3 colunas em 360 px), e Espaço entre os ícones (Fixo, Elástico, Preencher). A
Medida que não cabe mostra menos colunas; o ícone nunca encolhe. Lição: a Régua depende do zoom do Quadro,
que só se conhece depois de desenhar os widgets; o desenho usa a do anterior e refaz na hora se mudou. A
sobra do Elástico/Preencher vai como espaço entre as colunas (cada coluna fica com a casa mínima), senão
poucas colunas largas passavam da tela.

## Pegadinhas que ainda valem
- Classes novas de CSS: conferir se o nome já existe (`.up-row` já era a lista de envios do Arquivos; o
  painel de atualizações usa o prefixo `upd-`).
- Ao mudar JS/CSS, recompilar antes de testar contra o backend (`go:embed`).
- O formulário da loja do Runtipi manda string vazia para campos novos em apps já instalados: tratar vazio
  como padrão.

## Resumo do roadmap antigo (itens feitos, por letra)
Removido do `roadmap.md` em 12/11/2026; o que não foi feito está em `ideias-futuras.md`.
- **A** apps: aviso de atualização (A1, 0.11.0), widgets com dados dos apps (A2, 0.9.x), monitor de
  disponibilidade (A4, 0.7.0). **B** energia: desligamento seguro, quedas, consumo e custo, limite de carga,
  correções (0.4.0 a 0.11.0). **C1** central de notificações (0.4.0, ampliada a cada versão).
- **D** discos: desmontado, temperatura, SMART, previsão de disco cheio, mapa do espaço (0.4.0 a 0.11.2).
- **E** apps: logs, ligar/parar, pastas, páginas, uso por app, botão de atualizações, script que se
  atualiza, backup antes de atualizar (0.7.0 a 0.12.1). **F** arquivos: vídeo, editor, envio com retomada,
  extrair e duplicados (0.6.0).
- **G** monitoramento: histórico longo, processos, internet (0.8.0). **H3** aviso de login novo (0.5.0).
- **I1/I3** WatchYourLAN, Tailscale e AdGuard (0.9.x). **J** aparência: tela de bloqueio, papel de parede
  por horário, widgets de relógio/clima/notas, Configurações sem "Tudo", Terminal no dock, tela inicial
  nova (0.9.2 a 0.13.0). **K** terminal: comandos salvos, abas, chave própria (0.10.0).
- Plano da etapa 10 (SMART um disco por vez, itens pequenos e atualizações, tela inicial nova): entregue
  nas versões 0.11.2, 0.12.x e 0.13.0; o modelo de dados dele está em `arquitetura.md` (Preferências).
