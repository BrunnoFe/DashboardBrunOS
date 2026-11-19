# Decisões técnicas (e por quê)

Formato: decisão, motivo, consequência, em poucas linhas. Ao mudar alguma, registre aqui com a data. A
narrativa de cada versão está em `historico.md`; a versão longa deste arquivo (antes da limpeza de
18/11/2026) está no git (`git log -- docs/projeto/decisoes.md`).

## Base

1. **Painel próprio em vez de Homarr + CSS** (11/2026): o Homarr é pesado e o visual do umbrelOS pede
   controle total de layout. → Projeto leve, fiel ao umbrelOS.
2. **Go só com a biblioteca padrão.** Imagem `scratch` de ~4 MB, ~10 MiB de RAM. Exceções: `x/crypto/ssh`
   (10) e `yaml.v3` (61).
3. **Frontend sem framework e sem build**: módulos ES, CSS puro, ícones SVG inline; bibliotecas grandes
   só sob demanda (xterm.js, CodeMirror).
4. **Senhas em PBKDF2-SHA256** (`crypto/pbkdf2`), sem dependência. Parâmetros em `docs/privado/seguranca.md`.
5. **Docker por socket-proxy** (`wollomatic/socket-proxy:1`) com lista de rotas: o socket dá root no host;
   o proxy só libera listar, stats, logs, iniciar/parar/reiniciar (só do projeto do app pedido; reiniciar
   só com `BRUNOS_DOCKER_ACTIONS=true`).
6. **Arquivos com `os.Root`** (Go 1.24) para leituras; escritas validam o caminho real (`realDir`),
   porque o Go 1.24 não tem `Root.Rename`. Bloqueia fuga por `..` e links simbólicos.
7. **Discos mapeados pelo usuário** (0.2.0): sem configuração, só pontos de montagem reais.
8. **SSE em vez de WebSocket**: stdlib pura, passa pelo Traefik, reconecta sozinho. Teclas do terminal vão
   por POST em lote de 8 ms.
9. **Publicação pelo arquivo `VERSION`**: o CI publica quando a versão não existe no GHCR (o ambiente da
   nuvem não cria tags).
10. **Terminal por SSH embutido** (0.3.0): container sem privilégios, TOFU da chave do servidor, nenhum
    segredo gravado. `x/crypto` **vendorizado** e conferido no CI contra o sum.golang.org.
11. **Ações privilegiadas via SSH com chave restrita** (0.4.0) a um script fixo no servidor (desligar,
    SMART, limite de carga, atualizações...). Nunca `privileged`. O teste de conexão não abre sessão (o CI
    prova com OpenSSH real). Desenho: `docs/privado/seguranca.md`.
12. **Detalhes de segurança fora do repositório público** (pedido do Bruno, commit `1713d35`; reforçado em
    12/11/2026): ficam em `docs/privado/`; docs e exemplos públicos usam nomes genéricos.
13. **Modo demonstração** no frontend (`api.js` simula tudo): testar visual com Playwright sem backend.
14. **Configurações no servidor** (`/api/settings`), iguais em todos os aparelhos. Exceção: papel de
    parede "local" (arrastado, sem enviar).
15. **Formato da loja**: `docker-compose.json` dinâmico do Runtipi, validado pelo `@runtipi/common`.
    Pegadinhas em `runtipi-e-loja.md`.
16. **Uma versão por etapa** (07/11/2026, escolha do Bruno). Nada de celular por enquanto (C2, A3, F6).
17. **Avisos da central pelo mesmo SSE das estatísticas** (`event: notify`): o navegador limita conexões
    por host. A cada reconexão a interface relê a lista.

## Energia, discos, segurança

18. **Desligamento seguro começa em simulação** e só sai dela depois de um teste de conexão; 1 minuto
    para cancelar. **0.8.1:** uma trava (inhibitor) do systemd recusava o desligamento até a bateria
    acabar; o comando passou a ignorá-la, o BrunOS tenta a cada minuto na bateria e tudo virou um script
    no servidor com chave nova (servidor desatualizado nunca recebe pedido que interpretaria errado).
19. **Temperatura de HD sem acordar o disco** (0.5.0): o `drivetemp` acorda HD parado; só mede de novo
    quando o contador de E/S muda. Carregar o módulo exige root: a interface só mostra os comandos.
20. **"Lugar" do login novo = rede, não IP exato** (0.5.0): IP local/Tailscale = "rede local"; na
    internet, /24 (IPv4) ou /48 (IPv6). Se incomodar no 4G, afrouxar para /16.
