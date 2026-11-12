# Decisões técnicas (e por quê)

Formato: decisão, motivo, consequência. Ao mudar alguma, registre aqui com a data.

1. **Painel próprio em vez de Homarr + CSS** (11/2026). O Homarr é pesado e o visual do umbrelOS exige
   controle total de layout/animações. → Projeto próprio, leve, fiel ao umbrelOS.
2. **Go só com stdlib no backend.** Binário estático pequeno, pouca RAM, sem runtime. → Imagem `scratch`
   de ~4 MB, ~10 MiB de RAM. Exceção: `golang.org/x/crypto/ssh` para o terminal (decisão 10).
3. **Frontend sem framework e sem build.** Menos peso, nada a compilar, embutido no binário. →
   Módulos ES, CSS puro, ícones SVG inline. Bibliotecas grandes só sob demanda (xterm.js).
4. **Senhas em PBKDF2-SHA256 (600 mil iterações) do pacote `crypto/pbkdf2`.** Não exige dependência
   externa (bcrypt/argon2 estão no x/crypto). Detalhes em `docs/privado/seguranca.md`.
5. **Docker por socket-proxy (`wollomatic/socket-proxy:1`) com lista de rotas.** O socket do Docker dá
   root no host; o proxy só libera listar containers, stats e reiniciar, e só para o container
   `brunos`. Reiniciar só se `BRUNOS_DOCKER_ACTIONS=true`.
6. **Arquivos com `os.Root`** (Go 1.24) para leituras; escritas validam o caminho real (`realDir`).
   Motivo: bloquear fuga por `..` e links simbólicos. O Go 1.24 não tem `Root.Rename`, por isso a
   validação manual.
7. **Discos mapeados pelo usuário** (0.2.0). O Bruno tem pastas em `/mnt` sem disco montado. → Seção
   Discos com "Mapear"; sem configuração, mostra só pontos de montagem reais.
8. **SSE em vez de WebSocket** (estatísticas e terminal). Stdlib pura, passa pelo Traefik sem
   configuração, reconecta sozinho. Teclas do terminal vão por POST com lote de 8 ms.
9. **Publicação pela versão do arquivo `VERSION`** (0.1.0). O ambiente do Claude na nuvem não consegue
   criar tags no GitHub (o proxy bloqueia). → CI publica quando `VERSION` traz versão que não existe no
   GHCR. Tags `v*` criadas pelo Bruno também publicam.
10. **Terminal por SSH, embutido** (0.3.0; o Bruno escolheu entre: SSH embutido, atalho para o Sshwifty,
    shell root no host). Mantém o container sem privilégios. TOFU da chave do servidor, nada de segredo
    gravado. Usa `golang.org/x/crypto` **vendorizado** (o proxy do Go está bloqueado no ambiente da
    nuvem; o código veio do espelho oficial `github.com/golang/crypto` e o CI compara com o sum.golang.org).
11. **Ações privilegiadas via SSH com chave restrita** (implementado na 0.4.0 para o desligamento).
    Desligamento seguro, SMART, limite de carga: uma chave com `command="..."` no `authorized_keys` que
    só executa o necessário. Nunca dar `privileged` ao container. O Bruno escolheu um **usuário
    dedicado** (`brunos-power`, sem senha, `usermod -p '*'`) em vez do usuário dele. O teste de conexão
    nunca abre sessão (abrir executaria o comando forçado); o CI prova isso com OpenSSH real.
12. **Detalhes de segurança fora do README** (pedido do Bruno, commit `1713d35`). Ficam em
    `docs/privado/seguranca.md`, fora do git.
13. **Modo demonstração no frontend.** Sem backend, `api.js` simula tudo. Serve para testar visual com
    Playwright e para prévias.
14. **Configurações no servidor** (`/api/settings`), não no `localStorage`: iguais em todos os
    dispositivos. Papel de parede "local" (arrastado sem enviar) é a única exceção.
15. **Formato da loja: `docker-compose.json` dinâmico do Runtipi** validado pelo `@runtipi/common` (zod).
    Pegadinhas em `runtipi-e-loja.md`.
16. **Plano em etapas, uma versão por etapa** (07/11/2026, escolha do Bruno). Nada que envolva o
    celular por enquanto (C2, A3, F6 adiados). Plano em `roadmap.md`.
17. **Avisos da central pelo mesmo SSE das estatísticas** (0.4.0). O navegador limita conexões por
    host e o terminal já usa SSE; um `event: notify` a mais no stream existente não custa nada. A cada
    reconexão (`init`) a interface relê a lista, então nada se perde.
