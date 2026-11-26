# Histórico de versões e problemas resolvidos

Resumo do que cada versão trouxe e das lições que ainda valem. Detalhes de código: `arquitetura.md`;
porquês: `decisoes.md`; o que falta: `roadmap.md` e `ideias-futuras.md`. Detalhes de acesso ao servidor
e de segurança ficam em `docs/privado/` (não versionado). Textos mais longos de cada versão, antes da
limpeza de 18/11/2026, estão no git (`git log -- docs/projeto/historico.md`).

## v0 e 0.1.0 (07/11/2026): primeira versão no Runtipi
Protótipo estático inspirado no umbrelOS; depois o backend Go (auth, sessões, preferências, papéis de
parede, estatísticas por SSE, apps do Runtipi, Docker por proxy, Arquivos com lixeira), CI com teste de
fumaça em Docker real, imagem no GHCR e app na loja. O Bruno confirmou: "Funcionou no Runtipi!"
- Lições: fuga por link simbólico em upload/colar (escritas validam o caminho real); lista de discos vazia
  no runner do CI (nome do disco pelo `st_dev`); ícone sem logo vira gradiente; a nuvem não cria tags, a
  publicação é pelo arquivo `VERSION`.

## 0.2.0 a 0.4.0 (07/11)
- 0.2.0: bolinha de status, seção **Discos** com "Mapear", widget e janela de **bateria**, "Apps no dock".
- 0.3.0: terminal SSH (xterm.js, senha ou chave, TOFU, SSE, `x/crypto` vendorizado). Lição: a biblioteca
  preferia a chave ECDSA; passou a seguir a ordem do OpenSSH (ED25519 primeiro). Senha errada = recusa de
  login, não erro genérico.
- 0.4.0: sino (`internal/events`), quedas de energia (`internal/power`), **desligamento seguro** pela
  bateria (simulação por padrão, SSH com chave restrita), aviso de disco mapeado desmontado, TOFU
  compartilhado (`internal/sshknown`). Lição: `bytes.Buffer` compartilhado entre stdout e stderr do SSH
  perdia saída.

## 0.5.0 a 0.7.0 (08/11)
- 0.5.0: aviso de acesso novo (navegador+sistema+rede), temperatura dos discos (`drivetemp`/`nvme`, sem
  acordar HD), previsão de disco cheio **opcional**.
- 0.6.0: Arquivos: tipos de mídia próprios (a `scratch` não tem `/etc/mime.types`), editor CodeMirror 6,
  envio com retomada, extrair zip/tar, duplicados, mapa do espaço. Segurança etapa 3 adiada.
- 0.7.0: Detalhes do app (disponibilidade 24 h, CPU/memória, logs ao vivo, ligar/parar/reiniciar),
  desinstalar pelo painel do Runtipi, monitor de disponibilidade com aviso, pastas e páginas na tela inicial.

## 0.8.0 e 0.8.1 (08/11): monitoramento; correções depois do primeiro apagão de verdade
- 0.8.0: histórico de 24 h/7 dias, processos do servidor (como o htop), internet (latência TCP e quedas).
- 0.8.1: **o desligamento seguro falhava** num Ubuntu novo (trava do systemd valia até para o root).
  Corrigido com um script no servidor que ignora a trava, tentativas sem limite na bateria e desligamento
  imediato se a bateria despencar. Duração das quedas fecha no último minuto visto. Limite de carga
  agnóstico ao notebook.

## 0.9.0 a 0.9.3 (08/11): widgets personalizáveis e integrações
Janela Widgets (Modelos, Formatos, gráficos, prévia), Área de widgets por Página, colunas com encaixe,
alça de tamanho, Páginas montadas pelo Bruno, Arquivos personalizável. Integrações (credenciais cifradas,
nunca vão ao navegador): AdGuard, Speedtest Tracker, Immich, Plex, WatchYourLAN, Tailscale, Transmission,
Syncthing, Nextcloud. Papel de parede por horário. Lição: apagar uma integração durante uma busca dela
derrubava o BrunOS (nil pointer).

