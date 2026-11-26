# Roadmap

O que está **combinado e ainda não terminou**. O que já foi entregue está resumido em `historico.md`; o
que foi adiado ou sugerido sem data, em `ideias-futuras.md`; os porquês das escolhas, em `decisoes.md`.

Como se trabalha: uma versão por etapa, e o Bruno testa no servidor antes da próxima. Nada de privilégios
no container: o que precisa de root no host vai por SSH com chave restrita. Testar sempre também em tela de
celular.

## Estado atual (versão no arquivo `VERSION`)

- Entregue e testada no servidor: tudo até a 0.28.0 (o Bruno confirmou em 21/11/2026 que todos os testes
  pendentes passaram, inclusive a tomada Tuya).

## Plano de 26/11/2026 (combinado com o Bruno, duas versões; decisão 74)

Testar tudo também em tela de celular (390 px), com prints.

1. **0.38.0: correções e polimento** (lançada em 26/11, aguardando teste do Bruno):
   - celular: cada tela da tela inicial rola sozinha; saudação fixa no topo de todas;
   - Arquivos: cabeçalho da lista com a cor da janela; Janela com o nome da pasta da Guia ativa (todas);
     arrastar a Guia para fora mostra uma miniatura que segue o mouse e a Janela nova cresce onde soltou
     (no celular, só "Mover para nova janela", com a Janela deslizando);
   - Bateria: ícone de estado (tomada / carregando / na bateria) ao lado da % e texto curto;
   - Modelos em abas por app (BrunOS · cada Integração · Em branco);
   - Torrents: Densidade Confortável/Compacta, Colunas escolhidas (mais Enviado, Baixado, Falta, Pares,
     Adicionado em), "Ordenar por" e pausar/retomar um torrent na linha aberta.
2. **0.39.0: janela Integrações + dock configurável** (lançada em 26/11, aguardando teste do Bruno): janela do sistema com cartões do que cada
   Integração libera e sugestões dos apps instalados; abre pelo dock (computador e celular), menu do app,
   Widgets, Configurações e Ctrl+K; aviso no sino uma vez por app compatível instalado. Dock: escolher os
   ícones do sistema (a mesma escolha em todos os aparelhos; Configurações sempre fica).

## Plano de 25/11/2026, parte 2 (combinado com o Bruno, quatro versões; decisão 73)

Testar tudo também em tela de celular (390 px), com prints.

1. **0.34.0: vídeo mudo + Ações + AdGuard** (lançada em 25/11 e testada pelo Bruno; os três ajustes do
   escudo foram para a 0.34.1, lançada em 26/11, aguardando teste: canto esquerdo, verde no fim da pausa,
   lembrete no "Até eu ligar"):
   - player: o servidor lê o cabeçalho do .mkv/.mp4/.webm e devolve os formatos de vídeo e áudio; áudio que
     o navegador não toca (AC3, E-AC3, DTS, TrueHD...) mostra um aviso com Baixar e Abrir no Plex (procura
     pelo nome; sem resultado, o botão não aparece); formatos também em Propriedades;
   - Ações de integração (base comum) e escudinho do AdGuard na barra do topo: verde protegendo, pausado com
     o tempo que falta; menu 5 min / 30 min / 1 h / Até eu ligar, sem confirmação; só com Integração AdGuard.
2. **0.35.0: widget do Transmission** (lançada em 26/11, aguardando teste; junto, pedidos do Bruno: a tela
   de atualização avisar quando o Docker não baixa a imagem e a página recarregar de verdade na versão nova):
   Formato de tabela (nome, barra de progresso, tamanho, download,
   upload, estado); todos os torrents com filtro Todos / Baixando / Semeando; rodapé com velocidades
   totais, razão geral, pausar/retomar todos e modo tartaruga; a cada 5 s só com alguém olhando; no
   celular nome + barra + velocidade, o resto ao tocar.
3. **0.36.0: GPU** (lançada em 26/11, aguardando teste; estudo de aceleração em `ideias-futuras.md`: não
   vale hoje): Métricas da fonte BrunOS (Intel: frequência, uso estimado pelo RC6, consumo pelo
   RAPL "uncore" via script, temperatura; AMD: uso, VRAM, temperatura, consumo; NVIDIA pelo `nvidia-smi`
   via script, sem teste real), Modelo "GPU", seção no monitor de hardware, histórico de 30 dias. Junto:
   estudo de aceleração por GPU no BrunOS (parecer para o Bruno, sem código).
