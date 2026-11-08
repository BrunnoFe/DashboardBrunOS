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
O espaço de Widgets no topo de uma página da tela inicial, separado dos ícones dos apps.
_Evite_: fileira de widgets, board, painel

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
