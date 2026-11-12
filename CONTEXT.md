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
Quantas colunas por quantas linhas tem uma Área: em Quadros na Área de widgets (até 6 × 4), em ícones
na Área de apps (até 16 × 6, ou Automática: quantos couberem). É o máximo: linhas vazias no fim não
ocupam espaço na tela. Telas estreitas demais mostram a Área compacta (menos colunas, na ordem de
leitura) sem mudar a Medida guardada.
_Evite_: grade, conformação, tamanho (Tamanho é de um Widget)

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
apps de uma Página que também tem widgets; conta como a mesma Página.
_Evite_: página extra, página automática

**App solto**:
App sem Página escolhida (por exemplo, recém-instalado no Runtipi); fica na última Página que tem algum
app colocado.
_Evite_: app sem lugar, app órfão

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
