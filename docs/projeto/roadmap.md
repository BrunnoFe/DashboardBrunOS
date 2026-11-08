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
| A1 | Aviso de atualização de apps do Runtipi | M | a investigar | O Runtipi guarda a versão instalada no Postgres, não em arquivos. Opções: comparar `tipi_version` do repositório da loja com algo legível em `app-data` (verificar), ou API interna do Runtipi. Não abrir acesso ao banco. |
| A2 | Widgets com dados dos apps (Immich: fotos/vídeos; AdGuard: consultas/bloqueios; Plex: tocando agora) | M | a fazer | Cada integração precisa de URL + token do app, guardados no servidor (criptografados? decidir) e **nunca** enviados ao navegador; o backend faz as chamadas. |
| A3 | PWA (instalar no celular) | B | adiado (celular) | `manifest.webmanifest`, ícones, service worker mínimo (só cache da casca; API nunca em cache). Base para F6 e C2. |
| A4 | Monitor de disponibilidade dos apps com alerta | M | a fazer | Checagem HTTP periódica (porta/domínio de cada app) + estado do container; histórico de uptime; alertas pela central (C1). |

## B. Energia e bateria (o notebook é o servidor; a bateria é o nobreak)

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| B1 | **Desligamento seguro** quando a bateria cai abaixo de X% fora da tomada | M | feito (0.4.0) | Prioridade 1. Config: limite %, tempo de espera, aviso antes. Execução via SSH com chave restrita a `sudo /sbin/shutdown -h +1` (ou `systemctl poweroff`). Precisa de assistente de configuração (gera a chave, mostra a linha do `authorized_keys` e do sudoers). Testar sem desligar de verdade (modo simulação). |
| B2 | Registro de quedas de energia | B | feito (0.4.0) | Detectar transições AC→bateria→AC; gravar em `/data/power-events.json` (início, fim, % gasto). Persistir também o último consumo medido (hoje `estimate` some ao reiniciar). |
| B3 | Consumo do servidor (W) e custo (R$/mês) | B–M | a fazer | Intel RAPL (`/sys/class/powercap/intel-rapl:0/energy_uj`, pode exigir permissão de leitura) + consumo da bateria quando descarregando; tarifa em R$/kWh configurável. |
| B4 | Limite de carga em 80% | M | a fazer | Depende do driver (`charge_control_end_threshold` na Dell via `dell_laptop`/`dell-smbios`?). Mostrar estado; aplicar via SSH restrito ou ensinar o comando/udev rule. |

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
| D3 | Saúde SMART | M | a fazer | Precisa de root: app Scrutiny (integração por API) **ou** `smartctl -j` via SSH restrito. |
| D4 | Previsão de disco cheio | B | feito (0.5.0), **opcional e desligada por padrão** (pedido do Bruno: Configurações → Sistema → Previsão de disco cheio, preferência `diskForecast`) | Uso diário (gravado sempre) por disco em `/data/disk-history.json` (180 dias), regressão linear sobre 30 dias. Avisos: "vai encher em ≤ 30 dias" e "≥ 90% ocupado" (este é o "disco > 90%" do C1). |
| D5 | Mapa do espaço (treemap) no gerenciador | M | a fazer | Varredura limitada (tempo/entradas) em segundo plano, cache por pasta; desenho em canvas/SVG. |

## E. Apps

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| E1 | Logs do container | B | a fazer | Liberar `GET /containers/{id}/logs` no proxy (`-allowGET`); stream com `follow` via SSE; limitar tamanho. |
| E2 | Ligar/parar apps | B | a fazer | `-allowPOST` para `start`/`stop`; mesma opção `BRUNOS_DOCKER_ACTIONS`. Atenção: parar via Docker não muda o estado no Runtipi (documentar). |
| E3 | Pastas na tela inicial | M | a fazer | Preferência `folders: [{id,name,apps:[...]}]`; arrastar app sobre app cria pasta; janela da pasta estilo iOS/Umbrel. |
| E4 | Páginas na tela inicial | M | a fazer | Deslizar (toque/trackpad/setas); widgets na primeira página; preferência `pages`. |
| E5 | Uso por app ao longo do tempo | M | a fazer | Amostrar `usage` a cada 1 min em segundo plano (custo: stats one-shot de todos os containers; avaliar intervalo), guardar 24 h em memória/disco. |

## F. Arquivos

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| F1 | Vídeo com avançar/retroceder (HTTP Range) | B | a fazer | `http.ServeContent` no `/api/files/raw` (já com `os.Root`); confirmar `Accept-Ranges`. |
| F2 | Editor de texto com realce | M | a fazer | Editor leve (CodeMirror 6 vendorizado sob demanda?) ou textarea com realce simples; salvar com verificação de mtime (conflito). Limite de tamanho. |
| F3 | Link de compartilhamento com expiração e senha | M | a fazer | Desligado por padrão. Token aleatório, `/s/<token>` público só leitura, expiração, contador, revogar. Cuidado: expõe arquivo para fora. |
| F4 | Envio em pedaços com retomada | M | a fazer | Protocolo simples (offset) ou tus; arquivo temporário por id; limpeza de pendentes. |
| F5 | Extrair zip e encontrar duplicados | B–M | a fazer | `archive/zip` com proteção contra zip-slip e bomba (limites); duplicados por tamanho → hash. |
| F6 | Enviar do celular pelo menu Compartilhar | B | adiado (celular) | Depende de A3 (Web Share Target no manifest) → pasta configurável. |