18. **Desligamento seguro começa em modo simulação** (0.4.0) e só sai dele depois de um teste de
    conexão que passou para o mesmo host/porta/usuário. `shutdown -h +1` deixa 1 minuto para cancelar
    no servidor (`sudo shutdown -c`).
    **Revisto na 0.8.1:** no Ubuntu 26.04 (systemd 259) uma trava "block" vale até para o root, e o
    unattended-upgrades segura uma de `shutdown` o tempo todo; o `shutdown -h +1` era recusado e a
    bateria acabou em 07/11/2026. Agora o comando forçado é `systemctl poweroff --check-inhibitors=no
    --when=+1min` (testado no servidor do Bruno; `sudo shutdown -c` continua cancelando). Numa queda de
    energia, passar por cima da trava é mais seguro que deixar a bateria acabar. O BrunOS também não
    desiste mais depois de 3 tentativas: tenta a cada minuto enquanto estiver na bateria.
    O comando forçado virou o script `brunos-host` (desligar + limite de carga), com **chave nova**:
    com `command=` forçado, qualquer sessão roda o comando, então mandar "limite 80" a um servidor ainda
    com a configuração antiga o desligaria. A chave nova é recusada por servidor não atualizado já na
    autenticação, e o teste de conexão (que só autentica) prova a atualização.
19. **Temperatura de HD sem acordar o disco** (0.5.0). O `drivetemp` manda um comando ao disco a cada
    leitura e, em alguns modelos, isso acorda um HD parado (spin down). Por isso o `readHwmon` (a cada
    2 s) ignora o `drivetemp`, e o HD só é medido de novo quando o contador de E/S
    (`/sys/block/sdX/stat`) mudou; parado, mostra a última medida com o horário. Carregar o módulo
    exige root: a interface só mostra os comandos (`modprobe` + `/etc/modules-load.d`), em vez de usar
    o SSH restrito.
20. **"Lugar" do login novo = rede, não IP exato** (0.5.0). Qualquer IP local/Tailscale conta como
    "rede local" (o DHCP troca IPs); na internet, a faixa /24 (IPv4) ou /48 (IPv6), para não avisar a
    cada troca de IP do provedor. Celular no 4G pode mudar de faixa e gerar avisos; se incomodar,
    afrouxar para /16.
21. **Etapa 3 adiada, F3 junto** (08/11/2026, pedido do Bruno). O link público de compartilhamento
    abre arquivos para fora de casa e foi planejado para depois da verificação em dois passos; adiado
    com ela. As etapas seguintes foram renumeradas (a antiga 0.7.0 virou 0.6.0, e assim por diante).
22. **Editor com CodeMirror 6 vendorizado** (0.6.0, escolha do Bruno entre ele e um editor leve feito à
    mão). Como o frontend não tem build, o CodeMirror vem pronto num arquivo só
    (`web/static/vendor/codemirror/codemirror.mjs`, ~650 KB, ~230 KB gzip), gerado por `tools/codemirror`
    (versões fixas, esbuild) e carregado só quando o editor abre. Para atualizar: `npm ci && npm run build`
    lá dentro. Saiu o SQL das linguagens para economizar.
23. **Tipos de mídia próprios** (0.6.0). A imagem `scratch` não tem `/etc/mime.types`, e o `mime` do Go
    só conhece imagens e arquivos da web; `.mp4` e `.mp3` saíam como `application/octet-stream`.
24. **Desinstalar pelo painel do Runtipi** (0.7.0, escolha do Bruno). Quem sabe o que está instalado é
    o banco do Runtipi; apagar containers pelo Docker deixaria o Runtipi achando que o app existe. A
    alternativa (runtipi-cli por SSH com um usuário restrito e um script que valida o comando) foi
    oferecida e recusada por ora: o botão abre `/apps/<loja>/<app>`, rota conferida em
    `packages/frontend/src/routes.ts` do Runtipi.
25. **Ligar/parar pelo Docker** (0.7.0, escolha do Bruno). Funciona sem nada novo no servidor, mas o
    Runtipi não fica sabendo (o app volta ao estado dele quando o Runtipi/servidor reinicia); a interface
    avisa na confirmação. O proxy do Docker passou a liberar `GET containers/<id>/logs` e
    `POST containers/<id>/(start|stop)`; o BrunOS só aceita containers do projeto do app pedido.
26. **Monitor de disponibilidade sem guardar em disco** (0.7.0). 24 h × 1 ponto/minuto por app em
    memória (alguns KB); reiniciar o BrunOS zera o gráfico. Avisa só depois de 2 minutos seguidos fora
    do ar, e app parado de propósito não conta como queda.