21. **Etapa 3 (dois passos, sessões ativas, link público) adiada** (08/11/2026, pedido do Bruno). Ver
    `ideias-futuras.md`.
27. **Internet medida por TCP, não ICMP** (0.8.0): ICMP pede `CAP_NET_RAW`. Porta 443 de três provedores
    (vale o mais rápido); queda só depois de 2 minutos sem resposta.
28. **`/etc/passwd` montado só leitura** (0.8.0) para mostrar o dono dos processos (sem senhas).
30. **Limite de carga detectado uma vez por início** (0.8.1): trocar de notebook não exige mexer em nada;
    sem suporte, o vigia não faz nada até reiniciar; com limite pedido, relê um arquivo a cada 10 min.
39. **Bloqueio vale no servidor, por Sessão** (0.10.0): a Sessão ganha a marca "bloqueada" e o middleware
    recusa tudo (423) menos desbloquear, sair e ler o estado; só esconder a tela seria burlável (há
    Terminal e Arquivos). O modo só visual existe porque sai quase de graça.
40. **Chave do Terminal própria, trancada pela senha do BrunOS** (0.10.0). Recusado: guardar a chave
    pessoal do Bruno e cifrar com a chave de arquivo das integrações. Esquecer a senha perde a chave (basta
    criar outra). Detalhes: `docs/privado/seguranca.md`.
41. **"Ainda está rodando?" por um canal SSH extra** (0.10.0): um `ps` curto na mesma conexão, só ao fechar
    e nos intervalos de ociosidade; nada instalado no servidor. Falhou → "não sei" → pergunta.
45. **Preferências protegidas em três camadas** (0.11.1): (1) a tela nunca entra nem salva sem ler do
    servidor; (2) o servidor recusa (409) gravar sem `welcomed` por cima de com `welcomed`; (3) uma cópia
    por dia, 14 dias, em `/data/settings-backup`. Tomada Tuya: uma conexão por vez por tomada.
49. **O script do servidor se atualiza sozinho, só com assinatura do CI** (0.12.1): só instala script
    assinado, de versão maior e sintaxe válida. O preço é confiar no repositório do GitHub. Sem assinatura
    (compilação local), caminho manual pelo Terminal. Teste: `tests/script-selfupdate.sh`.
50. **Backup do Runtipi antes de atualizar app, caixinha sempre marcada** (0.12.1): é o mesmo backup do
    painel do Runtipi; se falhar, nada é atualizado. "Só o essencial" foi descartado (a restauração
    apagaria o que ficou de fora).
51. **Acesso de diagnóstico só de leitura para o Claude** (0.12.1): usuário próprio, chave presa a uma
    lista fixa de leituras. Desenho em `docs/privado/`.
54. **Formatar só com a Senha de configuração conferida pelo servidor** (0.16.0): quem confere é o
    servidor, fora do container, com trava após erros e nova conferência do disco antes de apagar; o
    cadastro é feito uma vez, direto no servidor (um invasor não cadastra a dele). Depois (0.19.0) a senha
    virou "Senha de configuração" e vale para todas as ações sensíveis. Detalhes em `docs/privado/`.

## Apps, widgets, arquivos

22. **Editor com CodeMirror 6 vendorizado** (0.6.0): um arquivo só (`web/static/vendor/codemirror/`,
    ~650 KB), gerado por `tools/codemirror` (esbuild, versões fixas), carregado só ao abrir o editor.
    Atualizar: `npm ci && npm run build` lá dentro.
23. **Tipos de mídia próprios** (0.6.0): a imagem `scratch` não tem `/etc/mime.types`.
24. **Desinstalar pelo painel do Runtipi** (0.7.0): quem sabe o que está instalado é o banco dele.
    Revisto na 59 (c): hoje vai pelo `runtipi-cli` no script.
25. **Ligar/parar pelo Docker** (0.7.0): funciona sem nada novo, mas o Runtipi não fica sabendo; a
    interface avisa. (A Loja, na 59 c, passou a ligar/parar pelo `runtipi-cli`.)
26. **Disponibilidade de apps** (0.7.0): 24 h × 1 ponto/minuto em memória; aviso só depois de 2 minutos
    fora; app parado de propósito não conta. A partir da 0.14.0 os dados vêm do tsdb (53).
