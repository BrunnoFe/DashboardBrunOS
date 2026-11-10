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
Quanto um Widget ocupa na Área de widgets: Pequeno, Largo, Alto, Grande ou Faixa.
_Evite_: dimensão, span

**Quadro**:
A unidade de Tamanho (o tamanho de um Widget Pequeno).
_Evite_: célula, unidade de grade

**Área de widgets**:
O espaço de Widgets no topo de uma Página, separado dos ícones dos apps; tem um número de colunas
(em Quadros) escolhido para a Página ou automático.
_Evite_: fileira de widgets, board, painel

## Tela inicial

**Página**:
Uma tela da tela inicial, montada pelo Bruno, com a sua Área de widgets e os apps colocados nela; pode
ser só de widgets, só de apps ou mista.
_Evite_: aba, tela, desktop

**Página de continuação**:
Página criada sozinha logo depois de uma Página cujos apps não cabem na tela; conta como a mesma Página.
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