27. **Internet medida por TCP, não por ping** (0.8.0). ICMP exigiria `CAP_NET_RAW` no container. Abrir
    uma conexão na porta 443 de três provedores grandes (vale o mais rápido) mede a latência e só acusa
    queda quando nenhum responde por 2 minutos.
28. **`/etc/passwd` montado só leitura** (0.8.0, escolha do Bruno) para mostrar o dono dos processos. O
    arquivo só tem nomes de usuário (as senhas ficam no `/etc/shadow`, que continua fora).
29. **Speedtest Tracker adiado para a 0.9.0** (escolha do Bruno). A API atual exige um token; a forma de
    guardar tokens de apps será decidida uma vez só, junto com Immich/AdGuard/Plex/Tailscale.
30. **Limite de carga detectado uma vez por início** (0.8.1, pedido do Bruno: "não tem pra que sempre
    verificar [...] em um notebook que não permite"). Detectar custa ler poucos arquivos; fazer isso a
    cada início (e não gravar "sem suporte" para sempre) é o que permite trocar de notebook sem mexer em
    nada. Sem suporte, o vigia não faz mais nada até reiniciar. Com suporte e um limite pedido, relê um
    arquivo a cada 10 min (alguns notebooks esquecem o limite ao reiniciar).
31. **Widgets = Integração → Métricas → Widget com Formatos** (etapa 7, escolhas do Bruno em
    08/11/2026; termos em `CONTEXT.md`). Cada Tipo de integração traduz a API do app em Métricas com
    nome em pt-BR; o Widget não conhece o app, só preenche os Espaços de um Formato (Destaque, Grade,
    Lista, Gráfico, Mídia, Atalhos) com Métricas de qualquer Fonte. Os widgets prontos são Modelos.
    Alternativas recusadas: um catálogo fixo por app (não mistura apps num widget) e um widget de "API
    crua" com caminho JSON (exige conhecer a API). Formatos, e não blocos livres, porque cada Formato é
    desenhado para os 5 Tamanhos e sempre fica bonito.
32. **Coleta híbrida das integrações** (etapa 7). O servidor só busca Métricas que algum Widget usa e só
    enquanto há alguém conectado ao fluxo ao vivo; uma busca serve todas as abas. Gráficos usam o
    histórico do próprio app quando ele tem (AdGuard, Speedtest). Só é coletado em segundo plano (a
    cada 5 min) o que estiver num gráfico sem histórico próprio ou tiver um Aviso pronto ligado. Assim o
    gasto cresce só com o que o Bruno escolhe, e com a tela fechada ele é zero.
33. **Credenciais das integrações cifradas com chave em arquivo** (etapa 7, escolha do Bruno). AES-GCM
    (biblioteca padrão) com chave aleatória em `/data/secret.key` (0600) e os dados em
    `/data/integrations.json`. Protege contra vazar só o arquivo das integrações (cópia, backup
    parcial); quem leva a pasta `/data` inteira já teria também a chave SSH do desligamento. Recusado:
    chave derivada da senha do BrunOS, que pararia a coleta e os avisos depois de reiniciar até alguém
    entrar. As credenciais nunca vão para o navegador (a API responde só `hasSecret`).
34. **Fotos do Immich só em Widget** (etapa 7, escolha do Bruno). O papel de parede com fotos do Immich
    saiu do plano; fica o formato Mídia (tamanho "prévia", foto atual guardada em `/data`). O papel de
    parede por horário (J2) continua. O Tailscale entra por SSH restrito (`tailscale status --json` no
    `brunos-host`), sem token da nuvem.
35. **Grade com colunas fixas + encaixe, Páginas montadas pelo Bruno** (0.9.1, escolhas do Bruno). A
    Área de widgets tem N colunas por Página (ou automático) e os widgets seguem a ordem, fechando os
    buracos (`grid-auto-flow: dense`): um Grande com Pequenos empilhados ao lado funciona sem guardar
    posição exata, e no celular cai para 2 colunas sem quebrar. Recusado: posição exata (linha/coluna),
    que exigiria regras para reorganizar em telas menores. Redimensionar na tela pula só entre os 5
    Tamanhos (cada Formato foi desenhado para eles). Apps ganham Página própria (`appPlace`); os soltos
    vão para a última Página com apps e, antes de mover algo, os soltos são "fixados" onde estão (senão
    colocar um app na Página 2 levaria todos). A numeração se compacta quando uma Página fica vazia.
    Arrastar widgets vale sempre, como os ícones; alça de tamanho e "−" só no modo Organizar.
36. **Script do servidor ganha versão própria** (0.9.2). O pedido `tailscale` (que roda
    `tailscale status --json`) entrou no `brunos-host` como versão 3 (`ScriptVersion`), **sem** subir o
    `SetupVersion`: este decide qual chave usar, e subi-lo faria os servidores já configurados tentarem
    desligar com a chave antiga, que eles recusam. Servidor com o script 2 continua desligando e só
    responde "pedido inválido" ao Tailscale, que vira a mensagem "copie os comandos de atualização". A
    leitura usa `Runner.Output` (saída inteira até 1 MB) e o main só deixa passar esse pedido.
37. **Mídia e avisos sem estado no BrunOS** (0.9.2, escolhas do Bruno). As fotos do Immich vêm numa
    lista do dia (até 24) e a troca acontece no navegador pelo relógio (padrão 30 min, ajustável): todas
    as abas mostram a mesma foto e o servidor só baixa cada imagem uma vez (tamanho "prévia", cache de
    60 imagens em `/data/img-cache`). A capa do Plex vem já reduzida pelo próprio Plex. O aviso de
    aparelho novo usa o "conhecido" do WatchYourLAN (um aviso por MAC; marcar como conhecido lá fecha o
    aviso), sem lista própria. O papel de parede por horário é uma lista livre de horários.
38. **Integrações só de leitura, com a credencial mais fraca que serve** (0.9.3). Transmission: usuário e
    senha opcionais, só `session-stats` e `torrent-get` (o id de sessão do RPC fica no cache). Syncthing:
    chave da API, só GETs em `/rest`. Nextcloud: preferimos o token do app serverinfo (`NC-Token`, só
    leitura) a usuário administrador + senha de app. Vaultwarden ficou de fora (escolha do Bruno): o que
    interessa exigiria o ADMIN_TOKEN, que administra o cofre de senhas.
39. **Bloqueio vale no servidor, por Sessão** (0.10.0, escolhas do Bruno). No modo com senha, a
    Sessão ganha a marca "bloqueada" e o middleware recusa toda rota autenticada (423) menos o
    desbloqueio, o logout e o estado da sessão: só esconder a tela seria burlável pelo console do
    navegador, e o painel tem Terminal e Arquivos. Cada Sessão bloqueia sozinha (a inatividade é de um
    aparelho); o modo só visual existe porque sai quase de graça. A Tela de bloqueio com senha não
    mostra nada que venha da API (clima, avisos) para não abrir exceções.
40. **Chave do Terminal própria, trancada pela senha do BrunOS** (0.10.0). Recusados: guardar a chave
    pessoal do Bruno (ela sairia do computador dele) e cifrar com o `secret.key` (quem copiar o `/data`
    entra no servidor). A chave AES vem da senha (PBKDF2, sal próprio) e só existe na memória da
    Sessão desbloqueada; reiniciar o container ou bloquear com senha a apaga. Esquecer a senha do BrunOS
    perde a chave (basta criar outra). Conexões salvas são lista desde já, mas a interface mostra só a
    principal enquanto houver uma.
41. **"Ainda está rodando?" por um canal SSH extra** (0.10.0). Na mesma conexão, um canal `exec` roda
    um `ps` curto: o pai desse comando é o mesmo processo do sshd da Aba, então o shell interativo é o
    irmão com terminal, e o grupo em primeiro plano do terminal diz se há algo rodando. Roda só ao
    fechar, no fim do tempo sem ninguém olhando e a cada 1 min com Aba minimizada rodando algo — nada
    contínuo, nada instalado no servidor. Falhou (não é Linux, sem `ps`) → "não sei" → pergunta.
42. **Medidor com Automático e tomada local** (0.11.0, escolhas do Bruno). O consumo tem várias
    origens possíveis e o Bruno escolhe (ou deixa no Automático: tomada → bateria → estimativa). Na
    queda de energia a tomada lê 0 W e o Automático passa para a bateria. A tomada Tuya é lida pelo
    protocolo local (sem nuvem: privado, rápido, funciona sem internet); a chave local é tirada uma vez
    com o tinytuya e fica cifrada como as outras credenciais. O RAPL só lê o processador e exige root
    desde 2020 (ataque Platypus): vai pelo script, e só quando o Medidor precisa; não deixamos o arquivo
    legível para todos. A estimativa calibrada aprende o "resto da máquina" durante as quedas.
43. **SMART e temperatura dos discos USB pelo smartctl, sem Scrutiny** (0.11.0). O Scrutiny seria outro
    container com banco (100–200 MB) para dados que mudam pouco. `smartctl -n standby` não acorda HD
    parado; adaptadores USB (ASM225 e similares) não deixam o drivetemp passar, mas o `-d sat` costuma
    passar, então a temperatura deles vem do SMART a cada 20 min.
44. **Atualizações (apps e Linux) só avisam** (0.11.0). Apps: a cópia instalada (`apps/`) contra a loja
    baixada (`repos/`), pelo `tipi_version`, sem tocar no Postgres do Runtipi. Linux: `apt-check` e
    `reboot-required` pelo script. O BrunOS não atualiza nada sozinho: abre o Runtipi ou digita o
    comando no Terminal para o Bruno confirmar.
45. **Preferências protegidas em três camadas** (0.11.1, depois do reset da 0.11.0). (1) A tela nunca
    entra com o padrão nem salva antes de ler as preferências do servidor (tenta de novo e, se não der,
    para com erro). (2) O servidor recusa (409) gravar preferências sem `welcomed` por cima de
    preferências com `welcomed` — o "Restaurar aparência padrão" de propósito mantém `welcomed: true`.
    (3) Uma cópia por dia, 14 dias, em `/data/settings-backup`, restaurável na interface. Uma cópia por
    dia (e não por salvamento) porque a interface salva a cada mudança pequena; 14 cópias de ~20 KB.
    Tomada Tuya: uma conexão por vez por tomada (fila + leitura reaproveitada), sem conexão persistente
    por enquanto (mais simples; reavaliar se a fila não bastar).
46. **SMART um disco por vez, SATA primeiro nos discos USB e "basta uma vez"** (0.11.2, combinado em
    11/11/2026; corrige a 43). No servidor do Bruno, o case JMicron JMS583 (chip de NVMe com um HD SATA
    dentro) fazia o `smartctl` adivinhar NVMe: o adaptador não respondia, o kernel reiniciava o USB do
    disco montado (`uas_eh_device_reset_handler`) e a leitura de todos os discos estourava os 2 min
    ("context deadline exceeded"). Com `-d sat` o mesmo disco respondeu em 2,5 s. O chip do case não diz
    o que tem dentro e o `vpd_pg89` só aparece nos discos internos, então: disco USB tenta SATA; se
    recusar na hora, o automático (que conhece as pontes NVMe); o modo que funcionou fica guardado pelo
    número de série. Travou uma vez → "sem SMART" e nunca mais é lido sozinho (cada tentativa pode
    reiniciar o USB de um disco montado); "Tentar de novo" testa o outro modo. Um limite de tempo por
    disco não interrompe o `smartctl` (ele fica preso esperando o kernel), por isso a regra é não
    insistir, e não esperar mais.
47. **Atualizar Linux e apps pela interface** (0.12.0, escolha do Bruno: opção A; substitui a 44). Pedidos
    fixos novos no script do servidor: `apt-get update && upgrade` sem perguntas (mantém os arquivos de
    configuração) e `runtipi-cli app update <app>:<loja>` com nome validado. Rodam pelo `systemd-run`,
    soltos da conexão SSH: se a atualização reiniciar o Docker (e o BrunOS junto), ela continua, e o
    BrunOS retoma o andamento quando volta. Sempre com confirmação, um de cada vez, aviso no fim;
    reiniciar o servidor continua manual. O risco somado é pequeno (quem tem a senha do BrunOS já
    desliga o servidor e abre o Terminal); fica anotada uma revisão de segurança do script para o futuro.
48. **Widgets com posição livre, apps em fila, e as duas Áreas nunca misturadas** (0.13.0, combinado em
    11/11/2026). Cada Página tem uma Disposição (onde ficam a Área de widgets e a Área de apps) e uma
    Medida para cada Área. Widgets guardam lugar (coluna, linha) e Tamanho (largura × altura em
    Quadros), com buracos permitidos, porque o Bruno quer montar painéis com liberdade (ex.: um 2×1 no
    meio da última linha). Apps continuam em fila (ordem + Página): buraco numa grade de ícones parece
    erro e a fila deixa o arrastar simples. Misturar ícones e widgets numa grade só (como no iPhone)
    foi descartado: obrigaria o ícone a ser uma fração exata do Quadro (≈110 px × 240 px), complicaria
    o arrastar e bagunçaria o celular. Em tela estreita a Área fica compacta (ordem de leitura, menos
    colunas) sem mudar o que está guardado.
