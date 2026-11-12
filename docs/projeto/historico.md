# Histórico de versões e problemas resolvidos

## v0 (protótipo, 11/2026)
- Interface estática com tema tirado do papel de parede, inspirada no código e num vídeo do umbrelOS.
- Entregue como zip e como artefato de demonstração.

## 0.1.0 (07/11/2026): primeira versão no Runtipi
- Backend Go (auth, sessões, settings, wallpapers, stats SSE, apps do Runtipi, Docker via proxy,
  gerenciador de arquivos com lixeira), interface completa, CI com teste de fumaça em Docker real,
  imagem no GHCR, app na loja (PR #1).
- O Bruno confirmou: **"Funcionou no Runtipi!"**

Problemas encontrados no caminho:
- Fuga por link simbólico em upload/colar (gravava fora da raiz) → `Root.realDir()` nas escritas.
- CI: lista de discos vazia no runner (raiz aparece como `/dev/root`) → nome do disco pelo `st_dev`.
- Logo parecia uma carinha triste → redesenhado (órbita com lua).
- Widgets claros demais e texto vazando; dock largo demais no celular (tamanho dinâmico); app sem ícone
  virava quadrado branco (agora gradiente da cor de destaque); painel de envios persistente (some
  sozinho); partições minúsculas listadas (filtro de 1 GiB).
- O ambiente da nuvem não cria tags no GitHub → publicação pelo arquivo `VERSION`.

## 0.2.0 (07/11/2026): ajustes pedidos pelo Bruno depois de usar
Pedidos (com prints):
1. Bolinha de status cortada pela metade → inteira, sem anel, sombra leve (o `overflow: hidden` dos
   ícones com logo cortava).
2. "X" de fechar da janela Arquivos desalinhado → mesma altura dos botões da barra (classe `sheet-files`).
3. Pastas de `/mnt` sem disco apareciam → seção **Discos** com "Mapear", recolhível, salva no servidor.
4. **Widget de bateria** (porcentagem, autonomia, Wh, ícone com cor por nível, tomada) + janela com
   histórico, saúde, ciclos.
5. Apps no dock → lista suspensa com caixas de seleção e busca.
6. (Terminal → feito na 0.3.0.)
7. Ícone da **loja de apps do Runtipi** no dock.
8. Descrição da cor de destaque no modo manual quebrava palavra por palavra → linhas de configuração
   quebram os controles para baixo.

## 0.3.0 (07/11/2026): terminal SSH
- Janela Terminal com xterm.js, SSH com senha ou chave, TOFU, sessões com SSE, fechamento automático.
- `golang.org/x/crypto` vendorizado e conferido no CI.
- Testes unitários com servidor SSH em processo e teste de fumaça com OpenSSH real.
- Problemas: a biblioteca preferia a chave ECDSA do servidor (impressão digital diferente da que o
  usuário costuma ver) → ordem do OpenSSH (ED25519 primeiro). Senha errada no OpenSSH real virava erro
  genérico → erro depois da chave aceita = recusa de login.
- Medido no CI: 9,7 MiB de RAM, imagem de 4 MB, 63 checagens da API.

## 0.4.0 (07/11/2026): energia, discos e central de notificações (etapa 1 do plano)
- **Central de notificações** (sino no canto da tela inicial): `internal/events`, `/data/events.json`
  (500 avisos / 30 dias), avisos repetidos não se acumulam enquanto o problema continua aberto; os
  avisos novos chegam pelo mesmo SSE das estatísticas (`event: notify`) e viram toast.
- **Quedas de energia**: `internal/power` registra tomada → bateria → tomada em
  `/data/power-events.json`, com duração e % gasto; o último consumo medido agora sobrevive a reinícios.
  Lista na janela da bateria e na janela Energia.
- **Desligamento seguro**: limite %, espera (1–10 min), faixa vermelha com contagem regressiva e
  "Cancelar", modo simulação ligado por padrão. Desliga por SSH com a chave própria do BrunOS
  (`/data/hostcmd_key`) num usuário dedicado (`brunos-power`) com `command=` forçado e sudoers de uma
  linha. Assistente em Configurações → Energia mostra os comandos e testa a conexão. Sair da
  simulação exige um teste de conexão que passou. Até 3 tentativas se o servidor não responder.
- **Disco mapeado desmontado**: verificação a cada minuto, aviso crítico e aviso de volta.
- TOFU do SSH extraído para `internal/sshknown` (terminal e desligamento usam o mesmo `known_hosts.json`).
- Testes: unitários de events/power (relógio falso)/hostcmd (servidor SSH em processo, conferindo que o
  "Testar" não abre sessão); apitest com as rotas novas; smoke com OpenSSH real e `command=` que só
  cria um arquivo (prova que o teste não executa o comando).
- Problema no caminho: `bytes.Buffer` compartilhado entre stdout e stderr da sessão SSH perdia a saída
  (as duas cópias rodam ao mesmo tempo) → buffers separados.

## 0.5.0 (08/11/2026): login novo, temperatura dos HDs e previsão de disco cheio (etapa 2)
- **Aviso de login novo** (`internal/logins`, `/data/logins.json`): navegador + sistema + rede; rede
  local conta como um lugar só, internet por faixa (/24, /48). Rede local = aviso informativo; internet
  = aviso. Também avisa quando um endereço é bloqueado por errar a senha (máx. 1 aviso a cada 6 h por
  endereço). As sessões já abertas antes da atualização são aprendidas sem avisar.
- **Temperatura dos discos**: hwmon `drivetemp` (HD/SSD SATA, precisa de `modprobe drivetemp`; a
  interface mostra os comandos) e `nvme`. Para não acordar HD parado, só mede de novo quando o disco
  teve atividade; a leitura de sensores a cada 2 s passou a ignorar o `drivetemp`. Aviso de disco
  quente (HD ≥ 55 °C, SSD ≥ 70 °C) e de volta ao normal.
- **Previsão de disco cheio** (`internal/diskhist`, `/data/disk-history.json`): um ponto por dia por
  disco, regressão sobre 30 dias, `GET /api/disks/forecast`. Avisos "deve encher em ~N dias" (≤ 30) e
  "quase cheio" (≥ 90%). Na janela Uso ao vivo: temperatura colorida e "+X/dia · cheio em ~N".
  A pedido do Bruno, a previsão é **opcional e vem desligada** (Configurações → Sistema → Previsão de
  disco cheio): desligada, não aparece nem avisa "vai encher"; o histórico continua sendo gravado e o
  aviso de "quase cheio" vale sempre.
- No celular, a lista de discos põe a barra de uso na linha de baixo (o nome cortava).

## 0.6.0 (08/11/2026): arquivos (etapa 4 do plano; a etapa 3 foi adiada)
- O Bruno pediu para adiar a etapa 3 (dois passos, sessões ativas); o link público (F3) foi junto.
- **Vídeo e áudio**: a imagem `scratch` não tem `/etc/mime.types` e a tabela do Go só conhece imagens e
  arquivos da web, então `.mp4`/`.mp3` iam como download. Agora há uma tabela própria de tipos de
  mídia (avançar/retroceder funciona pelo Range), `.mkv` liberado e um aviso quando o navegador não
  toca o codec.
- **Editor de texto** (CodeMirror 6, escolha do Bruno): "Editar" no menu e na pré-visualização,
  Ctrl+S, procurar/substituir, cores por linguagem, aviso ao sair sem salvar. Salva de forma atômica
  e recusa se o arquivo mudou desde que abriu. "Novo arquivo de texto".
- **Envio com retomada**: arquivos acima de 16 MB vão em pedaços de 8 MB; queda de conexão ou página
  recarregada continuam de onde pararam.
- **Extrair** `.zip`, `.tar`, `.tar.gz` (seguro contra zip-slip e zip-bomba) e **encontrar
  duplicados** (com seleção pronta para mandar à lixeira).
- **Mapa do espaço**: treemap das pastas, com cache de 10 min para navegar nas subpastas na hora.
- `openDialog` ganhou `beforeClose` (o editor pergunta antes de fechar com alterações).

## 0.7.0 (08/11/2026): apps (etapa 5 do plano)
- **Detalhes do app** (botão direito no ícone, ou clique na lista do Uso ao vivo): estado,
  disponibilidade das últimas 24 h (blocos de 15 min), gráfico de CPU/memória de 24 h, logs de cada
  container (com "Ao vivo" por SSE, sem códigos de cor), ligar/parar/reiniciar.
- **Desinstalar**: o Bruno pediu; escolheu o botão que abre a página do app no painel do Runtipi
  (`/apps/<loja>/<app>`, conferido no código do Runtipi) em vez de dar ao BrunOS o runtipi-cli por SSH.
- **Ligar/parar** pelo Docker (com `BRUNOS_DOCKER_ACTIONS`); o Runtipi não fica sabendo, e a interface
  avisa. Proxy do Docker passou a liberar `GET .../logs` e `POST .../start|stop`.
- **Monitor de disponibilidade** (`internal/appmon` + `apps.go`): a cada minuto, mede CPU/memória de
  cada app e testa a porta (qualquer resposta HTTP = no ar); 2 minutos seguidos fora do ar → aviso no
  sino, e outro quando volta. Bolinha vermelha no ícone. Liga/desliga em Configurações → Apps, e por
  app em Detalhes. Histórico só na memória (24 h).
- **Pastas** (arrastar um app sobre o meio de outro) e **páginas** na tela inicial (quando os apps
  não cabem: deslizar, setas, roda do mouse ou pontinhos).

## 0.8.0 (08/11/2026): monitoramento (etapa 6 do plano)
- **Histórico longo** (`internal/history`, `/data/history.json`): média por minuto (24 h) e por hora
  (7 dias) de CPU, memória, temperatura e rede, ponderada pelo tempo de cada amostra do coletor; grava a
  cada 10 min e ao desligar. No Uso ao vivo, seletor 5 min / 24 h / 7 dias.
- **Processos do servidor** (`internal/procs`): como o htop, lendo o `/proc` do host; CPU por diferença
  entre duas leituras, memória, usuário (com `/etc/passwd` montado só leitura, escolha do Bruno) e o
  app de cada container (pelo `cgroup`). Aba "Processos" no Uso ao vivo, com filtro. Sem matar processos.
- **Internet** (`internal/netmon`): a cada minuto mede o tempo de conexão TCP com Cloudflare, Google
  e Quad9; 2 minutos sem nenhum → aviso "a internet caiu", e outro quando volta (com a duração). Quedas
  em `/data/internet.json`. Widget Internet e janela com o gráfico de 24 h.
- Speedtest Tracker ficou para a 0.9.0: a API dele exige token, e o Bruno preferiu decidir como guardar
  tokens junto com as outras integrações.

## 0.8.1 (08/11/2026): correções depois do primeiro apagão de verdade
- **O desligamento seguro nunca funcionou no Ubuntu 26.04.** Na queda de 07/11 o `sudo shutdown -h +1`
  foi recusado ("Operation denied due to active block inhibitor"): no systemd 259 a trava vale até
  para o root, e o unattended-upgrades segura uma de `shutdown` o tempo todo. O BrunOS desistiu depois
  de 3 tentativas e a bateria acabou. Correções: script `brunos-host` no servidor (comando forçado da
  chave nova) que desliga com `systemctl poweroff --check-inhibitors=no --when=+1min` (testado no
  servidor do Bruno); tentativas sem limite enquanto estiver na bateria; erro em português; se a
  bateria despencar (≤ metade do limite ou ≤ 5%), desliga sem esperar a contagem.
- **Bateria que pula**: o histórico do UPower mostrou 31% → 6% em 5 minutos (bateria paralela de 9
  meses, mal calibrada). Daí o atalho acima.
- **Duração das quedas**: na bateria grava a cada minuto (`seen`); se o servidor apagar, a queda fecha
  nesse horário e aparece "apagou". Antes contava o tempo desligado (o "10 h 03" era ~5 h 40).
- **Limite de carga agnóstico** (`internal/power/charge.go`): detecta uma vez ao ligar se o notebook
  tem limite contínuo (`charge_control_end_threshold`), só liga/desliga (`charge_types` Long_Life ou
  `conservation_mode` do IdeaPad) ou nada (esmaecido com o modelo; caso do Inspiron 3542). Aplica pelo
  script; reconfere um arquivo a cada 10 min e reaplica se o notebook esquecer. Sem suporte, não faz nada.
- **Duas chaves SSH**: a configuração nova usa `/data/hostcmd_key_v2`; servidor não atualizado recusa a
  chave nova já na autenticação (nunca roda o comando antigo por engano), e o teste de conexão prova a
  atualização. A chave antiga (`hostcmd_key`) só desliga, até o Bruno colar os comandos novos.
- **Celular**: gaveta ☰ no Arquivos; nas configurações, interruptores/botões/setas à direita e só os
  controles largos embaixo; terminal em tela cheia com letra 11 e "Desconectar" só com ícone; porta do
  terminal vazia (usa 22); sem o texto `hsl(...)` na cor de destaque.

## 0.9.0 (08/11/2026): widgets personalizáveis e integrações (etapa 7a)
- **Janela Widgets** (dock): Meus widgets, Modelos e Integrações, com editor e prévia ao vivo. Formatos
  Destaque, Grade, Lista, Gráfico (linha, barras, rosca) e Atalhos; Tamanhos Pequeno, Largo, Alto, Grande
  e Faixa; por métrica: rótulo, unidade, casas, compacto, ícone, mini-gráfico e cor por faixa; por
  widget: clique e aparência própria (padrão: segue o tema). Termos em `CONTEXT.md`, decisões 31–34.
- **Área de widgets em cada página**; menu rápido no botão direito (ou segurar) e modo "Organizar
  widgets" (arrastar, remover, adicionar). Os 6 widgets antigos viraram Modelos e são convertidos sozinhos.
- **Integrações** AdGuard Home e Speedtest Tracker (`internal/integrations`), sugeridas a partir do
  Runtipi; senha/token cifrado (AES-GCM, `/data/secret.key`) e nunca enviado ao navegador; Avisos prontos
  (proteção desligada, download baixo, ping alto, sem responder).
- **Leveza**: com a tela fechada não busca nada; aberta, só o que algum widget usa, no ritmo de cada app;
  segundo plano (5 min) só para aviso ligado ou gráfico sem histórico no app.

## 0.9.1 (08/11/2026): melhorias dos widgets e páginas montadas (etapa 7a+)
- **Colunas por página** com encaixe (decisão 35): um Grande com Pequenos empilhados ao lado; controle
  "− N colunas +" no modo Organizar e na janela Widgets; no celular nunca passa do que cabe.
- **Arrastar widgets sempre** (como os ícones), inclusive para outra página; **alça de tamanho** no modo
  Organizar, pulando entre os 5 Tamanhos.
- **Páginas montadas pelo Bruno**: apps em qualquer página (arrastando ou "Mover para a página…"),
  página só de widgets, só de apps ou mista; páginas de continuação quando os apps não cabem; o modo
  Organizar mostra uma página nova vazia para soltar coisas.
- **Arquivos personalizável**: pastas escolhidas (navegador de pastas), grade ou lista, ícone e cor por
  pasta, espaço livre do disco.
- Correções: Destaque/Armazenamento com 3 a 5 informações (fileira sem buraco, nada passa da altura);
  "Aparência própria" da 0.9.0 não aplicava a transparência/cor (faltava um espaço na classe).

## 0.9.2 (08/11/2026): Plex, Immich, WatchYourLAN, Tailscale e papel de parede por horário (etapa 7b)
- **Formato Mídia**: fotos do Immich (neste dia, aleatórias, favoritas ou de um álbum) trocando a cada
  5 min a 1 dia, com legenda de data e lugar; capa do que está tocando no Plex, com quem assiste e o
  progresso. Imagens passam pelo BrunOS (rota `/api/integrations/{id}/image`, cache em `/data/img-cache`).
- **Integrações novas**: Immich (totais e fotos), Plex (sessões, conversões, bibliotecas), WatchYourLAN
  (online, desconhecidos, listas e **aviso de aparelho novo**), Tailscale (situação, aparelhos, IP) pela
  conexão SSH do desligamento seguro, com o script `brunos-host` 3 (decisão 36).
- **Papel de parede por horário** (Configurações → Papel de parede → Por horário), lista livre; troca
  sozinho a cada minuto conferido e a tela de login acompanha.
- Listas com nome e detalhe em duas linhas; widgets sempre do tamanho exato da grade.
- Correção: apagar uma integração durante uma busca dela podia derrubar o BrunOS (nil pointer).

## 0.9.3 (08/11/2026): Transmission, Syncthing e Nextcloud (etapa 7c, fim da etapa 7)
- **Transmission**: download/upload agora, baixando, semeando, pausados, com erro, totais e a lista do
  que está baixando (progresso, velocidade, quanto falta); aviso de torrent com erro.
- **Syncthing**: pastas em dia, quanto falta, arquivos com erro, aparelhos conectados e listas de pastas
  e aparelhos; aviso de erro de sincronização.
- **Nextcloud** (app serverinfo, token só de leitura): usuários, ativos, arquivos, compartilhamentos,
  espaço livre e banco de dados; aviso de pouco espaço.
- Vaultwarden adiado a pedido do Bruno (decisão 38).

## 0.10.0 (10/11/2026): Tela de bloqueio, relógio, clima, notas e Terminal com abas (etapa 8)
Combinado numa sessão de perguntas (termos novos em `CONTEXT.md`, decisões 39–41):
- **Tela de bloqueio**: relógio grande, data e saudação; com senha (padrão, o servidor recusa tudo com
  423 até a senha; vale por Sessão; errar 5 vezes encerra a Sessão) ou só visual (mostra clima e avisos).
  Ctrl+Shift+L, busca, botão direito e inatividade (padrão 15 min).
- **Clima** (Open-Meteo, sem chave): cidade por nome, Métricas, ícones próprios coloridos (o ícone vem
  com o valor: `Value.Icon`), Modelos "agora", "próximos dias", "semana na faixa" e "detalhes".
- **Relógio** (Formato): digital ou ponteiros, segundos, data, fuso; **Notas** (Formato): texto livre ou
  lista de tarefas, escritas na tela inicial, sincronizadas entre aparelhos pelo SSE.
- **Terminal**: até 4 Abas, minimizar (ícone na dock com pontinho), fim da tela guardado para reabrir,
  Comandos salvos, Conexões salvas, Chave do Terminal (cifrada pela senha do BrunOS), "ainda está
  rodando?" antes de fechar e no fim do tempo sem uso (configurável), aviso no sino quando um comando
  termina numa Aba minimizada.
- Testes: unitários novos (auth, termkey, notes, weather, terminal com chave/idle/ps), apitest 174 ok,
  interface contra o servidor real (bloqueio que sobrevive ao recarregar, nota em dois navegadores).

Problemas encontrados no caminho:
- O login montado por trás da Tela de bloqueio (ao recarregar bloqueado) roubava o foco do campo da
  senha → não monta mais nesse caso e fica escondido.
- Aviso "nota mudou" chegando durante a primeira busca era descartado → busca de novo ao terminar; ao
  (re)conectar o SSE as notas abertas são relidas.
- "Ainda está rodando?" conferido no OpenSSH real do teste de fumaça (CI): vê o `sleep` rodando e,
  depois do Ctrl+C, nada. Sem o `ps` completo (ex.: só busybox), a resposta é "não sei" e a janela
  pergunta antes de fechar.

## 0.10.1 (10/11/2026): correções do Clima e do Tailscale (prints do Bruno)
- **Clima não achava a cidade** ("não consegui procurar a cidade (o servidor está sem internet?)"): a
  imagem `scratch` não tinha os certificados das autoridades, então todo HTTPS para fora falhava com
  `x509: certificate signed by unknown authority` (reproduzido no WSL sem certificados). A imagem agora
  leva `/etc/ssl/certs/ca-certificates.crt` (~220 KB); o teste de fumaça procura uma cidade de dentro
  do container. O motivo real de falhas de conexão das integrações passou a ir para o log.
- **"Buscar a cada" mostrava 10 segundos** no Clima: faltava a opção de 15 minutos na lista.
- **Tailscale: clicar não fazia nada** (desde a 0.9.2): o Tipo sem campos ia como `"fields": null` e a
  tela quebrava ao percorrer a lista. Listas vazias agora vão como `[]` (teste Go e da API) e a tela
  tolera lista ausente.

## 0.11.0 (10/11/2026): consumo e custo, saúde dos discos e atualizações (etapa 9)
Combinado numa sessão de perguntas (Medidor e Estimativa calibrada em `CONTEXT.md`, decisões 42–44).
H4 (convidados) e I2 (Wake-on-LAN) foram para o futuro; B4 já existia.
- **Consumo e custo**: Medidor (Automático: tomada → bateria nas quedas → estimativa calibrada), amostra
  por minuto, kWh e R$ de hoje/mês/projeção com a tarifa, 24 h por minuto e 13 meses por dia; seção na
  janela Energia, Métricas da fonte BrunOS e Modelos prontos.
- **Tomada Tuya** (protocolo local 3.3/3.4/3.5, sem nuvem, só biblioteca padrão): a do Bruno é 3.5 e
  mede ~17 W. O cliente Go foi validado contra uma tomada falsa feita com as funções do próprio
  tinytuya 1.20 (negociação da chave de sessão, AES-ECB/HMAC e AES-GCM).
- **Saúde dos discos (SMART)** pelo script do servidor: selo OK/Atenção/Falhando, detalhes, avisos;
  temperatura dos discos em gavetas USB (o drivetemp não atravessa o adaptador) a cada 20 min.
- **Atualizações**: apps (cópia instalada × loja baixada, sem Postgres), selo ↑ no ícone e "Atualizar
  no Runtipi"; Linux (apt-check, reinício necessário) com "Atualizar no Terminal".
- Script do servidor **versão 4** (energia, smart, temperatura, atualizacoes; só leitura).
- Testes: pacotes energy/smart/updates/runtipi e Tuya; apitest 189 ok.

## 0.11.1 (11/11/2026): preferências nunca mais zeram; Tomada Tuya em fila
Bruno abriu o painel de manhã e achou tudo no padrão (widgets, segunda página, bloqueio); as
integrações sobreviveram (outro arquivo).
- **Causa**: ao entrar, se a leitura de `/api/settings` falhasse por qualquer motivo (rede acordando,
  proxy, arquivo ilegível — o servidor também devolvia `{}` em erro), a tela seguia com o padrão;
  `W2()` montava os 3 widgets padrão e `welcomed: false` disparava um salvamento, gravando o padrão
  por cima de `/data/settings.json`. Bug antigo, latente desde as primeiras versões.
- **Correção** (decisão 45): a tela tenta de novo e nunca entra com o padrão nem salva antes de ler;
  o servidor responde 500 em arquivo ilegível, recusa (409) gravar preferências sem `welcomed` por cima
  de preferências com `welcomed`, e guarda uma cópia por dia (14 dias) em `/data/settings-backup`,
  restaurável em Configurações → Sistema → Cópias das preferências.
- **Tomada Tuya**: o Testar funcionava, mas o Medidor (e o widget de consumo) dava "não consegui falar
  com a tomada". A tomada atende uma conexão por vez; agora há fila por tomada, a última leitura vale
  15 s (50 s para o Medidor), mais uma tentativa após 2 s, mensagem própria para "recusou a conexão"
  e o erro de verdade no log.

## 0.11.2 (11/11/2026): SMART um disco por vez (etapa 10a)
O SMART dava "context deadline exceeded". Diagnóstico com o Bruno no servidor: o case JMicron JMS583 do
sdb (chip de NVMe com HD SATA dentro) fazia o `smartctl` adivinhar NVMe; o adaptador não respondia e o
kernel reiniciava o USB do disco montado (`uas_eh_device_reset_handler` no `dmesg`, também nas leituras
automáticas a cada 20 min). Com `-d sat` o mesmo disco respondeu em 2,5 s.
- **Script do servidor versão 5**: `discos` (lsblk) e `smart|temperatura <disco> <sat|auto>`, um disco por
  pedido, só `-H -i -A`.
- **Um disco por vez** com limite próprio; USB tenta SATA e depois o automático (NVMe em case); o modo fica
  guardado pelo número de série. Travou ou veio sem dados uma vez → **"sem SMART"** (etiqueta vermelha,
  aviso descritivo no sino, nunca mais lido sozinho) e "Tentar de novo" testa o outro modo (decisão 46).
- "Ler agora" em segundo plano ("Lendo…" até terminar); mensagens em português no lugar do erro cru.
- Celular: o selo do SMART quebra a linha em vez de ser cortado na lista de discos.
- Testes: `internal/smart` refeito (casos do servidor do Bruno com um script falso que trava), conferência
  do script com pedidos novos; apitest 191 ok. O smoke.sh fica para o CI (Docker Desktop desligado aqui).
- No servidor (Bruno, 11/11): os quatro discos leem; o sdb (JMicron) passou a funcionar com `-d sat`;
  o sda avisou 1 setor realocado (Atenção).

## 0.12.0 (12/11/2026): botão de atualizações, Terminal no dock, Configurações sem "Tudo" (etapa 10b)
- **Botão de atualizações ao lado do sino**: bolinha com apps + Linux (laranja com atualização de
  segurança); um clique abre o painel e já confere (se a última tiver mais de 10 min); "Conferir agora".
- **Atualizar pela interface** (decisão 47), script **versão 6**: Linux (`apt-get upgrade` sem perguntas)
  e cada app (`runtipi-cli app update`), com confirmação, rodinha, últimas linhas do andamento e aviso no
  sino no fim; roda solto no servidor (`systemd-run`) e o BrunOS retoma o acompanhamento se reiniciar.
- **Frequência** em Configurações → Sistema → Procurar atualizações (6 h, 12 h, diária, semanal, manual).
- **Terminal sempre no dock** (pontinho só com Abas abertas); dá para esconder em Apps no dock.
- **Configurações sem "Tudo"**: abre na última aba usada; a pesquisa procura em todas as abas.
- **Bug antigo corrigido**: as rotinas do consumo (amostra por minuto), do SMART (6 h / 20 min) e das
  atualizações nunca eram ligadas desde a 0.11.0 (só rodavam ao clicar). Agora rodam sozinhas.
- Pegadinha: `.up-row` já existia (lista de envios do Arquivos, `height: 32px`); as classes do painel
  novo usam o prefixo `upd-`.
- Testes: `internal/updates` (iniciar, acompanhar, erro, duas ao mesmo tempo, registro antigo),
  conferência do script 6 (inclui "executar" recusado fora do systemd), linha do `/etc/brunos-host.conf`
  com caminhos estranhos recusados; apitest 196 ok.
- No servidor (12/11): o "Atualizar" não fazia nada porque o script ainda era o 5 (faltava colar os
  comandos); com o 6, o Linux parou no `apt update` por um repositório quebrado (Ookla speedtest-cli,
  404) e terminou com "código 100".

## 0.12.1 (12/11/2026): script que se atualiza sozinho, backup antes de atualizar, apt mais tolerante
- **Script do servidor versão 7** que se troca sozinho por um assinado (decisão 49): o BrunOS confere a
  versão ao ligar e no "Conferir"; com o 7+ instalado e a assinatura na imagem, atualiza sem comandos.
  Do 6 para o 7 ainda é manual (uma vez), agora com "Pelo Terminal" no painel.
- **Backup do Runtipi antes de atualizar app** (decisão 50), caixinha sempre marcada, tamanho medido na
  confirmação, andamento em duas etapas ("1. Backup → 2. Atualizar"); backup que falha não atualiza.
- **apt**: um repositório extra quebrado não impede mais de instalar (vira aviso com o nome dele);
  mensagens em português para trava do dpkg, disco cheio, sem internet e instalação pela metade.
- **Painel**: erro ao começar fica no painel (não só numa mensagem que some); faixa "Script do servidor
  desatualizado" com "Atualizar agora"/"Pelo Terminal"/"Ver comandos"; botões desligados quando o script
  não sabe fazer aquilo.
- **Acesso de diagnóstico só de leitura** (decisão 51): `tools/diagnostico/brunos-diag`.
- Testes: `tests/script-selfupdate.sh` (como root num contêiner, no CI), `internal/updates` (backup,
  aviso do repositório, mensagens, versão do script), conferência do script 7; apitest 199 ok. A
  assinatura com a chave real foi conferida localmente com os mesmos passos do CI.
- No servidor (12/11): script 7 colado pelo Terminal; segredo `BRUNOS_SCRIPT_KEY` cadastrado pelo Bruno;
  acesso de diagnóstico ligado e conferido. Atualizar o Linux instalou 5 pacotes (`fim 0`), mas o painel
  disse "a atualização não começou no servidor".

## 0.12.2 (12/11/2026): andamento de atualizações compridas; primeira versão assinada
- **Causa**: o pedido de andamento mandava só as últimas 30 linhas do registro; num registro comprido a
  linha `inicio <hora>` (que diz de qual atualização ele é) ficava de fora, o BrunOS achava o registro
  antigo e, ao terminar, concluía "não começou".
- **Correção**: a atualização vista rodando fica marcada (`seen`, gravado) e o registro passa a ser dela
  até o fim; e o **script 8** manda sempre a primeira linha antes do fim do registro.
- **Primeira versão assinada pelo CI**: o script do servidor deve passar do 7 para o 8 sozinho ao ligar
  o BrunOS 0.12.2 (primeiro teste real da decisão 49).
- Pegadinha: o segredo cadastrado pelo PowerShell (`wsl cat | gh secret set`) veio com CRLF e o
  `ssh-keygen` recusou ("Couldn't load public key"); o CI agora tira CR/BOM antes de assinar.
- No servidor (12/11): **o script passou sozinho do 7 para o 8** (decisão 49 funcionando de verdade).

## 0.12.3 (12/11/2026): liberação gradual do Ubuntu, "Conferir" por seção, lojas do Runtipi
- **"5 atualizações" que não somem**: eram os pacotes segurados pela liberação gradual (phasing) do
  Ubuntu (`deferred due to phasing`). O `apt-check` já os descontava (0), mas o BrunOS trocava o 0 pela
  lista do `apt list` (5). Agora conta só o que dá para instalar e mostra "N aguardando a liberação
  gradual do Ubuntu" à parte. Conferido no servidor pelo acesso de diagnóstico.
- **"Conferir" em cada seção** do painel (Apps e Sistema), além do "Conferir tudo" e da rotina.
- **Lojas do Runtipi atualizadas antes de conferir os apps** (script **9**, pedido `atualizar-lojas` =
  `runtipi-cli appstore update`); falha na loja vira aviso no painel e os apps são conferidos mesmo assim.

## 0.13.0 (12/11/2026): tela inicial nova (decisão 48)
- **Widgets com lugar e Tamanho livres**: cada Página tem uma grade de Quadros (a Medida, até 6 × 4) e
  cada widget fica num lugar fixo dela, com qualquer Tamanho até a Medida (ex.: um 2 × 1 no meio da
  última linha). Preferências `widgets2.v = 3` (`areas[página] = [{id, x, y}]`, `items[id].w/h`) e
  `pageCfg[página] = {disp, wm, am}`; as antigas (v1/v2) são convertidas uma vez, na ordem de antes.
- **Disposições**: widgets em cima (padrão), apps em cima, widgets à esquerda, à direita, só widgets e
  só apps. **Área de apps** com Medida própria (até 16 × 6) ou Automática.
- **Arrastar novo** (feito pelo BrunOS, sem o arrastar do navegador): o widget segue o ponteiro, uma
  sombra mostra onde cai (vermelha quando não dá) e os vizinhos abrem espaço (troca de lugar ou o livre
  mais perto). Alça do canto muda o Tamanho de Quadro em Quadro. Ícones de apps escorregam em fila;
  soltar no meio de outro cria pasta. Beira da tela muda de Página. "Desfazer" depois de cada mudança.
- Seletores de quadradinhos (Medida e Tamanho) e das 6 Disposições: na barra do modo Organizar, no menu
  do widget, no editor da janela Widgets e em Configurações → Aparência → **Páginas da tela inicial**.
- **Celular**: a Área fica compacta (2 colunas, ordem de leitura, sem mudar o que está guardado) e, por
  padrão, os widgets de uma Página ficam numa tela e os apps na seguinte (chave "No celular, widgets e
  apps em telas separadas"). Segurar o dedo entra no modo Organizar.
- A Área escala inteira (zoom) entre Quadros de 150 e 260 px; se a Área for alta demais para dividir a
  tela com os apps, os apps passam para a tela seguinte.
- Encaixe em `web/static/js/layout.js` (funções puras) com teste `tests/layout.test.mjs` (Node), que
  passou a rodar no CI.
- Bugs achados nos testes com Playwright: a regra antiga `justify-content: center` da `.app-page`
  centralizava as telas na vertical e a conta de apps por tela saía menor; a grade de apps com
  `auto-fill` alargava a tela no lado a lado (`min-width: 0`); soltar fora da tela inicial deixava o
  fantasma preso (agora os eventos de soltar são ouvidos na janela inteira).

## Próximas
Veja `roadmap.md`.
