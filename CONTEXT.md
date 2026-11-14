# BrunOS

Painel leve para o servidor caseiro do Bruno, no estilo do umbrelOS. Este glossário fixa as palavras
usadas na interface, no código e nas conversas.

## Widgets

**Widget**:
Peça da tela inicial que mostra uma ou mais Métricas, com Formato, Tamanho e textos escolhidos pelo
Bruno.
_Evite_: card, painel, bloco

**Modelo**:
Widget pronto, já preenchido, que serve de ponto de partida para um Widget novo.
_Evite_: template, widget padrão, predefinição

**Formato**:
O arranjo interno de um Widget: Destaque, Grade, Lista, Gráfico, Mídia ou Atalhos.
_Evite_: layout, tipo de widget, estilo

**Espaço**:
Lugar de um Formato que recebe uma Métrica, com rótulo, unidade, ícone e cores próprios.
_Evite_: slot, campo, célula

**Tamanho**:
Quanto um Widget ocupa na Área de widgets: largura × altura em Quadros, de 1 × 1 até a Medida da Área.
Alguns têm nome: Pequeno (1 × 1), Largo (2 × 1), Alto (1 × 2) e Grande (2 × 2). Cada Formato tem um
Tamanho mínimo.
_Evite_: dimensão, span, Faixa (virou só um Widget com a largura da Área)

**Quadro**:
A unidade de Tamanho (o tamanho de um Widget Pequeno).
_Evite_: célula, unidade de grade

**Área de widgets**:
O espaço de Widgets de uma Página, separado da Área de apps; onde ela fica na Página depende da
Disposição. É uma grade de Quadros com a Medida escolhida; cada Widget fica num lugar fixo dela (pode
sobrar Quadro vazio) e dois Widgets nunca dividem um Quadro.
_Evite_: fileira de widgets, board, painel

**Área de apps**:
O espaço dos ícones dos apps (e das pastas) de uma Página, com a sua própria Medida.
_Evite_: grade de apps, grade de ícones (Grade é um Formato)

**Medida**:
Quantas colunas por quantas linhas tem uma Área: em Quadros na Área de widgets (até 8 × 5), em ícones
na Área de apps (até 20 × 8, ou Automática: quantos couberem). É o máximo: linhas vazias no fim não
ocupam espaço na tela. Telas estreitas demais (ou, na Área de apps, um Tamanho do ícone grande demais)
mostram a Área compacta (menos colunas, na ordem de leitura) sem mudar a Medida guardada.
_Evite_: grade, conformação, tamanho (Tamanho é de um Widget)

**Área útil**:
O retângulo de cada tela onde ficam a Área de widgets e a Área de apps: com margem dos lados, de baixo da
saudação até em cima da pesquisa. Tudo de uma tela cabe nele; o Quadro cresce ou diminui (dentro de uma
faixa) para a grade da Medida encher a largura dele.
_Evite_: tela útil, espaço livre, viewport

**Régua**:
A largura que a Área de widgets teria com 6 colunas naquela tela, sem passar da Área útil, seja qual for
a Medida da Página. Os Tamanhos do ícone e o Espaço entre os ícones são medidos por ela; muda só quando a
tela muda.
_Evite_: largura da tela, largura útil

**Tamanho do ícone**:
Pequeno, Médio, Grande ou Enorme, o mesmo em todas as Páginas. O Grande é o maior em que 16 colunas de
apps ainda cabem na Régua; os outros são proporções dele (o Enorme não deixa caber 16 colunas). No
computador e na TV, ícones e widgets crescem e encolhem juntos (escala); só no celular cada tamanho tem
um mínimo fixo em px.
_Evite_: zoom, escala, tamanho (sozinho, que é de um Widget)

**Espaço entre os ícones**:
Como a Área de apps usa a largura: Fixo (ícone e espaço constantes, área centralizada, o padrão),
Elástico (o espaço estica até um limite para alinhar com a Régua e depois centraliza) ou Preencher
(sempre de ponta a ponta na Régua).
_Evite_: espaçamento, gap, margem

## Tela inicial

**Página**:
Uma tela da tela inicial, montada pelo Bruno, com a sua Área de widgets e os apps colocados nela,
arrumados conforme a Disposição dela.
_Evite_: aba, tela, desktop

**Disposição**:
Como uma Página arruma a Área de widgets e os apps: widgets em cima, apps em cima, widgets à esquerda,
widgets à direita, só widgets ou só apps. Cada Página tem a sua. Widgets e apps nunca se misturam na
mesma área.
_Evite_: layout (o arranjo de dentro de um Widget é o Formato), estilo, tema, modelo de página

**Página de continuação**:
Tela criada sozinha logo depois de uma Página cujos apps não cabem na tela, ou, no celular, a tela com os
apps de uma Página que também tem widgets; conta como a mesma Página. Não repete os widgets e usa a Área
útil inteira (as colunas da Medida, com quantas fileiras couberem); só é criada quando a anterior está
cheia.
_Evite_: página extra, página automática

**App solto**:
App sem Página escolhida (por exemplo, recém-instalado no Runtipi); fica na última Página que tem algum
app colocado.
_Evite_: app sem lugar, app órfão

## Janelas

**Janela**:
Qualquer quadro que abre por cima da tela inicial (Arquivos, Discos, um app...).
_Evite_: modal, popup, diálogo (diálogo é a pergunta curta de confirmar)

**Estilo das janelas**:
Como as Janelas se comportam no computador: Flutuante ou Folha. No celular e em telas estreitas as duas
ocupam a tela inteira.
_Evite_: modo de janela, tipo de janela