## 0.10.0 e 0.10.1 (10/11): bloqueio, relógio, clima, notas, Terminal com abas
Tela de bloqueio (com senha valendo no servidor, ou só visual), widgets Relógio, Clima (Open-Meteo) e
Notas, Terminal com Abas, Chave do Terminal, Conexões e Comandos salvos. Lições: o login montado atrás da
Tela de bloqueio roubava o foco; a `scratch` não tinha certificados de autoridades (todo HTTPS para fora
falhava com `x509`; a imagem leva `ca-certificates.crt`); listas vazias vão como `[]`, não `null`.

## 0.11.0 a 0.11.2 (10 e 11/11): consumo, SMART, atualizações; preferências protegidas
- 0.11.0: consumo e custo (Medidor, Tomada Tuya local), saúde dos discos (SMART pelo script), aviso de
  atualização de apps e do Linux.
- 0.11.1: **as preferências zeravam** quando a leitura de `/api/settings` falhava (a tela seguia com o
  padrão e salvava por cima). Agora a tela nunca entra nem salva sem ler, o servidor recusa gravar o padrão
  por cima (409) e guarda uma cópia por dia (14 dias). Tomada Tuya: fila por tomada.
- 0.11.2: o SMART estourava o tempo por causa de certos adaptadores USB; agora lê um disco por vez, tenta
  SATA primeiro nos USB, guarda o modo pelo número de série e marca "sem SMART" quando um disco trava.

## 0.12.0 a 0.12.3 (12/11): atualizar pela interface
- 0.12.0: botão de atualizações ao lado do sino; atualizar Linux e apps sem abrir o terminal (roda solto
  no servidor, o BrunOS retoma o andamento); Terminal sempre no dock. Bug antigo: as rotinas de consumo,
  SMART e atualizações nunca eram ligadas.
- 0.12.1: o script do servidor se atualiza sozinho (só com assinatura do CI); backup do Runtipi antes de
  atualizar app; `apt` tolerante a repositório extra quebrado.
- 0.12.2: o andamento de atualizações compridas perdia a primeira linha do registro e concluía "não
  começou". Pegadinha: um segredo salvo pelo PowerShell veio com CRLF e o `ssh-keygen` recusou; o CI limpa
  CR/BOM antes de assinar.
- 0.12.3: "5 atualizações que não somem" eram pacotes segurados pela liberação gradual do Ubuntu; agora
  contam à parte. Lojas do Runtipi atualizadas antes de conferir os apps.

## Etapa 11 (0.13.0 → 0.16.0, 12 e 13/11): tela inicial nova, Históricos, janela Discos
- 0.13.0: widgets com lugar e Tamanho livres numa grade de Quadros, Disposições, Área de apps com Medida
  própria, arrastar feito pelo BrunOS, celular com Área compacta. Encaixe em `web/static/js/layout.js`
  (funções puras) com `tests/layout.test.mjs`. Lições: `justify-content: center` antigo centralizava as telas
  na vertical; grade `auto-fill` alargava a tela (`min-width: 0`); ouvir os eventos de soltar na janela
  inteira, senão o fantasma do arraste ficava preso.
- 0.13.1: tamanho do ícone em 4 degraus pela Régua + Espaço entre ícones (Fixo, Elástico, Preencher). A
  Régua depende do zoom do Quadro, que só se conhece depois de desenhar os widgets: usa a do anterior e
  refaz se mudou.
- 0.13.2: **Área útil** (margem ~7 %), Quadro estica de 150 a 340 px, ícones e widgets na mesma escala,
  "Encolher os widgets"/"Widgets grandes", telas de continuação sem widgets. Uma matriz de 252 combinações no
  Playwright achou 7 defeitos. Lições: a Área de apps virou flex (grade não centraliza a última fileira);
  o ícone Grande arredonda para baixo; mouse parado em cima de um ícone e a animação de entrada dão falsos
  "fora do centro" na matriz.
