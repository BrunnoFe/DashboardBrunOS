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
