# Plano da etapa 10 (0.11.2 → 0.12.0 → 0.13.0)

Combinado com o Bruno em 11/11/2026 numa sessão de perguntas ("Correto. Pode seguir."). Termos novos em
`CONTEXT.md` (Disposição, Área de apps, Medida, Tamanho livre); porquês nas decisões 46–48. Três versões,
nesta ordem; cada uma é testada no servidor antes da próxima.

---

## 0.11.2: correção do SMART

**Problema.** `smartctl -a` de todos os discos numa leitura só, com 2 min de limite. O case JMicron JMS583
(chip de NVMe com HD SATA dentro) faz o `smartctl` adivinhar NVMe; o adaptador não responde e o kernel
reinicia o USB do disco (2 × ~60 s). A leitura toda estoura e vira "context deadline exceeded"; e a cada
20 min a temperatura repete o problema. Com `-d sat` o disco responde em 2,5 s.

**Script do servidor → brunos-host 5** (`internal/hostcmd/hostcmd.go`, `ScriptVersion = 5`):
- `discos`: uma linha por disco com nome, transporte e número de série, via
  `lsblk -dno NAME,TRAN,SERIAL` (não fala com o disco). Só `sd*` e `nvme*n1`.
- `smart <disco> <modo>` e `temperatura <disco> <modo>`: um disco por pedido. Disco validado
  (`sd[a-z]{1,2}` ou `nvme[0-9]n1`, e `[ -b /dev/$disco ]`); modo `sat` ou `auto` (`auto` = sem `-d`).
  Leitura leve: `smartctl -j -n standby -H -i -A` (smart) e `-j -n standby -A` (temperatura).
- O script continua aceitando `smart`/`temperatura` sem argumentos? **Não**: o BrunOS novo só usa a forma
  nova; script antigo responde "pedido inválido" → mensagem "atualize o script" que já existe.
- O script aceita até 3 argumentos agora (hoje é no máximo 2); conferir a validação duas vezes, como já é.

**BrunOS** (`internal/smart`):
- Estado por disco ganha `key` (número de série do `lsblk`, ou o nome se vier vazio), `mode`
  (`sat`/`auto`/vazio = ainda não sabe), `noSmart` (bool), `noSmartAt`, `triedModes`.
- Leitura: pede `discos`; para cada disco que não está em `noSmart`, um pedido com **limite de 50 s**.
  Disco USB sem modo: tenta `sat`; se voltar rápido sem dados (erro, não standby), tenta `auto` na mesma
  rodada. Guardou o modo que deu dados. Estourou o tempo ou nenhum modo deu dados → `noSmart` + evento
  (uma vez). Disco interno (sata/nvme): `auto` direto.
- Temperatura a cada 20 min só dos discos com modo conhecido e sem `noSmart`.
- `Refresh` vira assíncrono: `POST /api/smart/refresh` responde 202 na hora; a interface acompanha pelo
  `GET /api/smart` (campo `running`). `POST /api/smart/retry {key}` limpa `noSmart` e tenta o modo que
  ainda não foi testado (o outro).
- Saída aproveitada mesmo com erro de outro disco; `friendly()` traduz `context deadline exceeded` e
  afins.
- Aviso no sino (`disk-smart-unsupported`, nível info, uma vez por disco): título "Sem SMART: sdb
  (JMicron)"; corpo explica que o adaptador USB não respondeu, que o disco em si está bem, que o BrunOS
  parou de perguntar para não reiniciar o USB, e as saídas (Tentar de novo, case com chip ASMedia).

