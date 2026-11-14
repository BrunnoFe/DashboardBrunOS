# Plano da etapa 12: Janelas de app (0.17.0) e janelas do sistema (0.18.0)

Combinado com o Bruno em 13/11/2026 (sessão de perguntas). Termos em `CONTEXT.md` (seção Janelas);
porquê em `decisoes.md` (decisão 58). Ao terminar a etapa, este plano vira resumo no `historico.md` e é
apagado.

## 0.17.0: Janelas de app

### O que o Bruno vê

- **Configurações › Apps › "Abrir apps em"**: Nova aba (padrão, como hoje) · Mesma aba · **No BrunOS**.
- **Configurações › Aparência**:
  - "Estilo das janelas": **Flutuante** (padrão) · **Folha**.
  - "Dock com app maximizado": Sempre visível · **Esconde sozinha** (padrão) · App acima da dock.
- **Configurações › Sistema › "Limite de apps abertos"**: computador (padrão 8) e celular (padrão 5).
- Com "No BrunOS", clicar num app (tela inicial, dock, Ctrl+K) abre a **Janela de app**. Ctrl+clique e
  clique do meio continuam abrindo em nova aba. O mesmo app nunca abre duas vezes: clicar de novo traz a
  janela para a frente.
- **Barra da janela**: ícone + nome (arrastar; dois cliques maximizam), ⟳ recarregar, ↗ abrir em nova
  aba (os dois vão para a página inicial do app, porque o navegador não deixa ver a página atual), e à
  direita – ▢ ✕. No celular: barra fina, sem ▢ e sem arrastar.
- **Flutuante** (computador): várias janelas, a última tocada na frente; arrastar pela barra; redimensionar
  por bordas e cantos (mínimo ~360 × 240); não sai da tela (sobra sempre um pedaço da barra); encaixe ao
  arrastar: borda de cima maximiza, esquerda/direita ocupam metade, com sombra mostrando onde vai.
- **Folha** (computador): uma por vez, centralizada, fundo escurecido. Abrir outro app minimiza a atual;
  clicar no fundo minimiza (não fecha). Maximizar existe; arrastar e encaixar, não. Folhas do sistema
  abrem por cima do app, que fica intacto embaixo.
- **Celular e telas estreitas**: os dois estilos viram tela cheia; a dock é o trocador de apps.
- **Maximizada**: ocupa a tela inteira, com cantos arredondados. A dock fica por cima e, conforme a
  opção, fica visível, se esconde sozinha (some depois de ~1 s; volta com o mouse na beirada de baixo,
  ou deslizando de baixo para cima no celular) ou faz o app parar onde ela começa.
- **Dock**: app aberto ganha o pontinho (`open-dot`); aberto e não fixado aparece depois de um segundo
  divisor e some ao fechar. Clique: restaura se minimizado, traz para a frente se atrás, minimiza se já é
  o da frente. Botão direito ou toque longo: "Fechar", junto com o resto do menu do app.
- **Animações** (~200 ms, só `transform`/`opacity`): abrir saindo do ícone clicado; fechar encolhendo;
  minimizar voando até o ícone da dock (e o caminho inverso ao restaurar); maximizar/restaurar
  deslizando. No celular, sobe de baixo e desce para a dock. "Reduzir movimento" (ou o pedido do
  sistema) troca tudo por um esmaecer rápido.
- **Limite**: ao passar do limite, pergunta "Você já tem N apps abertos. Fechar o X?" (X = a minimizada
  usada há mais tempo; sem minimizada, a do fundo).
- **Só em aba**: app que recusa iframe abre sempre em nova aba, sem aviso. Não testado = tenta a janela.
  BrunOS em `https` com app em `http` = Só em aba direto. No menu do app: "Abrir em janela" / "Abrir em
  nova aba" (escolha na hora) e "Sempre abrir em nova aba" (fixo por app). Se o Bruno forçar "Abrir em
  janela" num app Só em aba, a janela explica "Este app não abre dentro do BrunOS" com o botão ↗.
- **Voltar** (celular, mouse, navegador): minimiza a janela da frente (depois de esgotar a navegação de
  dentro do app, que é regra do navegador).
- **F5 / fechar a aba** com Janela de app aberta (mesmo minimizada): o navegador pergunta antes
  (`beforeunload`). Quando o próprio BrunOS vai recarregar (depois de atualizar, reiniciar), avisa antes
  com mensagem própria: "Os apps abertos vão ser recarregados".
- **Lembrado por app e por aparelho** (`localStorage`): posição, tamanho e maximizada. Primeira vez:
  centralizada, ~80 % da tela. Depois do F5, nada reabre sozinho.
- **Bloqueio**: os apps continuam rodando, escondidos (`visibility: hidden`) atrás da Tela de bloqueio.
  **Sair** / Sessão expirada: fecha todas; "Sair" pergunta antes se houver app aberto.
- As janelas do sistema (Arquivos, Terminal, Discos...) continuam como estão nesta versão.

### Backend (Go)

1. **Checagem de iframe aproveitando o vigia** (`apps.go`, `probe`): o GET que já é feito a cada minuto
   para a bolinha de status passa a devolver também os cabeçalhos. Uma função pura nova (ex.:
   `frameVerdict(h http.Header)`) decide:
   - `X-Frame-Options` presente (`DENY`, `SAMEORIGIN`, `ALLOW-FROM` obsoleto) → recusa (o app está em
     outra porta/domínio, então SAMEORIGIN também recusa);
   - CSP `frame-ancestors` sem `*` (ou `'none'`/`'self'`) → recusa;
   - sem nenhum dos dois → aceita.
   Se a raiz redirecionar (302 para `/login`), seguir até 3 redirecionamentos **no mesmo endereço** só
   para a checagem (o vigia continua tratando qualquer resposta como "no ar").
