# Visão geral do BrunOS

## Origem e objetivo

O Bruno usava o **Homarr** com um CSS customizado para imitar o **umbrelOS** no servidor caseiro dele
(Ubuntu Server + Runtipi). Ele pediu um painel próprio que:

- reproduzisse a interface do umbrelOS: vidro, ícones, sombras, fonte Inter, papel de parede, dock com
  os apps principais, muita personalização ("algo fofo, legal, bonito");
- desse acesso aos apps do Runtipi, gerenciamento de arquivos, configurações gerais e uso de hardware;
- fosse **leve** ("é fundamental que essa interface seja relativamente leve");
- tivesse **usuário e senha próprios** criados na primeira execução, com credenciais bem guardadas;
- lesse **todas as pastas e discos possíveis, mas com segurança**.

A inspiração visual veio do código do Umbrel (`getumbrel/umbrel`, `packages/ui`) e de um vídeo que o
Bruno mandou mostrando a interface. Valores de sombras, curvas de animação e a ampliação do dock foram
adaptados de lá; os papéis de parede são os do Umbrel (licença PolyForm Noncommercial, uso pessoal).

## Princípios

1. Leveza: binário Go estático sobre `scratch`, ~4 MB de imagem, ~10 MiB de RAM.
2. Segurança sem privilégios: container restrito, proxy do Docker com lista de rotas, nada de root.
3. Fidelidade visual ao umbrelOS, com tema derivado do papel de parede.
4. Tudo em português do Brasil.
5. Configurações salvas no servidor (iguais em qualquer dispositivo).
6. Testes automáticos com Docker de verdade antes de publicar qualquer imagem.

## Funcionalidades

A lista atual está no `README.md` (seções por assunto). Resumo: tela inicial com papel de parede, widgets
personalizáveis com integrações, dock, busca `Ctrl+K`, gerenciador de arquivos completo, monitor de
hardware e de apps, central de notificações, energia e desligamento seguro, terminal SSH, tela de
bloqueio, atualizações pela interface. O que falta: `roadmap.md` e `ideias-futuras.md`.