- 0.14.0: `internal/tsdb` (Históricos de 30 dias em três camadas, com foto a cada 6 h e diário), leitura/
  escrita e % ocupado por disco, gráficos numa peça só (`mountChart`: eixo, balão, Período, Ponta viva).
  Lição: `display: flex/grid` vence o atributo `hidden`; regra `[hidden] { display: none !important }`.
- 0.15.0 e 0.15.1: janela **Discos** (lista, Visão geral, Saúde com SMART explicado e teste do próprio
  disco, Desempenho, Espaço, Linha do tempo, Ejetar); script 10. Teste do disco conferido a cada 30 s com
  andamento; dados reais trocados por genéricos nos testes e na demonstração. Lição: o plano dizia
  `/data/disks.json`, mas ele já guarda os discos mapeados do Arquivos; conferir os nomes em `/data` antes
  de criar um.
- 0.16.0: na janela Discos: **Montar**, **Desmontar**, **Renomear** e o assistente **Formatar** (ext4,
  Btrfs, exFAT, NTFS; digitar o modelo, senha do BrunOS e Senha de formatação conferida pelo servidor, 5
  erros = 1 h de trava; script 11). Depois de montar/desmontar, "Reiniciar o BrunOS agora" (o container só
  vê as montagens de quando ligou). Achado de segurança: o script procura o disco em todos os
  `/proc/*/mountinfo` antes de ejetar ou formatar (decisão 57). Testes: `tests/script-discos.sh` e
  `tests/script-formatar.sh` (script de verdade como root com ferramentas de disco falsas).

## Etapa 12 (0.17.0 e 0.23.x, 13 a 17/11): janelas
- 0.17.0: **Janelas de app** (decisão 58): "Abrir apps em: No BrunOS", iframe direto, Estilo Flutuante
  (arrastar, redimensionar, encaixar) ou Folha, dock com apps abertos, limite de apps abertos (8 no PC, 5 no
  celular), `internal/framecheck` lê os cabeçalhos e o que recusa vira Só em aba. Lições: o encaixe guarda o
  tamanho de antes do arraste; no celular, o toque na faixa da dock cancela o clique que cairia no app.
- 0.23.0: **janelas do sistema** (Arquivos, Discos, Widgets, Configurações, Terminal) no mesmo gerenciador,
  com Folha ou Flutuante; o Arquivos pode abrir uma segunda janela; `openSheet` abre `sys:<tipo>` no
  `windows.js`; o `terminal.js` monta dentro do corpo da janela (`ctx.host`).
- 0.23.1: bug (gif do Bruno): a segunda janela do Arquivos tomava o lugar da primeira (estado global único
  no `files.js`). Agora cada janela/guia tem o próprio estado (`hold`). **Guias no Arquivos** (`filetabs.js`):
  "+", clique do meio, Alt+T/W/1…9 (o Chrome não deixa sites usarem Ctrl+T/W), reordenar, destacar para
  janela própria sem recarregar, guias lembradas neste aparelho.

## Etapa 13 (0.18.0 → 0.20.0, 16 e 17/11): Loja
- 0.18.0: janela **Loja** (decisão 59): apps de todas as lojas do Runtipi lidos do disco, categorias, busca,
  favoritos, página do app; parar/iniciar/desinstalar e adicionar/remover lojas pelo script (versão 12).
  Lições: lojas são de terceiros (sem symlink nem HTML); mudar o `location.hash` minimiza a janela da frente;
  o `/tmp` do WSL some quando ele reinicia.
- 0.19.0: **Garimpo** (decisão 60): repositório `BrunnoFe/Garimpo`, conversor diário (CasaOS e
  LinuxServer → lojas do Runtipi, um ramo por origem); na Loja, cartão por loja e selo "via CasaOS". A
  Senha de formatação virou **Senha de configuração** (script 13). Lições: a LinuxServer põe o prefixo
  `linuxserver-` nos nomes; os testes oficiais precisam de `bun install --ignore-scripts`; o GitHub barrou
  um `SECRET_KEY` fixo com cara de chave da OpenAI (o conversor troca segredo fixo por campo `random`);
  no PowerShell, comandos bash longos vão em arquivo `.sh`.
