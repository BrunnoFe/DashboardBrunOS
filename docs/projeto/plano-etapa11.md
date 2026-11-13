# Plano da etapa 11 (0.13.1 → 0.14.0 → 0.15.0 → 0.16.0)

Combinado com o Bruno em 12/11/2026 numa sessão de perguntas ("Pode seguir."). Termos novos em
`CONTEXT.md` (Régua, Tamanho do ícone, Espaço entre os ícones, Histórico, Período, Ponta viva, Disco, Pasta
de montagem, Formatar, Senha de formatação); porquês nas decisões 52–54. Quatro versões, nesta ordem; cada
uma é testada no servidor antes da próxima.

---

## 0.13.1: ícones com tamanho fixo (Régua)

**Problema.** A Área de apps tem largura própria (`.page-apps`: no máximo 8 × `--app-w`) e `appsPer`
(`web/static/js/app.js`) encolhe o ícone até 44 px quando a Medida pede mais colunas do que cabem. O
tamanho do ícone muda com a Medida e não tem relação com a Área de widgets.

**Régua** (`layout.js`, função nova `regua(pageW)`): a largura que a Área de widgets teria com 6 colunas
naquela tela = `6·w + 5·gapX·k`, com `w` do `quadro(pageW, ∞, 6, 1)` (150–260 px). Sem caber (tela
estreita), a Régua é a largura útil da página. Calculada uma vez por desenho, igual em todas as Páginas
(não depende da Medida da Página).

**Tamanho do ícone** (substitui o controle 48–88 px `iconSize`):
- `iconSize` vira `'p' | 'm' | 'g' | 'e'` (Pequeno, Médio, Grande, Enorme). Migração ao ler: número
  < 52 → p, < 60 → m, < 72 → g, senão e. O padrão é `g`.
- Casa do Grande = `Régua / 16`; ícone = casa / 1,58 (o nome cabe embaixo). Proporções: p 0,75 · m 0,88 ·
  g 1 · e 1,2.
- Piso por tamanho (px do ícone): p 44 · m 52 · g 60 · e 72. Ícone = `max(piso, proporção × ícone do
  Grande)`; a casa acompanha (ícone × 1,58).
- Colunas que cabem = `floor(larguraDisponível / casaMínima)`; se a Medida pede mais, a Área fica
  compacta (menos colunas, na ordem de leitura) sem mudar a Medida guardada. Nunca encolhe o ícone.

**Espaço entre os ícones** (Aparência, chave nova `iconGap: 'fixo' | 'elastico' | 'preencher'`, padrão
`fixo`):
- Fixo: casa constante; largura = colunas × casa; centralizada.
- Elástico: casa = `min(Régua / colunas, 1,6 × casa mínima)`; centralizada quando bate no limite.
- Preencher: casa = `Régua / colunas` (sempre de ponta a ponta).
- Páginas sem widgets e "Só apps": mesma Régua, centralizada.
- Widgets à esquerda/direita: o tamanho não muda; a largura disponível é a sobra ao lado dos widgets
  (Fixo: as colunas que couberem; Elástico/Preencher esticam até a sobra).
- Medida Automática: colunas = quantas couberem na Régua com a casa mínima (máx. 16); linhas = quantas
  couberem na altura, como hoje.

**Código.** `drawHome`/`appsPer` deixam de mexer em `--app-icon` por caixa; CSS da `.page-apps` usa
`grid-template-columns: repeat(c, var(--casa))` com a largura calculada; sai o `min(100%, 8 × --app-w)`.
A pasta (`fd-grid`) e a paleta Ctrl+K continuam com os tamanhos próprios. Configurações: dois controles
segmentados em Aparência ("Tamanho dos ícones", "Espaço entre os ícones") com prévia ao vivo.

**Testes.** Playwright em modo demonstração: 1920, 1440, 1280, 1024, 768 e 390 px; Medidas 16 × 2, 10 × 3,
8 × 2, 4 × 4 e Automática; os 4 tamanhos × 3 espaços; Disposições wtop, atop, wleft e aonly. Conferir:
ícone com o mesmo px em todas as Medidas da mesma tela; Grande + 16 colunas = largura dos widgets;
centralizado; nada de rolagem horizontal. Unitário de `regua()` e da migração do `iconSize`.

