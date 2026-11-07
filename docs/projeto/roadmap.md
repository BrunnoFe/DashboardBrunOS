# Roadmap aprovado

Em 07/11/2026 o Bruno aprovou **todas** as ideias abaixo ("Gostei de todas as suas sugestões e gostaria
de implementar todas"), **exceto o tema claro** ("não vejo necessidade"). O plano de implementação
ainda vai ser elaborado junto com ele, então nada aqui foi começado.

Legenda: esforço B/M/A (baixo, médio, alto) · status `a fazer` / `em andamento` / `feito (versão)`.
Regra geral: nada de privilégios no container. O que precisar de root no host vai **por SSH com uma
chave restrita** (`command="..."` no `authorized_keys`, ver decisão 11 em `decisoes.md`).

## A. Sugestões originais (da 0.1.0)

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| A1 | Aviso de atualização de apps do Runtipi | M | a investigar | O Runtipi guarda a versão instalada no Postgres, não em arquivos. Opções: comparar `tipi_version` do repositório da loja com algo legível em `app-data` (verificar), ou API interna do Runtipi. Não abrir acesso ao banco. |
| A2 | Widgets com dados dos apps (Immich: fotos/vídeos; AdGuard: consultas/bloqueios; Plex: tocando agora) | M | a fazer | Cada integração precisa de URL + token do app, guardados no servidor (criptografados? decidir) e **nunca** enviados ao navegador; o backend faz as chamadas. |
| A3 | PWA (instalar no celular) | B | a fazer | `manifest.webmanifest`, ícones, service worker mínimo (só cache da casca; API nunca em cache). Base para F6 e C2. |
| A4 | Monitor de disponibilidade dos apps com alerta | M | a fazer | Checagem HTTP periódica (porta/domínio de cada app) + estado do container; histórico de uptime; alertas pela central (C1). |

## B. Energia e bateria (o notebook é o servidor; a bateria é o nobreak)

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| B1 | **Desligamento seguro** quando a bateria cai abaixo de X% fora da tomada | M | a fazer | Prioridade 1. Config: limite %, tempo de espera, aviso antes. Execução via SSH com chave restrita a `sudo /sbin/shutdown -h +1` (ou `systemctl poweroff`). Precisa de assistente de configuração (gera a chave, mostra a linha do `authorized_keys` e do sudoers). Testar sem desligar de verdade (modo simulação). |
| B2 | Registro de quedas de energia | B | a fazer | Detectar transições AC→bateria→AC; gravar em `/data/power-events.json` (início, fim, % gasto). Persistir também o último consumo medido (hoje `estimate` some ao reiniciar). |
| B3 | Consumo do servidor (W) e custo (R$/mês) | B–M | a fazer | Intel RAPL (`/sys/class/powercap/intel-rapl:0/energy_uj`, pode exigir permissão de leitura) + consumo da bateria quando descarregando; tarifa em R$/kWh configurável. |
| B4 | Limite de carga em 80% | M | a fazer | Depende do driver (`charge_control_end_threshold` na Dell via `dell_laptop`/`dell-smbios`?). Mostrar estado; aplicar via SSH restrito ou ensinar o comando/udev rule. |

## C. Notificações

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| C1 | Central de notificações (sino) | M | a fazer | Eventos: app caiu, disco > 90%, disco desmontado, energia caiu/voltou, temperatura alta, atualização disponível, login novo. `/data/events.json` com rotação; marcar como lido. |
| C2 | Avisos no celular (ntfy, Telegram, push do PWA) | M | a fazer | ntfy tem app no Runtipi; Telegram via bot token; Web Push exige VAPID (implementar com stdlib: ECDSA P-256 + JWT, viável). Regras de quais eventos vão para onde. |

## D. Discos

| # | Item | Esforço | Status | Notas técnicas |
|---|---|---|---|---|
| D1 | Alerta de disco mapeado que se desmontou | B | a fazer | Disco mapeado com `isMountPoint == false` → evento crítico (risco de o mergerfs/apps gravarem no SSD do sistema). |
| D2 | Temperatura dos HDs | B | a fazer | hwmon `drivetemp` (módulo do kernel: `sudo modprobe drivetemp` + `/etc/modules-load.d`). Associar hwmon ao `/dev/sdX`. |
| D3 | Saúde SMART | M | a fazer | Precisa de root: app Scrutiny (integração por API) **ou** `smartctl -j` via SSH restrito. |
| D4 | Previsão de disco cheio | B | a fazer | Guardar uso diário por disco (`/data/disk-history.json`), regressão linear simples. |
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
| F6 | Enviar do celular pelo menu Compartilhar | B | a fazer | Depende de A3 (Web Share Target no manifest) → pasta configurável. |

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
| H3 | Aviso de login novo | B | a fazer | Evento na central (C1) quando IP/navegador novo entra. |
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

## Ordem sugerida (a confirmar com o Bruno)

1. B1 + B2 (desligamento seguro, quedas de energia) e D1 (disco desmontado).
2. D2 + D4 (temperatura dos HDs, previsão de cheio).
3. C1 + C2 (central de notificações + ntfy/Telegram) → depois A4, H3, I1 usam a central.
4. H1 + H2 (TOTP, sessões).
5. A3 + F6 (PWA + compartilhar do celular).
6. Arquivos (F1 → F2 → F4 → F5 → D5 → F3), apps (E1, E2, E3, E4, E5), monitoramento (G1, G2, G3).
7. Integrações com apps (A2, I3, J2) e o resto da aparência (J1, J3), terminal (K1–K3), A1, B3, B4, D3, H4.