- 0.20.0: **instalar e Ajustes dentro do BrunOS** (decisão 61): formulário na Loja, mandado à API do Runtipi
  pelo script (script 14), Ajustes no `user-config` editados com `yaml.v3`, Senha de configuração em
  Configurações → Sistema. Lições: o Runtipi 4.12 trocou a chave do CLI (agora dentro do container); chave e
  token não vão na linha de comando; o arquivo de ajustes do Runtipi não tem nenhuma conferência.

## Etapa 14 (0.21.0 → 0.22.0, 17/11): janela de instalação completa
- 0.21.0: **janela única** de instalar e configurar (seções que abrem e fecham, "?" com a explicação,
  Acesso como no Runtipi, Ajustes já na instalação); **Requer privilégios** (decisão 62): selo, guia "Apps
  com Privilégios", "Entendo os riscos" + Senha de configuração; script 15; Garimpo publica esses apps na
  loja normal e põe perguntas de instalação por regra + `perguntas.json`. Conversão real: CasaOS 107 apps (21
  com privilégios), LinuxServer 198 (10); 40 dos 271 apps da loja oficial do Runtipi ganharam o selo.
  Lições: a régua do BrunOS (Go) e a do Garimpo (Python) precisam dar o mesmo texto (conferidas nos 305
  apps); caminho com `${VAR:-padrão}` tem ":" dentro da variável; o Runtipi ignora `maxBackups` na instalação
  e não apaga o `user-config` ao desinstalar.
- 0.21.1: teste do Bruno: o AnythingLLM ficou instável logo depois de instalar (a página já mostrava
  "Iniciar" com a imagem ainda baixando; pedidos de ligar recriaram o container e o Docker o matou no meio da
  primeira migração). Agora os botões ficam travados enquanto o Runtipi instala/atualiza (409), o erro da
  fila interna sai em português e Detalhes do app mostra "Logs de antes" (o Runtipi recria o container a
  cada "ligar"). Remédio para app estragado: desinstalar e instalar de novo.
- 0.21.2: "Instalando…" na Loja, anel no sino e "Em andamento" no painel; **Memória compartilhada** nos
  Ajustes (o Docker dá só 64 MB de `/dev/shm`, e navegadores Chromium, como o Brave, travam); o Garimpo põe
  `shm_size: 1gb` nos apps de área de trabalho. O aviso "--no-sandbox" no Brave é normal no container.
- 0.22.0: **Avançado**: editor do arquivo de Ajustes (CodeMirror sob demanda), conferido antes de salvar
  (`appcfg.CheckRaw`); privilégios novos só por ele, com a lista, "Entendo os riscos" + Senha de
  configuração, aviso no sino e selo no app.

## Melhorias de 18/11 (0.24.0 →)
- 0.24.0: **ícones**. O print do Bruno (link do jsDelivr colado "todo bugado", atalho sem ícone) juntava três
  bugs: o campo apagava tudo que não fosse letra/número/hífen; o `onerror` escrito no HTML nunca rodava
  (a CSP `script-src 'self'` barra script no próprio HTML, então a imagem quebrada ficava); e a inicial
  branca sumia no fundo branco. Agora: Catálogo aceita nome ou link, com prévia; **Enviar** arquivo (até
  1 MB); **Ícones guardados** em `/data/icons` (decisão 63), com o CDN como reserva. Lição: o jsDelivr
  responde 403, e não 404, para svg que não existe.
- 0.25.0: **menu de energia** (botão depois do sino: Bloquear, Sair, Reiniciar, Desligar), os mesmos botões
  nas Configurações e na busca, "Reiniciar agora" nas Atualizações. Reiniciar/Desligar pedem a senha e o
  script 16 agenda para +1 min (`reiniciar`, `cancelar-desligar`), com contagem e Cancelar (decisão 64).
  **Detalhes dos avisos** Aviso/Importante no sino e balão clicável ("Ver detalhes"). Lição: o teste que
  cruza os avisos do Go com os textos achou o `os-reboot`, que eu não tinha listado.