2. Resultado em memória por app (`ok` / `no` / desconhecido), refeito quando o app volta a responder ou
   quando a versão do app muda. Precisa rodar também com o monitor de apps desligado ou com o app fora
   do monitor (uma checagem leve por app a cada poucas horas nesse caso).
3. `GET /api/apps` ganha o campo `frame: "ok" | "no" | ""` por app. Links personalizados: sem checagem
   (`""`).
4. **CSP** (`main.go`, `appCSP`): `frame-src 'self' http: https:`, para permitir os iframes dos apps. O
   resto da CSP continua igual (o BrunOS continua recusando ser embutido: `frame-ancestors 'self'`).
5. Testes: tabela de cabeçalhos para `frameVerdict` (maiúsculas/minúsculas, CSP com várias diretivas,
   `frame-ancestors *`, `'none'`, vários cabeçalhos CSP); `apitest.py` confere o campo `frame` e a CSP.

### Frontend

1. **`web/static/js/windows.js`** (novo, genérico para servir também à 0.18.0): gerenciador de janelas
   com interface pequena: `open({id, title, icon, originEl, mount})`, `focus(id)`, `minimize(id)`,
   `toggleMax(id)`, `close(id)`, `list()`, evento de mudança (para a dock). Cuida de: camada (z-index),
   arrastar/redimensionar/encaixar com Pointer Events (com `pointer-events: none` nos iframes durante o
   gesto, senão o iframe engole o mouse), limites da tela, modos Flutuante/Folha/tela cheia (CSS +
   `matchMedia`), animações (FLIP com `transform`), histórico (`history.pushState` + `popstate` para o
   voltar), memória de posição por app no `localStorage` (com try/catch), limite com pergunta,
   `beforeunload`. Contas puras (limites, encaixe, quem fechar no limite) em funções exportadas e
   testáveis no `node --test`.
2. **`web/static/js/appwindow.js`** (novo): a Janela de app. Monta o iframe (`src` = `appUrl`,
   `referrerpolicy="no-referrer"`, `allow="fullscreen; clipboard-write"`, sem `sandbox`, que quebraria
   login e upload dos apps), a barra, os botões ⟳ e ↗, e a mensagem para Só em aba forçado.
3. **`app.js`**:
   - `openApp(a, {forceTab, forceWindow})` decide: Só em aba / "Sempre abrir em nova aba" / `https` com
     app em `http` / `openIn` ≠ "No BrunOS" → aba (ou mesma aba), como hoje; senão, janela.
   - Os `<a class="app">` mantêm o `href` (Ctrl+clique e clique do meio funcionam); o clique normal é
     interceptado só no modo "No BrunOS".
   - Dock: apps abertos não fixados depois de um segundo divisor; pontinho; regra do clique; "Fechar".
   - Dock esconde sozinha: classe no `body` com janela maximizada; faixa invisível de poucos px na
     beirada de baixo (por cima do iframe) traz a dock de volta.
   - Configurações: as três opções novas e as preferências `openIn: 'brunos'`, `winStyle`,
     `dockWithMax`, `winLimit: {pc: 8, mobile: 5}`; por app: `o.forceTab`.
   - Menu do app: "Abrir em janela" / "Abrir em nova aba" / "Sempre abrir em nova aba".
   - Bloqueio esconde as janelas; Sair e Sessão expirada fecham todas (perguntando no Sair).
   - Aviso próprio antes dos recarregamentos feitos pelo BrunOS.
4. **CSS** (`extra.css`): janela no estilo do BrunOS (vidro, cantos, sombra, botões redondos já usados),
   sombra do encaixe, modos Folha e tela cheia, dock escondida, `prefers-reduced-motion`.
5. **Modo demonstração** (`api.js`): apps de exemplo com `frame: "ok"` apontando para uma página local
   de exemplo (sem endereço real) e um com `frame: "no"`, para testar sem servidor.

### Testes e verificação

- `go vet`, `go test -race ./...`, `apitest.py`, `layout.test.mjs` + teste novo das contas do
  `windows.js`, `smoke.sh` (confere a CSP nova no container real).
- Playwright no modo demonstração, computador e celular (390 px): abrir, arrastar, encaixar, maximizar
  com as três opções da dock, minimizar para a dock, limite com pergunta, Folha, voltar, Reduzir
  movimento. Screenshots para o Bruno.
- No servidor do Bruno: conferir quais apps reais viram Só em aba e se os demais funcionam (login,
  WebSocket, upload) dentro da janela.

## 0.18.0: janelas do sistema (depois do teste da 0.17.0)

Arquivos, Terminal, Discos, Energia, Widgets e Configurações passam a usar o mesmo `windows.js` e a
seguir o "Estilo das janelas". Pontos a combinar com o Bruno antes de começar (nova rodada de perguntas):

- se todas migram ou alguma continua sempre Folha (Configurações?); diálogos de confirmação continuam
  sempre modais;
- o Terminal troca o seu "minimizar" próprio pelo comum (as Abas continuam rodando no servidor);
- tamanho inicial de cada janela do sistema e o que acontece com os atalhos de hoje (Esc fecha a folha);
- abrir a mesma janela do sistema duas vezes (ex.: dois Arquivos lado a lado) ou não;
- a opção "Limite de apps abertos" passa a contar as janelas do sistema ou não.