## G. Monitoramento

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| G1 | Histórico longo (24 h e 7 dias) | M | a fazer | Agregados por minuto/hora em arquivo circular em `/data`; gráfico com seletor de período. |
| G2 | Processos do servidor (tipo htop) | B | a fazer | Ler `/host/proc/[pid]/stat`, `status`, `cmdline` (já montado ro). Ordenar por CPU/memória. Sem matar processos. |
| G3 | Internet: latência, quedas, Speedtest Tracker | B | a fazer | Ping TCP/HTTP periódico (sem ICMP, que exige capability); integração com a API do Speedtest Tracker. |

## H. Segurança

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| H1 | Verificação em dois passos (TOTP) | M | a fazer | RFC 6238 com stdlib (HMAC-SHA1); QR code (gerar em JS ou Go sem dependência); códigos de recuperação. |
| H2 | Sessões ativas com "encerrar" | B | a fazer | `sessions.json` já guarda ip/userAgent/criação; listar e revogar. |
| H3 | Aviso de login novo | B | feito (0.5.0) | `internal/logins`: navegador + sistema + rede (rede local conta como um lugar só; internet por faixa /24 ou /48), `/data/logins.json`. Também avisa quando um endereço é bloqueado por errar a senha (no máximo 1 aviso a cada 6 h por endereço). |
| H4 | Contas de convidado (família) | A | a fazer | Papéis (admin/convidado), permissões por recurso (arquivos, terminal, configurações, apps visíveis). Muda auth e todas as rotas. |

## I. Rede da casa

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| I1 | Quem está na rede (WatchYourLAN) | B | a fazer | API do WatchYourLAN; aviso de aparelho desconhecido (C1). |
| I2 | Wake-on-LAN | B | a fazer | Pacote mágico UDP broadcast; da rede bridge do Docker o broadcast pode não chegar à LAN → testar; alternativa via SSH (`wakeonlan` no host). |
| I3 | Widget Tailscale e AdGuard | B–M | a fazer | Tailscale: API local do tailscaled exige socket (não montar) → usar API web com token ou SSH. AdGuard: API com usuário/senha. |

## J. Aparência

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| J1 | Tela de bloqueio com relógio grande | B | a fazer | Estilo umbrelOS; bloqueio por inatividade opcional (só visual ou exigindo senha? decidir). |
| J2 | Papel de parede por horário ou fotos do Immich ("neste dia") | M | a fazer | Depende da integração Immich (A2) para as fotos. |
| J3 | Widgets de relógio, clima e bloco de notas | B | a fazer | Clima: Open-Meteo (sem chave) chamado pelo backend; notas salvas em `/data`. |
| — | ~~Tema claro~~ | — | **recusado** | O Bruno não vê necessidade. |

## K. Terminal

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| K1 | Comandos salvos (botões) | B | a fazer | Preferência `termSnippets`; botão digita o texto (não executa sem Enter, ou opção). |
| K2 | Abas | M | a fazer | Várias sessões (limite já é 4 no backend). |
| K3 | Lembrar a chave SSH (criptografada) | M | a fazer | Cifrar com chave derivada da senha do BrunOS (PBKDF2 → AES-GCM); só decifra com a senha digitada na sessão. Avaliar riscos. |

## Plano de implementação (combinado em 07/11/2026)

Decisões do Bruno: **uma versão por etapa** (ele testa no servidor antes da próxima); **central de
notificações (C1) já na etapa 1**; desligamento por SSH com **usuário dedicado** `brunos-power` (sem
senha, só a chave do BrunOS, `command=` forçado e sudoers com uma linha); **nada de celular por ora**.

| Etapa | Versão | Itens |
|---|---|---|
| 1 | 0.4.0 | Fundações (`internal/events`, `internal/hostcmd`) · C1 · B1 · B2 · D1 — **feito, aguardando teste no servidor** |
| 2 | 0.5.0 | H3 · D2 · D4 — **feito, aguardando teste no servidor** |
| 3 | 0.6.0 | H1 · H2 |
| 4 | 0.7.0 | F1 → F2 → F4 → F5 → D5 → F3 |
| 5 | 0.8.0 | E1 · E2 · E3 · E4 · E5 · A4 |
| 6 | 0.9.0 | G1 · G2 · G3 |
| 7 | 0.10.0 | A2 · I1 · I3 · J2 |
| 8 | 0.11.0 | J1 · J3 · K1 · K2 · K3 |
| 9 | 0.12.0 | A1 · B3 · B4 · D3 · I2 · H4 |
| — | futuro | C2 · A3 · F6 (celular) |

**Decisões que ficam para o início de cada etapa** (perguntar com opções): A2/I3 como guardar tokens;
F2 qual editor; J1 bloqueio só visual ou com senha; K3 riscos de lembrar a chave SSH; H4 quais
permissões; A1 como saber a versão instalada sem tocar no Postgres.

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