4. **0.37.0: Nextcloud por WebDAV** (lançada e testada pelo Bruno em 26/11: "Deu certo!"; fora da 1ª versão: zip, busca,
   editor, extrair, duplicados, mapa do espaço e envio em pedaços com retomada): usuário + senha de app na Integração Nextcloud ligam o lugar
   "Nextcloud" na janela Arquivos: navegar, ver (vídeo com avançar/voltar), baixar, enviar, criar pasta,
   renomear, mover, apagar (lixeira do Nextcloud) e copiar/mover entre o Nextcloud e os discos.

## Plano de 25/11/2026 (combinado com o Bruno, duas versões)

1. **0.32.0: correções** (lançada e testada pelo Bruno em 25/11: "Tudo certo!"):
   - Arquivos: cabeçalho da lista fixo com fundo (as linhas passavam por cima dele ao rolar);
   - Arquivos: caminho em linha própria embaixo dos botões, recolhendo o meio em "…" (com menu) quando não
     cabe; a barra se adapta à largura da janela (container query): Pasta/Enviar só ícone, busca vira lupa;
   - Atualizações: o painel não mudava depois de atualizar (a interface parava de ler o servidor antes de
     ele terminar de conferir de novo). Tirar o app da lista ao terminar bem e continuar lendo enquanto o
     servidor confere.
2. **0.33.0: atualizações mais bonitas e escala** (decisões 70 a 72; lançada e testada pelo Bruno em 25/11:
   "Testei a 0.33.0 e tudo funcionou"):
   - Tela de atualização: ideia do Bruno depois dos protótipos, a logo em "órbita" (o arco gira e encolhe, a
     bolinha dá a volta, o centro pulsa) dentro de um anel que mostra o progresso das etapas, com um brilho
     girando; etapas em pílulas embaixo (Backup · Baixando · Reiniciando · Voltando);
   - anel de progresso no ícone do app que está atualizando;
   - Novidades na primeira abertura de cada versão (e em Configurações → Sobre);
   - Escala da Tela inicial e do Sistema, por aparelho, em Configurações → Aparência.

## Plano de 24/11/2026 (combinado com o Bruno, três versões)

1. **0.29.0: fechamentos** (lançada em 24/11, testada):
   - processos com o comando mascarado (`--password=••••`, `TOKEN=••••`, `usuario:••••@`, chaves longas) e
     botão "ver completo" que pede a senha do BrunOS (V13 da revisão; rota `GET /api/processes/cmd`);
   - imagem do proxy do Docker congelada (versão + digest), e o `smoke.sh` lê a mesma do compose;
   - saiu a conversão das preferências antigas dos widgets (`widgets2` v1/v2 → v3); instalação nova monta
     os widgets direto no formato atual;
   - branches antigas apagadas; Tuya fora das pendências (funcionando).
2. **0.30.0: Docker por socket e trava na chave** (lançada em 24/11, testada; decisão 68):
   - o BrunOS e o proxy do Docker conversam por um arquivo de socket compartilhado só entre os dois, em vez
     da rede (nenhum outro container fala com o proxy);
   - `from=` na chave do script: o próprio script descobre a faixa da rede do BrunOS ao instalar/atualizar a
     chave (e regrava se o Runtipi recriar a rede); comando de socorro em `docs/privado/`;
   - testar antes com o `smoke.sh` (Docker real); se falhar no servidor, voltar para a 0.29.0 pelo Runtipi.
3. **0.31.0: Assistente de tomadas Tuya** (lançada em 24/11, aguardando teste do Bruno; decisão 69):
   - nuvem da Tuya só para configurar: Access ID + Access Secret + região guardados cifrados, e usar pede a
     senha do BrunOS (5 min); as leituras continuam só pela rede de casa;
   - lista as tomadas da conta com nome, ID e chave local; busca na rede de casa (porta 6668 na faixa da
     rede) e testa a chave de cada tomada para achar o IP e a versão; mostra o consumo de agora para conferir;
   - tomada já cadastrada (mesmo ID): "Atualizar" no mesmo cadastro (widgets, Medidor e histórico seguem);
     nova: "Adicionar"; IP mudou: avisa, procura de novo e pede confirmação com senha (a chave continua presa
     ao IP); passo a passo em pt-BR para criar o projeto na Tuya, com os erros comuns (região, ligar o app,
     plano de desenvolvedor vencido) e a sugestão de reservar o IP no roteador;
   - teste do Bruno: "Atualizar" a tomada atual (deve dizer "tudo certo") e "Adicionar como nova" com o
     nome "Tomada (teste)", apagando depois; conferir que o histórico da Energia continua.

