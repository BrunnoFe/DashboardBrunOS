# Roadmap aprovado

Em 07/11/2026 o Bruno aprovou **todas** as ideias abaixo ("Gostei de todas as suas sugestões e gostaria
de implementar todas"), **exceto o tema claro** ("não vejo necessidade"). No mesmo dia o plano de
implementação foi montado com ele (seção **Plano de implementação** no fim deste arquivo). Também
ficou combinado **adiar tudo que envolve o celular** (C2, A3, F6): "Vamos deixar para outro momento no
futuro qualquer implementação que envolva o celular."

Legenda: esforço B/M/A (baixo, médio, alto) · status `a fazer` / `em andamento` / `feito (versão)` / `adiado`.
Regra geral: nada de privilégios no container. O que precisar de root no host vai **por SSH com uma
chave restrita** (`command="..."` no `authorized_keys`, ver decisão 11 em `decisoes.md`).

## A. Sugestões originais (da 0.1.0)

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| A1 | Aviso de atualização de apps do Runtipi | M | **feito (0.11.0)** (apps × repos pelo tipi_version; + Linux) | O Runtipi guarda a versão instalada no Postgres, não em arquivos. Opções: comparar `tipi_version` do repositório da loja com algo legível em `app-data` (verificar), ou API interna do Runtipi. Não abrir acesso ao banco. |
| A2 | Widgets com dados dos apps (Immich: fotos/vídeos; AdGuard: consultas/bloqueios; Plex: tocando agora) | M | a fazer | Cada integração precisa de URL + token do app, guardados no servidor (criptografados? decidir) e **nunca** enviados ao navegador; o backend faz as chamadas. |
| A3 | PWA (instalar no celular) | B | adiado (celular) | `manifest.webmanifest`, ícones, service worker mínimo (só cache da casca; API nunca em cache). Base para F6 e C2. |
| A4 | Monitor de disponibilidade dos apps com alerta | M | feito (0.7.0) | Checagem HTTP periódica (porta/domínio de cada app) + estado do container; histórico de uptime; alertas pela central (C1). |

## B. Energia e bateria (o notebook é o servidor; a bateria é o nobreak)

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| B1 | **Desligamento seguro** quando a bateria cai abaixo de X% fora da tomada | M | feito (0.4.0) | Prioridade 1. Config: limite %, tempo de espera, aviso antes. Execução via SSH com chave restrita a `sudo /sbin/shutdown -h +1` (ou `systemctl poweroff`). Precisa de assistente de configuração (gera a chave, mostra a linha do `authorized_keys` e do sudoers). Testar sem desligar de verdade (modo simulação). |
| B2 | Registro de quedas de energia | B | feito (0.4.0) | Detectar transições AC→bateria→AC; gravar em `/data/power-events.json` (início, fim, % gasto). Persistir também o último consumo medido (hoje `estimate` some ao reiniciar). |
| B3 | Consumo do servidor (W) e custo (R$/mês) | B–M | **feito (0.11.0)** (Medidor + Tomada Tuya local) | Intel RAPL (`/sys/class/powercap/intel-rapl:0/energy_uj`, pode exigir permissão de leitura) + consumo da bateria quando descarregando; tarifa em R$/kWh configurável. |
| B4 | Limite de carga (agnóstico ao notebook) | M | feito (0.8.1) | Detecta uma vez ao ligar (contínuo, liga/desliga ou nada); aplica pelo script `brunos-host`; reconfere a cada 10 min só se houver suporte e pedido. O Inspiron 3542 do Bruno não tem: aparece esmaecido com o modelo. |
| B5 | Correções do desligamento seguro | B | feito (0.8.1) | Comando novo que passa pela trava do unattended-upgrades; tenta sem limite enquanto estiver na bateria; erro em pt-BR; queda fecha no último minuto ligado quando o servidor apaga; aviso para atualizar servidores configurados antes. |
| B6 | Ajustes visuais no celular | B | feito (0.8.1) | Gaveta ☰ no Arquivos; controles à direita nas configurações; terminal em tela cheia com letra 11; porta vazia (22); sem o texto HSL na cor de destaque. |

## C. Notificações

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| C1 | Central de notificações (sino) | M | feito (0.4.0): energia e discos; 0.5.0: login novo, disco quente, disco cheio; os outros eventos entram nas etapas deles | Eventos: app caiu, disco > 90%, disco desmontado, energia caiu/voltou, temperatura alta, atualização disponível, login novo. `/data/events.json` com rotação; marcar como lido. |
| C2 | Avisos no celular (ntfy, Telegram, push do PWA) | M | adiado (celular) | ntfy tem app no Runtipi; Telegram via bot token; Web Push exige VAPID (implementar com stdlib: ECDSA P-256 + JWT, viável). Regras de quais eventos vão para onde. |

## D. Discos

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| D1 | Alerta de disco mapeado que se desmontou | B | feito (0.4.0) | Disco mapeado com `isMountPoint == false` → evento crítico (risco de o mergerfs/apps gravarem no SSD do sistema). |
| D2 | Temperatura dos HDs | B | feito (0.5.0) | hwmon `drivetemp` (módulo do kernel: `sudo modprobe drivetemp` + `/etc/modules-load.d`; a interface ensina) e `nvme`. HD só é lido de novo quando teve atividade (não acorda disco parado). Aviso na central: HD ≥ 55 °C, SSD ≥ 70 °C. |
| D3 | Saúde SMART | M | **feito (0.11.0)** (smartctl pelo script; temperatura dos discos USB) | Precisa de root: app Scrutiny (integração por API) **ou** `smartctl -j` via SSH restrito. |
| D4 | Previsão de disco cheio | B | feito (0.5.0), **opcional e desligada por padrão** (pedido do Bruno: Configurações → Sistema → Previsão de disco cheio, preferência `diskForecast`) | Uso diário (gravado sempre) por disco em `/data/disk-history.json` (180 dias), regressão linear sobre 30 dias. Avisos: "vai encher em ≤ 30 dias" e "≥ 90% ocupado" (este é o "disco > 90%" do C1). |
| D5 | Mapa do espaço (treemap) no gerenciador | M | feito (0.6.0) | Varredura limitada (tempo/entradas) em segundo plano, cache por pasta; desenho em canvas/SVG. |
| D6 | Correção do SMART ("context deadline exceeded") | M | **feito (0.11.2)** | Um disco por vez, SATA primeiro nos USB, modo guardado por número de série, "sem SMART" depois de travar uma vez (decisão 46). Script 5. |

## E. Apps

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| E1 | Logs do container | B | feito (0.7.0) | Liberar `GET /containers/{id}/logs` no proxy (`-allowGET`); stream com `follow` via SSE; limitar tamanho. |
| E2 | Ligar/parar apps | B | feito (0.7.0) | `-allowPOST` para `start`/`stop`; mesma opção `BRUNOS_DOCKER_ACTIONS`. Parar via Docker não muda o estado no Runtipi (a interface avisa). **Desinstalar** (pedido do Bruno em 08/11/2026): botão que abre `/apps/<loja>/<app>` no painel do Runtipi (escolha do Bruno, em vez de runtipi-cli via SSH restrito). |
| E3 | Pastas na tela inicial | M | feito (0.7.0) | Preferência `folders: [{id,name,apps:[...]}]`; arrastar app sobre app cria pasta; janela da pasta estilo iOS/Umbrel. |
| E4 | Páginas na tela inicial | M | feito (0.7.0) | Deslizar (toque/trackpad/setas); widgets na primeira página; preferência `pages`. |
| E5 | Uso por app ao longo do tempo | M | feito (0.7.0) | Amostrar `usage` a cada 1 min em segundo plano (custo: stats one-shot de todos os containers; avaliar intervalo), guardar 24 h em memória/disco. |
| E6 | Botão de atualizações ao lado do sino + frequência + atualizar pela interface | M–A | **feito (0.12.0)** | Clique abre o painel e já confere; frequência 6 h/12 h/diária/semanal/manual; `apt upgrade` e `runtipi-cli app update` pelo script (`systemd-run`), com confirmação e andamento (decisão 47). Script 6. |
| E7 | Script do servidor que se atualiza sozinho (assinado) | M | **feito (0.12.1)** | Decisão 49. Falta o Bruno cadastrar o segredo `BRUNOS_SCRIPT_KEY` no GitHub. |
| E8 | Backup do Runtipi antes de atualizar app | B | **feito (0.12.1)** | Decisão 50; caixinha sempre marcada. |

## F. Arquivos

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| F1 | Vídeo com avançar/retroceder (HTTP Range) | B | feito (0.6.0) | O Range já funcionava (`http.ServeContent`), mas a imagem `scratch` não tem `/etc/mime.types`: `.mp4`/`.mp3` iam como download. Tabela própria de tipos de mídia, `.mkv` liberado, aviso quando o navegador não toca o codec. |
| F2 | Editor de texto com realce | M | feito (0.6.0) | CodeMirror 6 (escolha do Bruno) num bundle vendorizado (`tools/codemirror` → `web/static/vendor/codemirror/`, ~650 KB, ~230 KB gzip, carregado sob demanda). Salvar atômico com conferência de mtime (409 se mudou), máx. 5 MB, recusa binário/não-UTF-8/links. "Novo arquivo de texto". |
| F3 | Link de compartilhamento com expiração e senha | M | adiado (junto com a etapa 3) | Desligado por padrão. Token aleatório, `/s/<token>` público só leitura, expiração, contador, revogar. Cuidado: expõe arquivo para fora. |
| F4 | Envio em pedaços com retomada | M | feito (0.6.0) | Arquivos > 16 MB em pedaços de 8 MB com posição (offset); `.brunos-part-<id>` na pasta de destino; estado em `/data/uploads.json` (sobrevive a reinício); retoma após queda ou recarregar a página (mesmo arquivo, mesma pasta); pendentes > 48 h apagados. |
| F5 | Extrair zip e encontrar duplicados | B–M | feito (0.6.0) | `.zip`, `.tar`, `.tar.gz` pelo `os.Root` (zip-slip impossível), links ignorados, limite de espaço livre − 1 GB e 200 mil itens, apaga tudo se falhar. Duplicados: tamanho → resumo (começo+fim) → SHA-256; já marca as cópias menos a mais antiga. |
| F6 | Enviar do celular pelo menu Compartilhar | B | adiado (celular) | Depende de A3 (Web Share Target no manifest) → pasta configurável. |

## G. Monitoramento

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| G1 | Histórico longo (24 h e 7 dias) | M | feito (0.8.0) | Agregados por minuto/hora em arquivo circular em `/data`; gráfico com seletor de período. |
| G2 | Processos do servidor (tipo htop) | B | feito (0.8.0) | Ler `/host/proc/[pid]/stat`, `status`, `cmdline`, `cgroup` (app de cada container). Nomes de usuário por `/etc/passwd` montado ro (escolha do Bruno). Sem matar processos. |
| G3 | Internet: latência, quedas, Speedtest Tracker | B | latência e quedas feitas (0.8.0); **Speedtest vai para a 0.9.0** (precisa de token; o Bruno preferiu decidir tokens junto com as integrações) | Ping TCP/HTTP periódico (sem ICMP, que exige capability); integração com a API do Speedtest Tracker. |

## H. Segurança

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| H1 | Verificação em dois passos (TOTP) | M | adiado | RFC 6238 com stdlib (HMAC-SHA1); QR code (gerar em JS ou Go sem dependência); códigos de recuperação. |
| H2 | Sessões ativas com "encerrar" | B | adiado | `sessions.json` já guarda ip/userAgent/criação; listar e revogar. |
| H3 | Aviso de login novo | B | feito (0.5.0) | `internal/logins`: navegador + sistema + rede (rede local conta como um lugar só; internet por faixa /24 ou /48), `/data/logins.json`. Também avisa quando um endereço é bloqueado por errar a senha (no máximo 1 aviso a cada 6 h por endereço). |
| H4 | Contas de convidado (família) | A | **futuro** (adiado em 10/11/2026) | Papéis (admin/convidado), permissões por recurso (arquivos, terminal, configurações, apps visíveis). Muda auth e todas as rotas. |
| H5 | Revisão de segurança dos acessos e do script do servidor | M | **futuro** (pedido em 11/11/2026) | "No futuro vamos fazer um checagem de segurança antes e podemos rever esses detalhes de acesso e excução de scripts." Revisar os pedidos do brunos-host (desligar, limite, leituras, atualizar Linux/apps). |

## I. Rede da casa

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| I1 | Quem está na rede (WatchYourLAN) | B | a fazer | API do WatchYourLAN; aviso de aparelho desconhecido (C1). |
| I2 | Wake-on-LAN | B | **futuro** (adiado em 10/11/2026) | Pacote mágico UDP broadcast; da rede bridge do Docker o broadcast pode não chegar à LAN → testar; alternativa via SSH (`wakeonlan` no host). |
| I3 | Widget Tailscale e AdGuard | B–M | a fazer | Tailscale: API local do tailscaled exige socket (não montar) → usar API web com token ou SSH. AdGuard: API com usuário/senha. |

## J. Aparência

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| J1 | Tela de bloqueio com relógio grande | B | **feito (0.10.0)** | Com senha (padrão, vale no servidor: 423) ou só visual; Ctrl+Shift+L, busca, botão direito e inatividade (5/15/30/60 min ou nunca). Decisão 39. |
| J2 | Papel de parede por horário | B | feito (0.9.2), lista livre de horários | Em 08/11/2026 o Bruno tirou as fotos do Immich do papel de parede: elas ficam só no formato Mídia de um Widget (decisão 34). |
| J3 | Widgets de relógio, clima e bloco de notas | B | **feito (0.10.0)** | Clima = Tipo de integração (Open-Meteo, ícones próprios); Relógio e Notas = Formatos novos; notas em `/data/notes.json` com aviso pelo SSE. |
| J4 | Configurações sem a aba "Tudo" | B | **feito (0.12.0)** | Abre na última aba; a pesquisa procura em todas. |
| J5 | Terminal sempre no dock | B | **feito (0.12.0)** | Desmarcável em "Apps no dock". |
| J6 | Tela inicial nova: Disposições, Medidas, posição e Tamanho livres, arrastar novo | A | a fazer (0.13.0) | Decisão 48; detalhes em `plano-etapa10.md`. Celular: Área compacta e Página com widgets e apps em duas telas. |
| — | ~~Tema claro~~ | — | **recusado** | O Bruno não vê necessidade. |

## K. Terminal

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| K1 | Comandos salvos (botões) | B | **feito (0.10.0)** | `termSnippets`; "executar direto" por comando (desligado = só digita); também na busca. |
| K2 | Abas | M | **feito (0.10.0)** | Até 4; minimizar mantém rodando (ícone na dock); "ainda está rodando?" por `ps` num canal SSH extra (decisão 41); tempo sem uso configurável; aviso no sino ao terminar. |
| K3 | Lembrar a chave SSH (criptografada) | M | **feito (0.10.0)** | Chave do Terminal própria (ED25519), cifrada com chave derivada da senha do BrunOS, aberta só na memória (decisão 40); Conexões salvas. |

## Plano de implementação (combinado em 07/11/2026)

Decisões do Bruno: **uma versão por etapa** (ele testa no servidor antes da próxima); **central de
notificações (C1) já na etapa 1**; desligamento por SSH com **usuário dedicado** `brunos-power` (sem
senha, só a chave do BrunOS, `command=` forçado e sudoers com uma linha); **nada de celular por ora**.

| Etapa | Versão | Itens |
|---|---|---|
| 1 | 0.4.0 | Fundações (`internal/events`, `internal/hostcmd`) · C1 · B1 · B2 · D1 — **feito, aguardando teste no servidor** |
| 2 | 0.5.0 | H3 · D2 · D4 — **feito, aguardando teste no servidor** |
| 4 | 0.6.0 | F1 → F2 → F4 → F5 → D5 — **feito, aguardando teste no servidor** |
| 5 | 0.7.0 | E1 · E2 · E3 · E4 · E5 · A4 (+ desinstalar pelo Runtipi) — **feito, aguardando teste no servidor** |
| 6 | 0.8.0 | G1 · G2 · G3 (sem Speedtest) — **feito, aguardando teste no servidor** |
| 7a | 0.9.0 | **Widgets personalizáveis** (motor: Área de widgets por página, Tamanhos, Formatos, gráficos, janela Widgets, fonte BrunOS, conversão dos widgets atuais, credenciais cifradas) + AdGuard (A2/I3) + Speedtest Tracker (G3) — **feito (0.9.0), aguardando teste no servidor** (Mídia entra junto com Immich/Plex na 0.9.1) |
| 7a+ | 0.9.1 | **Melhorias dos widgets** pedidas depois de usar a 0.9.0: colunas por página com encaixe, arrastar widgets sempre, alça de tamanho (5 tamanhos), Páginas montadas pelo Bruno (apps na página 1 ou 2, continuação automática), Arquivos personalizável (pastas, grade/lista, ícone, cor, espaço livre), correções do Destaque/Armazenamento e da aparência própria — **feito (0.9.1), aguardando teste no servidor** |
| 7b | 0.9.2 | Plex e Immich (A2, Immich só em Widget, formato Mídia) · WatchYourLAN (I1, aviso de aparelho novo) · Tailscale por SSH (I3) · papel de parede por horário (J2) — **feito (0.9.2), aguardando teste no servidor** (o Tailscale pede colar os comandos de atualização do script) |
| 7c | 0.9.3 | Transmission · Syncthing · Nextcloud (pedidos do Bruno em 08/11/2026) — **feito (0.9.3), aguardando teste no servidor**. Vaultwarden **adiado** ("Vamos sem integração com Vaultwarden nesse momento. Deixe para o futuro."): sem token só daria no ar, versão e cadastro aberto; com o ADMIN_TOKEN, usuários (decidir no futuro) |
| 8 | 0.10.0 | J1 · J3 · K1 · K2 · K3 — **feito (0.10.0), aguardando teste no servidor** |
| 9 | 0.11.0 | A1 · B3 · D3 — **feito (0.11.0), aguardando teste no servidor** (B4 já feito na 0.8.1; H4 e I2 adiados em 10/11/2026: "Vamos deixar H4 e I2 para o futuro") |
| 10a | 0.11.2 | D6 (SMART) — **feito (0.11.2), aguardando teste no servidor** |
| 10b | 0.12.0 | J4 · J5 · E6 — **feito (0.12.0), aguardando teste no servidor** |
| 10c | 0.13.0 | J6 — a fazer |
| 3 | futuro | H1 · H2 · F3 (adiados em 08/11/2026: "Gostaria de deixar a etapa 3 para o futuro"; o F3, link público, vai junto porque abre arquivos para fora e dependia da segurança da etapa 3) |
| — | futuro | C2 · A3 · F6 (celular) · H4 (convidados) · I2 (Wake-on-LAN) · H5 (revisão de segurança) |

Em 08/11/2026 o Bruno escolheu o **CodeMirror 6** para o editor (F2), em vez de um editor leve feito à
mão: bundle vendorizado em `web/static/vendor/codemirror/`, carregado sob demanda.

**Decisões que ficam para o início de cada etapa** (perguntar com opções): ~~A2/I3 como guardar
tokens~~ (decidido em 08/11/2026: cifrados com chave em arquivo, decisão 33); ~~F2 qual editor~~; J1
bloqueio só visual ou com senha; K3 riscos de lembrar a chave SSH; H4 quais permissões; A1 como saber a
versão instalada sem tocar no Postgres.

### Etapa 10 (0.11.2 → 0.13.0) combinada em 11/11/2026

Sessão de perguntas uma a uma; resumo aprovado ("Correto. Pode seguir."). Plano completo em
`plano-etapa10.md`; termos novos em `CONTEXT.md` (Disposição, Área de apps, Medida, Tamanho livre);
porquês nas decisões 46–48. Três versões separadas: SMART primeiro ("Pode ser separado"), depois os itens
pequenos com as atualizações, depois a tela inicial. O celular volta a contar: "Futuramente o acesso ao
dashboard no celular será muito importante" — testar sempre em tamanho de celular.

### Etapa 9 (0.11.0) combinada em 10/11/2026

Pedido: "Pode seguir para a etapa 9". H4 e I2 adiados; B4 já existia. Termos novos em `CONTEXT.md`
(Medidor, Estimativa calibrada); porquês nas decisões 42–44.

- **B3 Consumo e custo**: Medidor em Configurações → Energia (Automático = tomada → bateria
  descarregando → estimativa calibrada; ou fixo: tomada, bateria, processador/RAPL, estimativa).
  Integração nova **Tomada Tuya** (protocolo local 3.3/3.4/3.5; a do Bruno é 3.5, DPS 19 = W×10,
  20 = V×10, 18 = mA, 17 = kWh×100). Amostra a cada minuto mesmo com a tela fechada; tarifa R$/kWh
  (padrão 0,90); kWh e R$ de hoje/mês/projeção; 24 h por minuto + 13 meses por dia. Seção "Consumo" na
  janela Energia, Métricas da fonte BrunOS e Modelos "Consumo e custo", "Consumo 24 h", "Gasto do mês".
  RAPL só quando o Medidor precisa (pelo script, pedido `energia`).
- **D3 Saúde SMART**: `smartctl` pelo script (pedido `smart`), a cada 6 h com `-n standby`; selo
  OK/Atenção/Falhando no Uso ao vivo + detalhes; avisos uma vez por mudança (FAILED = crítico; setores
  realocados/pendentes novos = aviso). Temperatura dos discos USB (sem drivetemp) a cada **20 min**
  (pedido `temperatura`), também sem acordar.
- **A1 Atualização de apps**: 1×/dia (+ "Conferir agora"), `apps/<loja>/<app>` × `repos/<loja>/apps/<app>`
  pelo `tipi_version` (loja oficial "migrated" e a do Bruno; `_user` fora). Selo ↑ no ícone, "v1.2 →
  v1.3" + "Atualizar no Runtipi" nos detalhes, aviso no sino por versão (desligável), busca e Métrica.
- **Atualizações do Linux**: 1×/dia pelo script (pedido `atualizacoes`: apt-check, reboot-required,
  lista); Configurações → Sistema, "Atualizar no Terminal" (comando digitado, sem Enter), avisos de
  reinício necessário e de segurança, Métricas.
- Script do servidor **versão 4** com os quatro pedidos de leitura (uma atualização só).

### Etapa 8 (0.10.0) combinada em 10/11/2026

Tudo numa versão só (pedido do Bruno: "Pode trazer tudo junto no mesmo release"). Termos em
`CONTEXT.md` (Sessão, Bloqueio, Tela de bloqueio, Chave do Terminal, Conexão salva, Aba, Comando
salvo); porquês nas decisões 39–41.

- **J1 Bloqueio**: modo "com senha" (padrão) ou "só visual" nas Configurações. Com senha, o servidor
  marca a Sessão como bloqueada e recusa toda a API (inclusive Arquivos e Terminal) até a senha; o
  desbloqueio usa o limitador do login (errou demais → a Sessão é encerrada). Bloqueia pelo botão, pela
  busca `Ctrl+K`, por atalho e por inatividade (5/15/30/60 min ou nunca, padrão 15; só mouse e teclado
  contam). Cada Sessão bloqueia sozinha. Tela: relógio grande, data, saudação; no modo só visual também
  clima e número de avisos. As Abas do Terminal não são encerradas durante o Bloqueio.
- **K3 Chave do Terminal**: ED25519 gerada pelo BrunOS, cifrada com chave derivada da senha do BrunOS
  (PBKDF2 → AES-GCM). A chave derivada fica só na memória enquanto a Sessão está desbloqueada (some no
  Bloqueio com senha, no logout e no reinício; aí o Terminal pede a senha do BrunOS). Recifrada ao
  trocar a senha; botão para apagar; linha `authorized_keys` pronta para copiar. **Conexões salvas**
  (lista, uma principal; começa com uma); abrir o Terminal conecta direto na principal.
- **K2 Abas**: até 4 no total; "–" minimiza (Abas seguem vivas, pontinho na dock), "×" fecha. Tempo
  para encerrar Aba sem ninguém olhando: 3/5/10/15/20/30 min ou nunca (padrão 15). Antes de fechar
  (pelo "×" ou pelo tempo) um `ps` num canal SSH extra confere se há algo rodando: no "×" pergunta, no
  tempo adia. Guarda ~2.000 linhas de tela por Aba; Aba minimizada com algo rodando é conferida a cada
  1 min e avisa no sino quando o comando termina.
- **K1 Comandos salvos**: lista única, faixa de botões que dá para esconder e também na busca
  `Ctrl+K`; cada um com "executar direto" (desligado por padrão).
- **J3**: **Clima** é Tipo de integração sem credencial (Open-Meteo, cidade por nome, a cada 15 min só
  se usado), com Métricas, ícones próprios de tempo e Modelo pronto. **Relógio** é Formato novo
  (digital/analógico, segundos, data, fuso; sempre 24 h). **Notas** é Formato novo (texto livre ou
  lista de tarefas, editado no widget, salvo em `/data`, aviso de mudança pelo SSE que já existe, vale
  a última gravação, até 20 mil caracteres).
- Depois dos testes: publicar e abrir o PR na loja sem perguntar.

### Etapa 7 (0.9.0 → 0.9.2) combinada em 08/11/2026

Pedido do Bruno: widgets com "sua própria guia de configuração e criação", flexíveis em apps, funções,
fontes, posição, formato, gráficos e textos, **sem pesar** no servidor. Escolhas (termos em
`CONTEXT.md`, porquês nas decisões 31–34):

- Integração → Métricas → Widget; widgets prontos são Modelos; os 6 widgets atuais viram Modelos da
  fonte BrunOS (Arquivos vira o formato Atalhos), convertidos sozinhos na atualização.
- Área de widgets separada no topo de cada página; Tamanhos Pequeno 1×1, Largo 2×1, Alto 1×2,
  Grande 2×2 e Faixa; Formatos Destaque, Grade, Lista, Gráfico, Mídia, Atalhos.
- Gráficos SVG próprios: linha/área, barras, rosca e mini-gráfico; período 1 h / 24 h / 7 dias.
- Por Espaço: rótulo, unidade, ícone, cor por faixa, casas decimais. Por Widget: clique (app, janela do
  BrunOS, endereço ou nada) e aparência opcional — **por padrão segue o tema do BrunOS**.
- Janela **Widgets** no dock (Meus widgets · Modelos · Integrações com avisos), prévia ao vivo; menu
  rápido e modo edição na tela inicial.
- Integração em qualquer endereço, várias do mesmo tipo, endereço sugerido pelo Runtipi; frequência
  por tipo ajustável de 10 s a 1 h; coleta híbrida; Avisos prontos (sem regras livres).

Depois de usar a 0.9.0 (mesmo dia), o Bruno pediu e escolheu (decisão 35): colunas fixas por página com
encaixe ("um widget largo e ao lado direito dele outros widgets nas duas linhas"), arrastar sempre como
os ícones, redimensionar só entre os 5 tamanhos, páginas montadas por ele ("uma pagina 1 com widgets de
monitoramento e uma página 2 só com os ícones dos app"), Arquivos com pastas escolhidas, grade ou lista,
ícone e cor e espaço livre, e **melhorias primeiro** (integrações andam uma versão: 0.9.2 e 0.9.3).

### Etapa 1 (0.4.0) em detalhe

- **`internal/events`**: `/data/events.json` (`id, time, kind, level info|aviso|critico, title, body,
  read, key`), rotação 500 eventos / 30 dias, deduplicação por `key` + `Resolve(key)`. Eventos novos
  vão pelo SSE que já existe (`/api/stats/stream`, `event: notify`), sem conexão extra. Rotas
  `GET /api/events`, `POST /api/events/read`, `POST /api/events/clear`.
- **`internal/hostcmd`**: chave ED25519 em `/data/hostcmd_key`; TOFU reaproveitando o
  `known_hosts.json` do terminal (extrair helper comum); assistente mostra as linhas de `useradd`,
  `authorized_keys` (`restrict,command="sudo /sbin/shutdown -h +1 ..."`) e sudoers.
  **Pegadinha:** com `command=` forçado, qualquer sessão desliga o servidor; o botão "Testar" só
  autentica e fecha **sem abrir sessão** (teste automatizado obrigatório).
- **B2 (`internal/power`)**: assina as amostras do `sysinfo.Collector`, detecta tomada → bateria →
  tomada, grava `/data/power-events.json`, persiste o último consumo (o `estimate` deixa de sumir).
- **B1 (`internal/power`)**: limite % (20), espera (2 min), **simulação ligada por padrão**;
  `armado → contagem (evento crítico + faixa "Cancelar") → executa via hostcmd`; voltar a tomada
  cancela. Rotas `GET/PUT /api/power/config`, `GET /api/power/events`, `POST /api/power/cancel`,
  `GET /api/power/setup`, `POST /api/power/test`.
- **D1**: a cada 60 s, `files.Service.Disks()`; mapeado e não montado → evento crítico
  `disk-unmounted:<nome>`; voltou → resolve.
- **Frontend**: `js/notifications.js` (sino, painel, toasts), `js/power.js` (seção Energia com
  assistente e faixa de contagem), quedas no diálogo da bateria, modo demonstração em `api.js`.
- **Testes**: unitários de events/power (relógio falso)/hostcmd (SSH em processo); apitest;
  smoke com usuário `brunos-power` no OpenSSH de teste apontando para um script que só registra.
  No servidor real: Testar → simulação tirando da tomada → só depois desligar a simulação.

### Etapa 2 (0.5.0) em detalhe

- **H3 (`internal/logins`)**: no login com sucesso, `Seen(ip, userAgent)`; aparelho novo → aviso
  (`info` se veio da rede local, `aviso` se veio da internet). Ao atualizar para a 0.5.0, as sessões
  já abertas são aprendidas sem avisar. `auth.Manager.OnBlock` avisa quando um IP é bloqueado.
- **D2 (`internal/sysinfo/drivetemp.go`)**: liga o hwmon `drivetemp` ao `sdX` (`hwmonN/device/block/*`)
  e o hwmon `nvme` ao controlador. `readHwmon` (a cada 2 s) **ignora** o `drivetemp`. HD só é lido de
  novo após 1 min **e** se `/sys/block/sdX/stat` mudou; parado, mostra a última medida com o horário.
  `Disk.temp`/`tempAt` no snapshot.
- **D4 (`internal/diskhist`)**: `Record` a cada hora (substitui o ponto do dia), `Forecasts()` com
  regressão sobre 30 dias (mínimo 5 pontos em 6 dias). Rota `GET /api/disks/forecast`.
- **Frontend**: `js/disks.js` (temperatura com cores, "+X/dia · cheio em ~N dias", dica "Como ativar"
  o drivetemp quando um HD não tem temperatura). No celular a barra de uso desce para a linha de baixo.