29. **Tokens de integrações decididos uma vez só** (etapa 7), junto com Immich/AdGuard/Plex/Tailscale.
31. **Widgets = Integração → Métricas → Widget com Formatos** (etapa 7; termos em `CONTEXT.md`): cada Tipo
    de integração traduz a API em Métricas em pt-BR; o Widget só preenche os Espaços de um Formato
    (Destaque, Grade, Lista, Gráfico, Mídia, Atalhos), cada um desenhado para os 5 Tamanhos. Recusados:
    catálogo fixo por app e "API crua" com caminho JSON.
32. **Coleta híbrida das integrações**: só busca Métricas que algum Widget usa e só com alguém conectado;
    em segundo plano (5 min) só o que está num gráfico sem histórico próprio ou tem Aviso ligado.
33. **Credenciais das integrações cifradas** (AES-GCM, chave aleatória em arquivo 0600) em
    `/data/integrations.json`; nunca vão ao navegador (`hasSecret`). Recusado: chave derivada da senha
    (pararia a coleta depois de reiniciar).
34. **Fotos do Immich só em Widget** (formato Mídia, tamanho "prévia"); Tailscale entra por SSH restrito.
35. **Grade de widgets com encaixe** (0.9.1; superada pela 48 na tela inicial): colunas por Página,
    ordem + `grid-auto-flow: dense`. Apps têm Página própria (`appPlace`); soltos vão para a última Página
    com apps e são "fixados" antes de mover algo; numeração compacta quando uma Página esvazia.
36. **Script do servidor com versão própria** (0.9.2, `ScriptVersion`), separada da `SetupVersion`
    (usuário/chave), para servidores já configurados continuarem desligando. Script antigo responde
    "pedido inválido" → "copie os comandos de atualização".
37. **Mídia e avisos sem estado no BrunOS** (0.9.2): fotos do Immich vêm numa lista do dia (até 24) e a
    troca acontece no navegador (padrão 30 min); cache de 60 imagens em `/data/img-cache`; aparelho novo usa
    o "conhecido" do WatchYourLAN (um aviso por MAC).
38. **Integrações só de leitura, com a credencial mais fraca que serve** (0.9.3): Transmission
    (`session-stats`, `torrent-get`), Syncthing (GETs em `/rest`), Nextcloud (token do app serverinfo).
    Vaultwarden fora (exigiria o `ADMIN_TOKEN`).
42. **Medidor com Automático** (0.11.0): tomada → bateria → estimativa. Tomada Tuya pelo protocolo local
    (sem nuvem), chave local cifrada como as outras credenciais. RAPL só pelo script (exige root).
    Estimativa calibrada aprende o "resto da máquina" nas quedas.
43. **SMART pelo smartctl, sem Scrutiny** (0.11.0): o Scrutiny seria outro container com banco. `-n
    standby` não acorda HD parado.
44. **Atualizações: como se descobre**: apps pela cópia instalada (`apps/`) contra a loja (`repos/`) pelo
    `tipi_version`, sem tocar no Postgres; Linux por `apt-check` e `reboot-required` pelo script. (A parte
    "só avisam" foi substituída pela 47.)
46. **SMART um disco por vez, SATA primeiro nos USB, "basta uma vez"** (0.11.2): num case USB o `smartctl`
    adivinhava NVMe, o kernel reiniciava o USB do disco montado e tudo estourava o tempo. Disco USB tenta
    `-d sat`, depois o automático; o modo que funcionou fica guardado pelo número de série. Travou uma vez
    → "sem SMART" e nunca mais é lido sozinho; "Tentar de novo" testa o outro modo. Limite de tempo não
    interrompe o `smartctl`, então a regra é não insistir.
47. **Atualizar Linux e apps pela interface** (0.12.0): `apt-get update && upgrade` sem perguntas e
    `runtipi-cli app update` com nome validado, soltos da conexão SSH (`systemd-run`); o BrunOS retoma o
    andamento. Sempre com confirmação, um por vez; reiniciar o servidor continua manual. Revisão de
    segurança do script anotada (`ideias-futuras.md`, H5).
48. **Widgets com posição livre, apps em fila, Áreas nunca misturadas** (0.13.0): cada Página tem
    Disposição e uma Medida por Área. Widgets guardam (coluna, linha) e Tamanho em Quadros, com buracos
    permitidos; apps ficam em fila (buraco numa grade de ícones parece erro). Misturar ícones e widgets
    numa grade só foi descartado. Tela estreita: Área compacta sem mudar o que está guardado.