- 0.25.1: teste do Bruno: **não deu para cancelar o reinício** ("chave recusada") e o servidor reiniciou.
  O `systemctl --when=+1min` faz o logind criar o `/run/nologin` e o PAM recusa qualquer login de usuário
  comum até desligar (no registro do ssh: "Access denied ... by PAM account configuration"). Script 17:
  `reiniciar` e o novo `desligar-agendado` esperam num temporizador do systemd (`brunos-energia.timer`),
  que não bloqueia logins; cancelar para o temporizador. Lição: o teste com `systemctl` falso não pega
  efeito colateral do systemd de verdade; conferir num systemd real (deu para fazer no WSL com `--user`).
- 0.26.0: **Formas de acesso** (decisão 65). O Automático segue o endereço usado para abrir o BrunOS
  (sem porta e com nome = Domínio; `IP:porta` ou `server.lan:porta` = Porta) e troca para a outra forma
  quando a escolhida não respondeu. Teste novo do Domínio (`internal/access`, a cada 10 min) direto no
  Traefik pelo IP, sem depender do DNS do container; bolinhas verde/cinza no botão direito, Personalizar,
  Detalhes do app e lista de apps do Uso ao vivo. Regra em `js/access.js`, com teste; o CI passou a rodar
  todos os testes de JS. Lição: o Traefik responde "404 page not found" para nome desconhecido, o mesmo
  texto do 404 padrão do Go; por isso só esse corpo exato conta como "não roteado".

## 0.27.0 (19/11/2026): revisão de segurança
Auditoria completa (relatório só local, em `docs/privado/`) e correções dos lotes aprovados pelo Bruno: Go
1.27 e `x/crypto` novo com `govulncheck` no CI, limite de senha e prazos de conexão, atalhos (links
simbólicos) recusados na lixeira e nos arquivos das lojas, credencial das integrações presa ao endereço,
selo "Requer privilégios" mais rigoroso, script do servidor 18, proxy do Docker mais estreito, CI com actions
fixadas por hash e interface com escapes e sandbox. Lições:
- O Go 1.24 saiu do suporte quando o 1.26 foi lançado: o `govulncheck` agora barra o CI se aparecer falha
  conhecida, e a versão do Go precisa subir junto.
- `go run` dentro da VM do Docker Desktop falha com "permission denied" porque o `/tmp` dela não executa
  programas: usar `GOTMPDIR` fora do `/tmp`.
- Prazo de leitura no corpo do pedido: tirar o prazo só quando o corpo termina (EOF). Tirar também em erro
  deixava o servidor esperando para sempre o resto de um corpo que nunca chega.
- O proxy do Docker (wollomatic) já põe `^...$` sozinho nas regras.

## 0.28.0 (20/11/2026): revisão de segurança, lotes 3 e 4
Arquivos: ocultos da pasta pessoal só leitura; pasta de dados do BrunOS, `app.env` dos apps, `.env` do Runtipi
e chaves privadas de `.ssh` somem do Arquivos. Sessão: código da aba além do cookie (todos entram de novo uma
vez), a Chave do Terminal pede a senha ao abrir o Terminal, senha de novo nas ações destrutivas (5 min), Abas
do Terminal por Sessão, `X-Forwarded-For` só do Traefik e nomes de endereço aceitos (Configurações →
Sistema). Decisões 66 e 67. Lições:
- O cookie vai para qualquer porta do mesmo IP; o `localStorage` não. Por isso o código fica no
  `localStorage` e vai no cabeçalho (e em `?k=` só nos GET que o navegador faz sozinho).
- Consulta de DNS dentro de um cadeado precisa de prazo: um nome que não existe na rede travaria os pedidos.
- O pacote runtipi só lê o domínio local quando existe a pasta `apps` (cuidado ao montar testes).