**Flutuante**:
Estilo em que a Janela pode ser arrastada e maximizada, e várias ficam abertas ao mesmo tempo, uma por
cima da outra (a última tocada fica na frente).
_Evite_: livre, solta, janela do Windows

**Folha**:
Estilo em que a Janela fica centralizada, uma por vez, com o fundo escurecido.
_Evite_: sheet, painel, modal

**Janela de app**:
Janela que mostra um app (do Runtipi ou um link) dentro do BrunOS, sem abrir outra aba.
_Evite_: navegador embutido, iframe, visualizador

**Minimizar**:
Esconder uma Janela sem fechá-la: o que está nela continua carregado e ela fica na dock até voltar.
Fechar descarta.
_Evite_: ocultar, esconder, suspender

**Só em aba**:
App que não aceita ser mostrado dentro de outra página; sempre abre em nova aba, mesmo com "Abrir apps
em: No BrunOS".
_Evite_: app bloqueado, app incompatível

## Dados

**Fonte**:
Quem fornece Métricas: cada Integração e o próprio servidor (a fonte "BrunOS").
_Evite_: provedor, origem

**Métrica**:
Um valor com nome em português oferecido por uma Fonte ("AdGuard › Consultas hoje"); pode ser número,
texto, lista ou série no tempo.
_Evite_: dado, campo, indicador, stat

**Tipo de integração**:
Um app compatível que o BrunOS sabe ler (AdGuard, Plex, Immich, Speedtest Tracker...).
_Evite_: conector, plugin, provider

**Integração**:
Um serviço de um Tipo de integração, com nome próprio, endereço e credencial; pode haver várias do
mesmo tipo ("AdGuard do Pi").
_Evite_: conexão, app conectado, conta

**Aviso pronto**:
Alerta para o sino que um Tipo de integração oferece, que o Bruno liga, desliga e ajusta (ex.:
"aparelho novo na rede").
_Evite_: regra, alerta personalizado, gatilho

**Histórico**:
Os valores passados de uma Métrica que muda o tempo todo, guardados no servidor por até 30 dias (mais
detalhados quanto mais recentes); sobrevive ao reinício do BrunOS.
_Evite_: log, registro, banco de dados

**Período**:
Quanto tempo um gráfico mostra: 5 min (ao vivo), 1 h, 24 h, 48 h, 7 dias ou 30 dias. Cada gráfico tem o
seu.
_Evite_: intervalo, janela de tempo, range

**Ponta viva**:
A bolinha com o valor atual no fim de um gráfico que se mexe na frente do Bruno (Período ao vivo); os
gráficos de Períodos longos não têm.
_Evite_: cursor, marcador

## Discos

**Disco**:
Um aparelho de armazenamento ligado ao servidor (HD, SSD ou NVMe, interno ou USB), reconhecido pelo
número de série; as letras (sda, sdb) são só o nome do momento.
_Evite_: drive, volume, HD (sozinho, quando pode ser SSD)

**Pasta de montagem**:
Onde o conteúdo de um Disco aparece no servidor, sempre dentro de /mnt. Um Disco sem Pasta de montagem
está conectado mas não montado.
_Evite_: ponto de montagem, mount, caminho

**Formatar**:
Apagar um Disco inteiro e deixá-lo com uma única partição vazia (ext4, Btrfs, NTFS ou exFAT).
_Evite_: limpar, zerar, inicializar

**Senha de formatação**:
Senha própria, criada uma vez pelo Terminal com sudo, que o servidor confere antes de Formatar;
diferente da senha do BrunOS.
_Evite_: senha de admin, senha do sudo, PIN

## Energia

**Medidor**:
De onde vem o consumo do servidor (em watts): Automático, uma tomada inteligente, a bateria
descarregando, o processador (RAPL) ou a estimativa calibrada. O custo em R$ é calculado sobre ele.
_Evite_: fonte (que é quem fornece Métricas), sensor, origem

**Estimativa calibrada**:
Consumo do servidor inteiro calculado pelo processador mais o "resto da máquina" que o BrunOS aprende
comparando com a bateria quando ela está descarregando.
_Evite_: consumo aproximado, chute

## Acesso

**Sessão**:
Um login do Bruno em um aparelho (navegador); todas as abas desse navegador compartilham a mesma
Sessão.
_Evite_: login, conexão, token

**Bloqueio**:
Estado de uma Sessão em que o painel fica coberto pela Tela de bloqueio; vale só para aquela Sessão.
Pode ser só visual (qualquer toque desbloqueia) ou com senha (nada funciona até digitar a senha).
_Evite_: logout, suspensão, descanso de tela

**Tela de bloqueio**:
A tela com relógio grande sobre o papel de parede mostrada durante o Bloqueio.
_Evite_: lock screen, protetor de tela

## Terminal

**Chave do Terminal**:
Chave SSH criada pelo próprio BrunOS só para o Terminal, guardada trancada pela senha do BrunOS; o
Bruno autoriza essa chave em cada computador onde quer entrar. Diferente da chave usada no
desligamento seguro, que só pode rodar um comando.
_Evite_: minha chave, chave SSH (sozinha), chave do servidor

**Conexão salva**:
Um computador e usuário guardados para o Terminal entrar direto com a Chave do Terminal ("Servidor
(bruno)"); uma delas é a principal.
_Evite_: host, destino, perfil, favorito

**Aba**:
Uma sessão SSH aberta dentro da janela Terminal; várias Abas podem estar abertas ao mesmo tempo.
_Evite_: sessão (sozinha, que é a Sessão do BrunOS), janela

**Comando salvo**:
Um botão do Terminal com um texto de comando que o Bruno guardou para não precisar digitar.
_Evite_: snippet, atalho, macro