52. **Tamanho do ícone em degraus medidos pela Régua** (0.13.1): 4 tamanhos (Grande = o maior em que 16
    colunas cabem na largura dos widgets com 6 colunas), piso em px no celular; Medida que não cabe vira
    Área compacta, nunca ícone menor. Espaço entre ícones é escolha (Fixo padrão, Elástico, Preencher).
53. **Históricos guardados pelo próprio BrunOS, em três camadas** (0.14.0; substitui a 26): 1 min × 48 h,
    5 min × 7 dias, 20 min × 30 dias (média + pico nas duas últimas); ~4,5 MB para ~100 séries em
    `/data/tsdb/`. Foto completa a cada 6 h e no desligamento + diário só com pontos novos a cada 5 min
    (reescrever 4,5 MB a cada 5 min seria ~1,3 GB/dia no SSD). Recusados: banco do Beszel e
    Prometheus/InfluxDB.
55. **Área útil e escala única** (0.13.2): a Medida decide e o Quadro estica (150 a 340 px) para encher a
    largura; no computador os ícones acompanham a escala (até 60 %), só o celular tem piso fixo; a Régua
    depende só da largura da tela. Escolhas de gosto viraram opções (última fileira, encolher widgets ou
    mandar apps para a próxima tela).
56. **Janela Discos só lê, com três ações que não apagam** (0.15.0): teste do próprio disco, teste de
    velocidade (lê 1 GB para `/dev/null`) e ejetar (USB, nada montado). Lista detalhada pelo `lsblk` a cada
    10 min e quando o `/proc/diskstats` muda. Disco travado pelo SMART (46) não recebe nem `hdparm -C`.
    Linha do tempo em `/data/disk-events.json`.
57. **Container não acompanha montagens** (0.16.0): o JSON do Runtipi não aceita `bind propagation`, então
    o container guarda a montagem de quando ligou. Por isso: (a) depois de montar/desmontar, a interface
    oferece reiniciar o BrunOS; (b) antes de ejetar/formatar, o script procura o disco em todos os
    `/proc/*/mountinfo` e diz quem segura; (c) antes de desmontar, o Arquivos solta a pasta do disco.
    Recusado: montar `/mnt` com propagação fora do Runtipi.
58. **Janela de app = iframe direto, com checagem de cabeçalhos, sem proxy** (0.17.0): o vigia de apps lê
    `X-Frame-Options` e `frame-ancestors`; app que recusa vira **Só em aba**; o que não deu para testar
    tenta a janela. Proxy reescrevendo respostas foi recusado (pesado, quebra login/cookies/WebSocket,
    abre brecha). O peso fica no navegador, por isso há limite de apps abertos.
63. **Ícones guardados: só os usados, baixados pelo servidor** (0.24.0, escolha do Bruno): o ícone do
    catálogo dashboard-icons é baixado do jsDelivr pelo BrunOS na primeira vez que aparece e fica em
    `/data/icons/di` para sempre; os enviados (até 1 MB, nome = hash do conteúdo) ficam em `/data/icons`.
    Recusado: embutir o pacote inteiro (milhares de ícones, a imagem cresceria muitos MB) e continuar só
    pelo CDN (sem internet o ícone sumia). Download só do jsDelivr, nome conferido, sem seguir link de
    outro site (outro endereço = "baixe e envie"); SVG servido com CSP `sandbox`. Sem galeria nem limpeza
    (escolha do Bruno: cada ícone é pequeno).
64. **Desligar e reiniciar pela interface: senha + 1 minuto no próprio servidor** (0.25.0, escolhas do
    Bruno): menu de energia depois do sino, botões nas Configurações e "Reiniciar agora" nas Atualizações.
    Pede a senha do BrunOS (com o mesmo limite de tentativas do login); o script agenda um temporizador do
    systemd (`systemd-run --unit=brunos-energia --on-active=60 … systemctl reboot|poweroff`), então quem
    desliga é o servidor (mesmo que o BrunOS caia), e cancelar (`cancelar-desligar`) para o temporizador.
    Na 0.25.0 era `systemctl --when=+1min`: o logind cria o `/run/nologin`, o PAM barra o login da chave do
    BrunOS e não dava para cancelar (o servidor do Bruno reiniciou no teste). Por isso o desligar do menu
    é o `desligar-agendado`; o `desligar` da bateria continua com `--when` (lá ninguém cancela pela chave). Sem a conexão da Energia, os botões aparecem e levam à janela Energia. Não usa o modo
    simulação (é um pedido explícito). Recusado: desligar na hora, sem a espera para cancelar.
    Detalhes dos avisos: só os de nível Aviso/Importante abrem no sino; os textos ficam no frontend
    (`notifdetails.js`, por tipo), e não no servidor como planejado, para valer também para os avisos já
    guardados e não mudar o `events.json`; um teste confere que todo aviso importante do Go tem texto.