## 0.29.0 (24/11/2026): fechamentos da revisão de segurança
Processos (V13): a lista mostra o comando com senhas, tokens, `usuario:senha@` e chaves longas trocados por
`••••`; o olho ao lado abre o comando inteiro depois de confirmar a senha do BrunOS. Proxy do Docker
congelado em versão + digest (`smoke.sh` lê a mesma imagem do compose e falha sem digest). Saiu a conversão
das preferências antigas dos widgets (anteriores à 0.13); instalação nova monta os widgets direto no formato
atual. Lições:
- A tag `:1` do proxy pulou para uma versão de 3 dias: congelar numa com pelo menos 2 semanas.
- Mascarar por padrão erra para mais (esconde um caminho de `--key-file`), nunca para menos: por isso o
  "ver completo" com senha, em vez de tentar acertar todos os formatos.

## 0.30.0 (24/11/2026): Docker por socket e chave presa à rede do Docker
O proxy do Docker deixa a rede: fala só por um socket numa pasta montada nele e no BrunOS (dono root, grupo
do BrunOS, 0660). Script do servidor 19 com `trava-chave`: grava `from="<faixa da ponte do Docker>"` na linha
da chave, com o endereço lido do próprio sshd; o BrunOS pede uma vez cada vez que liga. Decisão 68. Lições:
- O servidor vê o IP do container (ponte br-*), não o do roteador: dá para travar sem valor fixo.
- O socket-proxy apaga o socket velho ao ligar e pula o `-allowfrom` quando usa socket.
- Root sem capabilities não cria arquivo em pasta de outro dono: o CI (pasta do runner) pegou o que o teste
  local como root escondia; o proxy ganhou `DAC_OVERRIDE` e o smoke usa pasta de usuário comum.

## 0.31.0 (24/11/2026): Assistente de tomadas Tuya
Widgets → Integrações → Assistente de tomadas: Access ID e Access Secret do projeto da Tuya (cifrados, com
senha), lista das tomadas da conta do app, busca na rede de casa (porta 6668 na faixa do servidor) testando a
chave de cada uma, consumo de agora para conferir, "Atualizar" o cadastro que já existe ou "Adicionar como
nova"; passo a passo do projeto da Tuya com os erros comuns; o aviso de tomada sem responder manda procurar de
novo. Decisão 69. Lições:
- A assinatura da Tuya é a mesma do tinytuya (apiKey + token + t + método, hash, cabeçalhos, caminho); um
  "exemplo da documentação" lembrado de cabeça estava errado: conferir no código de quem já funciona.
- O cliente das integrações recusa 127.0.0.1: nos testes, a nuvem de mentira usa o cliente do httptest.
- Testes de script e o smoke rodam no Windows num contêiner `golang:1.27-bookworm` com o socket do Docker
  Desktop, `-v /tmp:/tmp`, `--network host` e o docker CLI estático.

## 0.32.0 (25/11/2026): Arquivos pela largura da janela e painel de Atualizações em dia
Arquivos: o cabeçalho da lista ganhou fundo de vidro (as linhas apareciam por trás dele ao rolar); o caminho
foi para uma linha própria e recolhe as pastas do meio num "…" com menu quando não cabe (a raiz e a pasta
atual ficam sempre); a barra segue a largura da própria janela (container query `fwin`): gaveta lateral
abaixo de 760 px, Pasta/Enviar só com ícone, e abaixo de 560 px a busca vira lupa e ocultos/lista/grade vão
para o menu "⋯". Atualizações: o que terminou bem sai da lista na hora, a conferência de depois espera outra
que esteja rodando em vez de pular, e a interface segue lendo o servidor enquanto ele confere. Lições:
- Coluna `1fr` de grade não encolhe abaixo do conteúdo: o caminho comprido alargava a janela em vez de
  recolher. `minmax(0, 1fr)` + `min-width: 0`.