**Interface** (`web/static/js/disks.js`): etiqueta vermelha pequena **"sem SMART"** no lugar do selo;
nos detalhes do disco, "Tentar de novo" com aviso ("pode reiniciar a conexão USB do disco por alguns
segundos"). "Ler agora" mostra "lendo…" e atualiza sozinho quando terminar.

**Testes**: `internal/smart` com Query falsa (disco que demora, que recusa `sat`, standby, NVMe);
`hostcmd` (validação dos pedidos novos, script sem tabs); `tests/smoke.sh` com o OpenSSH real (pedido
`discos` e `smart sda sat` contra um `smartctl` falso no PATH do contêiner de teste).
**No servidor**: colar os comandos novos de Configurações → Energia e reinstalar o `smartmontools`.

---

## 0.12.0: itens pequenos + atualizações pela interface

1. **Configurações sem "Tudo"** (`app.js` `renderSettings`/`drawSettingsRows`): abas Aparência, Apps,
   Conta, Sistema; abre na última usada (`settingsTab` lembrado; padrão `look`); com texto na pesquisa,
   procura em todas as abas, agrupado por categoria; apagar o texto volta para a aba.
2. **Terminal sempre no dock** (`renderDock`): aparece sempre (exceto `BRUNOS_TERMINAL=false`), depois de
   Configurações, com o pontinho quando há Abas. Preferência `dockHide: []`; o seletor "Apps no dock"
   ganha a linha Terminal (marcada por padrão).
3. **Botão de atualizações ao lado do sino** (`index.html`, novo `web/static/js/updates.js`, CSS em
   `extra.css`): mesmo estilo do sino, seta para cima, bolinha com a soma apps + Linux (laranja se há
   atualização de segurança). Clique abre o painel e já confere, se a última conferência tiver mais de
   10 min (senão só mostra, com "Conferir de novo"). Painel: Apps (lista, clique abre Detalhes,
   botão "Atualizar" por app), Linux (contagem, segurança, reinício necessário, "Ver lista", "Atualizar"),
   hora da última conferência. No celular, painel na largura da tela.
4. **Frequência** em Configurações → Sistema, "Procurar atualizações": 6 h · 12 h · diária (padrão) ·
   semanal · só quando eu mandar. Guardada no servidor (`/data/updates.json`, campo `every`), usada pelo
   `internal/updates` no lugar do 1×/dia fixo.
5. **Atualizar pela interface** (decisão 47), script **brunos-host 6**:
   - `atualizar-linux`: `systemd-run --unit=brunos-atualizar --collect` com
     `DEBIAN_FRONTEND=noninteractive apt-get update && apt-get -y -o Dpkg::Options::=--force-confdef
     -o Dpkg::Options::=--force-confold upgrade`. Recusa se já houver uma rodando.
   - `atualizar-app <app>:<loja>`: nome validado (`[a-z0-9][a-z0-9_-]{0,63}` dos dois lados), roda
     `./runtipi-cli app update <app>:<loja>` na pasta do Runtipi, também pelo `systemd-run`.
     A pasta do Runtipi no servidor entra no script na instalação (comandos de Configurações → Energia),
     descoberta pelo BrunOS pelas montagens dos contêineres do Runtipi no Docker (só leitura) ou digitada
     pelo Bruno se não achar.
   - `atualizacao-estado`: ativo/terminou/falhou + últimas ~15 linhas (`journalctl -u brunos-atualizar`).
   - BrunOS: `internal/updates` ganha `Start(kind, alvo)` e `Status()`; consulta o estado a cada 3 s
     enquanto há atualização e a tela está aberta, a cada 1 min sem tela; retoma depois de reiniciar
     (estado em `/data/updates.json`); no fim, confere de novo e manda aviso no sino.
   - Interface: confirmação ("Atualizar Immich de 1.120 para 1.122?"), rodinha "Atualizando…", últimas
     linhas, um de cada vez (os outros botões esperam).
6. Roadmap: anotar **revisão de segurança do script e dos acessos** (futuro, pedido do Bruno).

**Testes**: unidade do `updates` (frequência, estado retomado, validação do nome do app), `hostcmd`
(pedidos novos), `apitest.py` (rotas novas, 409 com atualização em andamento), smoke com OpenSSH real e
`apt-get`/`runtipi-cli`/`systemd-run` falsos; prints do painel (computador e celular).

---

## 0.13.0: tela inicial nova

### Modelo de dados (preferências)
- `widgets2.v = 3`. Cada widget: `w`, `h` (Quadros) no lugar de `size`. Lugar por Página:
  `widgets2.areas[página] = [{ id, x, y }]` (coluna e linha a partir de 0).
- `pages[página] = { disp, wm: [colunas, linhas], am: 'auto' | [colunas, linhas] }`, com `disp` em
  `wtop` (padrão) · `atop` · `wleft` · `wright` · `wonly` · `aonly`. `widgets2.cols` some.
- Preferência `mobileSplit: true` (chave "No celular, widgets e apps em telas separadas").
- **Migração** (uma vez, ao ler `v < 3`): `s`→1×1, `w`→2×1, `t`→1×2, `l`→2×2, `f`→largura da Medida×1.
  Medida da Página = colunas fixas de hoje ou as automáticas, linhas = as necessárias (até 4).
  Encaixe na ordem de hoje (leitura, primeiro lugar livre); o que passar de 4 linhas vai para uma Página
  nova. A Disposição migrada é `wtop`, apps em `auto`. Os Modelos (`widgets.js`) passam a usar `w`/`h`.
  A proteção de preferências da 0.11.1 continua valendo (cópia diária antes).

### Funções puras novas (`web/static/js/layout.js`, testáveis com `node --test`)
- `fits(area, item, x, y)`, `firstFree(area, w, h)`, `place(area, item)` (para a migração e "não coube").
- `dragPlan(area, id, x, y, w, h)` → novo arranjo ou `null` (vermelho): lugar livre → vai; ocupado →
  quem estava lá vai para o lugar deixado (troca) se couber, senão para o livre mais próximo; os outros
  não se mexem.
- `compact(area, cols)` → arranjo em ordem de leitura com largura limitada a `cols` (não grava nada).
- `quadro(espaçoW, espaçoH, medida)` → tamanho do Quadro entre o mínimo (~150 px) e o máximo (~260 px),
  mantendo a proporção de hoje (240 × 148); `null` = abaixo do mínimo → compacto.
- Testes em `tests/layout.test.mjs` (ferramenta de desenvolvimento, não vai para a imagem).

### Desenho (`app.js` `drawHome`, `extra.css`)
- Área de widgets: CSS grid com `grid-template-columns/rows` da Medida e `grid-column/grid-row`
  explícitos por widget; linhas vazias no fim não ocupam espaço. Quadro calculado por `quadro()` para o
  espaço que a Disposição dá (largura toda, ou a parte no lado a lado; altura que sobra).
- Disposições: a Página vira um contêiner flex (coluna ou linha, invertido conforme `disp`).
  `wonly`/`aonly` escondem a outra Área. Abaixo do mínimo, lado a lado vira um em cima do outro.
- Celular (`innerWidth < 640`): Área compacta de 2 colunas; com `mobileSplit`, Página com widgets e
  apps vira duas telas (a de apps é Página de continuação).
- Área de apps com Medida: `am` fixa → colunas × linhas por tela, ícones encolhem até ~44 px (o
  "Tamanho dos ícones" vira o máximo), abaixo disso compacto; `auto` = o cálculo de hoje
  (`perPage`/`pagePer`).
- "Páginas na tela inicial" desligado: Páginas uma embaixo da outra, cada uma com a sua Disposição.

### Formatos com Tamanho livre (`widgets.js`)
- Trocar as tabelas por letra (`{ s:…, w:…, t:…, l:…, f:… }`, `DIMS`, `cellsOf`, `per`, `cap7`, tamanho
  da rosca, altura do mini-gráfico, relógio) por regras de `w`/`h` (e a proporção em px).
- `FORMATS[*].min = [w, h]` (ex.: Gráfico de linha 2×1). `normalize()` corrige Tamanho abaixo do mínimo.
- Prints de cada Formato em 1×1, 2×1, 1×2, 2×2, 3×1, 1×3, 3×2, 4×2, 6×1 e 6×4.

### Arrastar e soltar (`app.js`, troca o arrastar nativo)
- Pointer events (mouse e dedo iguais): o widget levanta (escala ~1,04, sombra) e segue o ponteiro com
  `transform`; sombra encaixada na grade; vizinhos animados (FLIP, ~200 ms; instantâneo com "Reduzir
  animações"); vermelho quando `dragPlan` dá `null`. Beira da tela muda de Página (650 ms, como hoje).
- Alça do canto: mesmo `dragPlan` com `w`/`h` novos, limitada ao mínimo do Formato e à Medida.
- "Desfazer" num aviso por alguns segundos (guarda o arranjo anterior).
- Celular: segurar o dedo (~500 ms) entra no modo Organizar; o menu do widget vai para o botão "⋯" no
  modo Organizar.
- Ícones dos apps: o mesmo motor em fila (os outros escorregam); soltar em cima de outro app continua
  criando pasta; beira da tela muda de Página.

### Controles
- Configurações → Aparência → **"Páginas da tela inicial"**: abas das Páginas (começa na aberta), 6
  desenhos de Disposição, seletor de Medida dos widgets (6 × 4) e de apps (16 × 6, + "Automático"),
  chave do celular. Seletor: quadradinhos, acende do canto até o mouse com "4 × 3", clique aplica;
  Medida menor que não comporta os widgets → confirma antes ("2 widgets vão para a Página 3").
- Modo Organizar: a barra de cada Página troca "− N colunas +" por dois botões (Disposição e Medida)
  que abrem os mesmos seletores num balãozinho.
- Tamanho do widget: o mesmo seletor de quadradinhos na janela Widgets (no lugar dos 5 botões) e no
  menu do widget, limitado à Medida e ao mínimo do Formato.

### Testes da 0.13.0
- `node --test tests/layout.test.mjs` (encaixe, troca, vermelho, compacto, migração de cada Tamanho
  antigo, "não coube" → Página nova).
- Prints com Playwright no modo demonstração em 1920×1080, 1366×768, 768×1024 e 390×844: cada
  Disposição, Medidas extremas, arrastar (antes/durante/depois), celular com e sem `mobileSplit`.
- `go test`, `apitest.py` e `smoke.sh` como sempre (o backend só guarda as preferências).
- Conferir no modo demonstração que preferências antigas (v2) migram e ficam com a mesma cara.