## Loja, Garimpo, instalação

59. **Loja: vitrine do BrunOS, gerência com o Runtipi** (0.18.0): (a) o catálogo sai do disco
    (`repos/<loja>/apps/`, montado só leitura; sem internet nem permissão nova); (b) instalar foi para o
    BrunOS na 61; (c) parar, iniciar, desinstalar e lojas vão pelo script (`runtipi-cli`); desinstalar
    pede a Senha de configuração; o próprio BrunOS não pode ser parado nem desinstalado por ali; (d) lojas
    são de terceiros: sem seguir symlink, Markdown mínimo sem HTML/imagens externas, endereço de loja nova só
    https sem usuário/senha/porta; (e) mais apps vêm por lojas extras (conversor fora do servidor, 60);
    (f) a Loja é a primeira janela do sistema e o desenho segue a largura da janela (container query).
    Recusado: o BrunOS instalar containers por conta própria.
60. **Garimpo: lojas do CasaOS convertidas fora do servidor, um ramo por loja** (0.19.0): repositório
    `BrunnoFe/Garimpo`, conversor em Python no GitHub Actions todo dia (nada roda no servidor). Cada
    origem é um ramo (`casaos`, `linuxserver`) = uma loja do Runtipi com o nome da origem; `garimpo.json`
    dá nome, selo, origem e licença. Recusa por segurança (privileged, socket do Docker, rede/pid/ipc do
    servidor, poderes do Linux, pastas do sistema, aparelhos além de `/dev/dri`), com segunda conferência
    (`confere.py`) e os testes oficiais da loja; apps que já existem na loja oficial ficam de fora. Origens:
    CasaOS (Apache-2.0), LinuxServer (GPL-3.0); Play fora (sem licença); Umbrel para depois. (d) foi
    revista na 62.
61. **Instalar, editar e Ajustes dentro do BrunOS** (0.20.0; revê 59 b): (a) instalar/editar vão pela API
    do Runtipi, chamada pelo script com a chave do `runtipi-cli` (JWT HS256, `sub: "cli"`, 5 min), que
    nunca sai do servidor nem aparece na linha de comando; o Runtipi continua dono do app. Script antigo →
    abre a tela do Runtipi. (b) o BrunOS confere o formulário antes; nenhuma senha volta ao navegador.
    Instalar comum não pede a Senha de configuração. (c) **Ajustes** = `user-config/<loja>/<app>/
    docker-compose.yml`; o Runtipi não confere, então o BrunOS confere (pastas novas só de `/mnt`, `/home`
    e mídia; nada de privileged, rede do servidor, socket, poderes do Linux, aparelhos além da GPU); gravar
    pede a Senha de configuração, guarda 5 cópias e oferece reiniciar. (d) edita a árvore do YAML com
    `gopkg.in/yaml.v3`, preservando o que o Bruno escreveu à mão ("Tome sempre cuidado com os ajustes que
    já fiz"): o que já era perigoso continua, mas os Ajustes não acrescentam nada parecido. (e) listas
    prontas com o recomendado marcado. (f) linha da Senha de configuração em Configurações → Sistema, com
    "Criar agora" abrindo o Terminal com o comando digitado.
62. **Apps que requerem privilégios: avisar e deixar escolher** (0.21.0 e 0.22.0; revê 60 d e 61 c): (a) o
    BrunOS lê a receita de **todo** app, de qualquer loja, com a régua do Garimpo, e mostra o selo
    **Requer privilégios** com a lista em português (a loja oficial já tinha apps assim); (b) instalar um
    deles pede "Entendo os riscos" + Senha de configuração; (c) o Garimpo passa a publicar esses apps na
    loja normal da origem (inclusive os do socket do Docker); (d) os Ajustes não têm botões de privilégio,
    mas o editor do Avançado (0.22.0) aceita, com a mesma confirmação, registro nos Eventos e selo;
    (e) Instalação e Ajustes viram uma janela só, e os Ajustes podem ir junto da instalação (gravados
    antes de pedir a instalação; se ela falhar, o BrunOS tira o arquivo e guarda cópia; recusado: instalar
    com `skipRun`); (f) perguntas novas nos apps convertidos por regra automática (senha com valor padrão
    conhecido, campo marcado pelo CasaOS) + `perguntas.json` de exceções no Garimpo.