Depois das três: conversa sobre **admin em casa / convidado fora** (pelo domínio público só convidado; em
casa ou pelo Tailscale, admin com senha nas ações perigosas) e **funções novas pelo script do servidor**
(rede, sensores, discos, Tailscale, tarefas agendadas, registros). Sem root no BrunOS e sem substituir o
Runtipi (conversa de 24/11/2026).

## Melhorias de 18/11/2026 (combinadas com o Bruno, três versões)

1. **0.24.0: Ícones** (lançada e testada pelo Bruno em 18/11): bug do link colado no campo (virava um nome
   impossível) e do `onerror` inline barrado pela CSP (atalho sem nem a inicial); campo Catálogo aceita
   nome ou link, com prévia; Enviar arquivo (svg/png/ico/jpg, até 1 MB); Ícones guardados no servidor
   (decisão 63).
2. **0.25.0: Energia e avisos** (lançada em 18/11; o teste achou que cancelar não funcionava, corrigido na
   0.25.1, testada pelo Bruno em 19/11; decisão 64): botão de energia no canto (depois do sino) com Bloquear, Sair, Reiniciar e
   Desligar; os mesmos botões ao lado de "Sair" nas Configurações; Desligar/Reiniciar pedem a senha do
   BrunOS e esperam 1 min com Cancelar no sino; sem chave SSH, levam à janela Energia; pedido novo
   `reiniciar` no script (troca assinada, sem colar nada); "Reiniciar agora" no painel de Atualizações
   quando o Ubuntu pedir. Avisos de nível Aviso/Importante expandem no painel (explicação, o que fazer,
   dados e botão de atalho); clicar no balão abre o sino no aviso.
3. **0.26.0: Formas de acesso** (lançada em 19/11, testada; decisão 65): o Automático segue a Forma de acesso usada para abrir o BrunOS
   (`brunos.server.lan` → `https://app.server.lan`; `IP:porta` ou `server.lan:porta` → mesmo nome com a
   porta do app), com troca para a outra se a escolhida não funcionar; o vigia testa a porta (já faz) e, a
   cada ~10 min, se o domínio responde pelo Traefik; as duas formas com bolinha verde/cinza no botão
   direito ("Abrir por…"), em Personalizar e no Monitor de apps.

## Revisão de segurança de 19/11/2026 (0.27.0, lançada em 19/11, testada pelo Bruno em 20/11)

Os detalhes ficam no relatório privado. O que entrou:
- Go 1.27 e `x/crypto` atualizado; `govulncheck` no CI.
- Limite de tentativas de senha mais firme (inclusive na troca de senha) e prazos contra conexões lentas.
- Lixeira, logos e configurações das lojas não seguem atalhos; integrações com credencial presa ao
  endereço e destinos internos proibidos.
- Loja: selo "Requer privilégios" mais rigoroso; adicionar loja passa a pedir a Senha de configuração
  (o Bruno tinha dito em 17/11 que não precisava; entrou no lote aprovado em 19/11) e instalar sem senha
  só de loja confiável (script do servidor 18).
- Regras do proxy do Docker mais estreitas; CI com actions fixadas e assinatura num job separado.
- Interface: escapes, sandbox na Janela de app, demonstração só onde não há servidor e dados genéricos.

## Revisão de segurança, lotes 3 e 4 (0.28.0, lançada em 20/11/2026, testada)

Escolhas do Bruno em 19/11: ocultos da Home só leitura; segredos somem do Arquivos (pasta de dados do
BrunOS, app.env dos apps, .env do Runtipi, chaves privadas de .ssh); código da aba + Terminal (a Chave do
Terminal pede a senha ao abrir o Terminal, não no login); senha de novo nas ações destrutivas (5 min); IP
só do Traefik; Abas do Terminal por Sessão; nomes de endereço aceitos (Configurações → Sistema → Endereços
aceitos). Depois de atualizar, todo mundo precisa entrar de novo uma vez.

O que ficou de fora dos lotes (socket do proxy, `from=` na chave, V13) entrou no plano de 24/11 acima.

## Em aberto

- Senha também para parar app e para remover lojas: o Bruno disse que não precisa por enquanto
  (17/11/2026). Adicionar loja passou a pedir na revisão de segurança de 19/11.
- Itens do `ideias-futuras.md` que o Bruno pedir passam para cá, com versão.

## Como lançar uma versão

Está no `CLAUDE.md` (seção "Como lançar uma versão").