---

## 0.14.0: Históricos de 30 dias + gráficos novos

### Backend: `internal/tsdb` (pacote novo, um só para todos)

- **Série**: nome (`sys.cpu`, `app.<id>.mem`, `net.lat`, `disk.<série>.rd`...) + três anéis de tamanho
  fixo:
  - fina: 1 min × 2880 (48 h), `float32` (média do minuto);
  - média: 5 min × 2016 (7 dias), média + pico;
  - grossa: 20 min × 2160 (30 dias), média + pico.
  - Disponibilidade (no ar/fora): média = fração no ar (0–1); pico = pior minuto.
- `Add(nome, valor, t)` acumula o minuto (média ponderada pelo tempo, como o `history` faz); ao virar o
  minuto, empurra para a fina e acumula nas outras duas.
- `Query(nome, período)` escolhe a camada: 5 min/1 h/24 h/48 h → fina; 7 dias → média; 30 dias → grossa.
  Devolve `{t, v, max?}`; buracos ficam `null` (o gráfico já corta a linha).
- **Arquivo**: `/data/tsdb/` com um arquivo binário por grupo (`sys`, `apps`, `net`, `disks`, `integ`):
  cabeçalho com versão e, para cada série, os anéis em `float32` little-endian. Escrita atômica
  (`internal/store`), 0600. Fina gravada a cada 5 min; média e grossa a cada 20 min; tudo ao desligar
  (SIGTERM).
- Série sem dado novo há 30 dias (app desinstalado, disco que sumiu) é apagada.
- Limite: no máximo 400 séries (proteção de memória); passou disso, não cria série nova e avisa no log.
- ~45 KB por série; ~100 séries ≈ 4,5 MB em disco e na RAM. Medir a RAM com 20 apps no `smoke.sh`
  (o container tem limite de 128 MB).

### Quem passa a gravar

- `internal/history` (CPU, memória, temperatura, rx, tx) → tsdb, + ventoinha, bateria % e watts do
  Medidor. Ler o `/data/history.json` antigo uma vez e importar (24 h e 7 dias), depois apagar.
- `internal/appmon` (CPU, memória, no ar por app) → tsdb. A lógica do aviso continua igual; substitui a
  decisão 26.
- `internal/netmon` (latência) → tsdb; as quedas continuam em `/data/internet.json`.
- **Coleta nova**: leitura e escrita por disco em `internal/sysinfo`, lendo `/host/proc/diskstats` a cada
  coleta (bytes = setores × 512; % ocupado pelo campo "tempo fazendo E/S"). Só discos inteiros (`sd*`,
  `nvme*n1`, `mmcblk*`), chave pelo número de série quando conhecido (vem do `smart`).
- `internal/integrations/series.go` → tsdb (a camada fina fica com buracos onde a coleta é de 5 min; a
  média cobre); camada de 7 dias passa a 30.
- Temperatura dos HDs pelo SMART (a cada 20 min) → tsdb como ponto esparso.
- `internal/diskhist` (1/dia, 180 dias) continua como está.

### API

- `GET /api/history?serie=<nome>&periodo=<5m|1h|24h|48h|7d|30d>` (várias `serie=` numa chamada). Lista
  permitida de prefixos; erro amigável em pt-BR.
- As rotas antigas (`/api/stats/history`, histórico do app, internet) passam a ler do tsdb, com o mesmo
  formato, para não quebrar a interface durante a troca.

### Gráficos (`web/static/js/charts.js`)

- **Peça única** `chart(el, { series, periodo, unidade, formato, eixo, ponta })` que desenha o SVG e uma
  camada HTML por cima (rótulos, bolinha e balão em HTML posicionados em %, porque o SVG usa
  `preserveAspectRatio="none"` e deformaria círculos e texto).
- **Eixo Y**: 3 valores redondos, texto pequeno e apagado à esquerda, linhas-guia bem fracas
  (`rgba(255,255,255,.06)`). Eixo X: horário do começo e do fim.