- Para recolher o meio antes de cortar a última pasta, medir com a última no tamanho inteiro (classe `.fit`).
- O painel parava de ler o servidor quando via "terminou", mas a conferência nova só vinha depois, e a
  conferência recente (menos de 10 min) impedia conferir de novo ao abrir o painel.

## 0.33.0 (25/11/2026): Tela de atualização, Novidades e Escala
Tela de atualização com a logo em órbita dentro de um anel que mostra as etapas (atualizar o BrunOS, Linux
quando o servidor para de responder, reiniciar/desligar o servidor, reiniciar o BrunOS); recarrega sozinha
quando ele volta. Anel girando no ícone do app que atualiza e ✓ ao terminar. Novidades na primeira abertura
de cada versão (e ao clicar na versão nas Configurações). Escala da Tela inicial e do Sistema, por aparelho,
70–110 % (85 % no celular). Decisões 70 a 72. Lições:
- Só `zoom` na tela inicial não adianta: o encaixe estica tudo de volta. O zoom vai na `.page-body` e as
  contas continuam em px da tela (a composição encolhe); o arrastar desconta a escala (`areaZoom`).
- Elementos posicionados por `left/top` em JS (menus `.ctx`) andam errado com `zoom`: ficaram de fora.
- O anel do ícone vive num `<style>` com o seletor do app: sobrevive aos redesenhos da tela inicial.
- Protótipos injetados na própria página de demonstração (papel de parede e desfoque de verdade) e vídeos
  pelo Playwright (`recordVideo`, precisa do ffmpeg dele) ajudaram o Bruno a escolher (e a misturar dois).

## Pegadinhas que ainda valem
- Janela de app: o clique dentro do app não chega na página do BrunOS (Ctrl+K e atalhos vão para o app); o
  BrunOS usa o `blur` da página para saber que o app foi tocado.
- Classes novas de CSS: conferir se o nome já existe (`.up-row` já era a lista de envios do Arquivos; o
  painel de atualizações usa o prefixo `upd-`).
- Ao mudar JS/CSS, recompilar antes de testar contra o backend (`go:embed`).
- O formulário da loja do Runtipi manda string vazia para campos novos em apps já instalados: tratar vazio
  como padrão.
- Muitas classes CSS são montadas em JS (`wsz-${size}`, `lv-${nivel}`, `disp-${disp}`, `wfm-${formato}`):
  ao procurar CSS morto, um `grep` do nome inteiro não basta.

## Limpeza do repositório (18/11/2026)
Pedido do Bruno: enxugar o que serve de contexto para IA, apagar o que não é mais usado e tirar código
morto. Feito: `deadcode` e `staticcheck` no Go (sobrou só `Runner.Output`, removido; parâmetro `now` não usado
em `power.executeLocked`); no JS, funções exportadas que ninguém usava (`batteryWidgetHTML`,
`internetWidgetHTML`, `paintInternet` e o `sparkPath` dele, `navigate` do Arquivos, `brunosDef`,
`clockTicks`, `toggleMax`, `_test`) e cerca de 30 regras de CSS órfãs (widgets antigos de bateria e
internet, `.w-stats`, `.wsz-ico`...); `roadmap.md`, `decisoes.md` e este arquivo reescritos de forma
compacta (as versões longas estão no git); fatos do Runtipi 4.12 passaram do roadmap para
`runtipi-e-loja.md`.
Mantido de propósito: `staticcheck ST1005` em `apps.go` (mensagens de erro em português com inicial
maiúscula, que vão para a interface).

## Resumo do roadmap antigo (itens feitos, por letra)
A (apps), B (energia), C1 (central de notificações), D (discos), E (apps: logs, ligar/parar, pastas, páginas,
atualizações, backup), F (arquivos), G (monitoramento), H3 (login novo), I1/I3 (WatchYourLAN, Tailscale,
AdGuard), J (aparência: bloqueio, papel de parede por horário, relógio/clima/notas, tela inicial nova) e K
(terminal: comandos salvos, abas, chave própria): todos entregues entre 0.4.0 e 0.13.0. O que não foi feito
está em `ideias-futuras.md`.