- **Mouse/dedo**: linha vertical fina, bolinha na curva, balão de vidro com horário e valor (e pico em 7
  e 30 dias); `pointermove` limitado por `requestAnimationFrame`; no toque, arrastar.
- **Ponta viva**: só no Período ao vivo (5 min do Uso ao vivo, leitura/escrita ao vivo dos discos e
  widgets de Gráfico com Métricas ao vivo). Bolinha com pulso leve (desligado com "reduzir movimento") e
  valor pequeno acima.
- **Seletor de Período** `1 h · 24 h · 48 h · 7 dias · 30 dias` (+ "5 min" onde há ao vivo) em: Uso ao
  vivo, Detalhes do app, Internet, Energia. Widget de Gráfico: a opção Período do editor ganha 48 h e
  30 dias.
- **Migrar** os três gráficos feitos à mão (Uso ao vivo em `app.js`, `appinfo.js`, `internet.js`) e os do
  `energy.js` para a peça única. Faixa de disponibilidade do app com balão ("14:20 · fora do ar"); quedas
  de internet como faixa vermelha no gráfico de latência; barras de kWh com "12/nov: 0,41 kWh ·
  R$ 0,39".
- **Não mexer**: mini-gráficos dos widgets, roscas, mini-gráfico do topo da janela Internet, Mapa do
  espaço e Duplicados. Widgets de Gráfico: só o balão do mouse, sem eixo.

### Testes

`internal/tsdb`: anéis (virada de minuto, de 5 e de 20 min, buracos, pico), escolha de camada, gravar e
ler, arquivo corrompido (começa vazio e avisa, não trava), importação do `history.json`, limite de
séries; `-race` com Add e Query simultâneos. `apitest.py`: `/api/history` (lista permitida, períodos).
`smoke.sh`: reiniciar o container e conferir que o histórico de apps e da latência continua lá; medir a
RAM. Playwright: cada gráfico migrado com o balão aberto e a ponta viva, em 1440 e 390 px.

---

## 0.15.0: janela Discos (leitura e ações seguras)

### Script do servidor (próxima versão do `brunos-host`, assinado, troca automática)

Pedidos novos, todos validados como os de hoje (disco `sd[a-z]{1,2}` ou `nvme[0-9]n1`, `[ -b /dev/$d ]`):
- `discos-detalhe`: `lsblk -J -b -o NAME,PATH,TYPE,SIZE,MODEL,VENDOR,SERIAL,WWN,REV,TRAN,ROTA,RM,FSTYPE,
  LABEL,UUID,PARTUUID,MOUNTPOINTS,FSUSED,FSAVAIL,PTTYPE` + `findmnt -J` (raiz, swap) + `/etc/fstab`
  filtrado (só UUID e pasta). Inclui discos sem Pasta de montagem.
- `estado <disco>`: `hdparm -C` (dormindo/acordado, não acorda o disco); sem hdparm, "desconhecido".
- `smart-teste <disco> <modo> <curto|longo>`; o andamento e o resultado vêm no `smart` de sempre.
- `erros <disco>`: `journalctl -k --since -30d` filtrado só pelas linhas do disco (erro de E/S, reset,
  USB desconectado), no máximo 200 linhas.
- `velocidade <disco>`: leitura sequencial de 1 GiB (`dd if=/dev/<disco> of=/dev/null bs=1M count=1024
  iflag=direct`; não grava nada); um por vez, com limite de tempo.
- `ejetar <disco>`: só disco USB e só se nada estiver montado nele; `sync` e
  `echo 1 > /sys/block/<disco>/device/delete`.

### BrunOS

- `internal/disks` (pacote novo): junta `discos-detalhe` + SMART (`internal/smart`) + temperatura e E/S
  do tsdb + `diskhist` + apps que usam o disco (pastas montadas nos containers, pelo Docker, comparadas com
  as Pastas de montagem) + **linha do tempo** (conectado, sumiu, montado, SMART mudou, pico de
  temperatura) em `/data/disks.json` (até 200 eventos por disco).
- Atributos SMART explicados em pt-BR com "normal / preocupante" (tabela no código; ATA e NVMe).
- Avisos no sino: disco novo conectado, disco sumiu, temperatura alta, SMART piorou (os que já existem
  continuam).

### Interface (`web/static/js/disksapp.js`, novo)

- Janela "Discos", ícone fixo na dock entre Arquivos e Uso ao vivo (alvo novo da dock + migração de quem
  já tem a dock personalizada: entra ao lado de Arquivos).
- Lista à esquerda (nome, selo, temperatura, barra de espaço); grupo "Sem pasta de montagem".
- Abas: **Visão geral** (identificação, caminho `/dev/disk/by-id`, barra de partições, apps que usam,
  dormindo/acordado), **Saúde** (SMART explicado, teste curto/longo com andamento), **Desempenho**
  (leitura/escrita e % ocupado com Período e ponta viva no ao vivo, temperatura, teste de velocidade),
  **Espaço** (histórico, previsão, atalhos para Mapa do espaço e Duplicados daquele disco), **Linha do
  tempo** (eventos + erros do registro do Linux). Botão "Ejetar com segurança" nos discos USB.
- Uso ao vivo: a lista de discos vira resumo (nome, selo, temperatura, espaço) + "Abrir Discos".
- Celular: lista e detalhe em telas seguidas (voltar no topo).

### Testes

`internal/disks` com Query falsa (lsblk de exemplo com USB, NVMe, disco sem partição, partição sem
montagem); `hostcmd` (validação dos pedidos novos); `smoke.sh` com `lsblk`, `hdparm`, `journalctl` e `dd`
falsos no PATH do OpenSSH de teste. Playwright da janela em 1440 e 390 px.

---

## 0.16.0: montar, desmontar e Formatar

### Script do servidor

Pedidos novos, todos validados duas vezes (no BrunOS e no script), como os de hoje. Os detalhes de
validação, o cadastro e a conferência da Senha de formatação e as travas ficam em
`docs/privado/plano-etapa11-formatar.md` (não versionado). Em resumo:

- `montar` e `desmontar`: só por UUID e só dentro de `/mnt/`, com cópia de segurança da configuração
  antes de mexer e volta atrás se falhar; recusa o disco do sistema e disco em uso.
- `rotulo`: troca o rótulo do disco desmontado, conforme o sistema de arquivos.
- `senha-formatar`: cadastro **só pelo Terminal, com sudo** (recusado quando o pedido vem do BrunOS).
- `formatar`: confere a Senha de formatação, trava depois de erros, confere de novo o disco certo e todas
  as travas antes de apagar, e mostra o andamento como nas atualizações compridas.

### BrunOS

- Assistente "Formatar" na janela Discos: escolhe o sistema de arquivos (uma linha explicando cada um) e
  o rótulo → pede de novo a senha do BrunOS → mostra o que será apagado (partições, rótulos, espaço usado)
  → digitar o rótulo ou o modelo → Senha de formatação → andamento. Sem Senha de formatação criada: o botão
  "Criar senha de formatação" abre o Terminal com `sudo brunos-host senha-formatar` já digitado (não
  executa).
- "Montar": nome da pasta (sugestão pelo rótulo), confirmação e, no fim, "Reiniciar o BrunOS agora para
  ver o disco (leva uns 5 s)" (restart do próprio container pelo proxy) + Disco mapeado automático no
  Arquivos.
- "Desmontar": mostra apps e processos que usam o disco; recusa enquanto houver.
- Tudo vira evento no sino ("Disco montado", "Disco formatado").

### Testes

Script: `smoke.sh` com um **arquivo de loop** fazendo papel de disco (montar, desmontar, formatar,
senha errada, trava, recusas, volta atrás). `hostcmd`: validação dos argumentos. Playwright do
assistente. Casos completos em `docs/privado/plano-etapa11-formatar.md`.

---

## Ideias anotadas (não aprovadas)

- Formatar uma partição só e editor de partições (criar, apagar, redimensionar).
- "Dormir após X minutos parado" (`hdparm -S`): ganho pequeno e alguns adaptadores USB ignoram.
- Integração com o Beszel como Fonte de Métricas em widgets (não como lugar dos Históricos).
