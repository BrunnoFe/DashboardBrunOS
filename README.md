# BrunOS

Painel para servidor caseiro com [Runtipi](https://runtipi.io), no estilo do umbrelOS: tela inicial com papel de parede, widgets de vidro, dock com ampliação, gerenciador de arquivos e uso do hardware ao vivo.

- **Leve:** um binário Go estático (~9 MB, imagem `scratch`), sem dependências externas, ~12–20 MB de RAM.
- **Integrado ao Runtipi:** lê os apps instalados (nome, logo, porta, domínio local) direto da pasta do Runtipi, só leitura.
- **Seguro:** usuário próprio criado na primeira execução, senha em PBKDF2, sessões com cookie HttpOnly e Docker atrás de um proxy com permissões mínimas.

## Estrutura

```
main.go                  servidor HTTP, rotas, autenticação, preferências, apps, estatísticas
internal/auth            usuário, senha (PBKDF2-SHA256, 600k iterações), sessões, limite de tentativas
internal/sysinfo         CPU, memória, temperatura, ventoinha, bateria, discos, rede (lê /proc e /sys)
internal/runtipi         descobre os apps instalados (apps/<loja>/<app>/config.json + app.env)
internal/dockerc         cliente mínimo da API do Docker (listar, estatísticas, ligar/parar/reiniciar, logs)
internal/appmon          uso e disponibilidade de cada app nas últimas 24 h (aviso quando um app cai)
internal/history         histórico de uso do servidor (24 h por minuto, 7 dias por hora)
internal/procs           processos do servidor (como o htop), só leitura
internal/netmon          latência e quedas da internet
internal/files           gerenciador de arquivos (os.Root, lixeira, upload com retomada, zip, busca,
                         editor de texto, extrair, duplicados, mapa do espaço)
internal/events          central de notificações (o sino da tela inicial)
internal/power           quedas de energia e desligamento seguro pela bateria
internal/diskhist        uso diário dos discos e previsão de disco cheio
internal/logins          aparelhos que já entraram (aviso de acesso novo)
internal/hostcmd         comandos no servidor por SSH (usado pelo desligamento seguro)
internal/terminal        terminal SSH embutido (abas, "ainda está rodando?")
internal/termkey         Chave do Terminal
internal/notes           texto dos widgets de Notas
web/static               interface (HTML/CSS/JS puros, sem build), embutida no binário
runtipi-app/brunos       pacote para a app store (config.json + docker-compose.json)
tools/codemirror         receita do pacote do CodeMirror usado pelo editor (só para atualizá-lo)
tests/                   fixture, testes da API e teste de fumaça com Docker
```

## Instalação no Runtipi

1. **Imagem:** cada push na `main` roda o teste com Docker de verdade (`tests/smoke.sh`). Quando o arquivo `VERSION` traz uma versão nova e o teste passa, o GitHub Actions publica `ghcr.io/brunnofe/brunos:<versão>` (amd64 e arm64). Na primeira publicação, abra o pacote em *Your profile → Packages → brunos → Package settings* e mude a visibilidade para **Public**; assim o servidor baixa a imagem sem login.
2. **App store:** o app fica em [`BrunnoFe/RuntipiAppStore`](https://github.com/BrunnoFe/RuntipiAppStore), na pasta `apps/brunos` (mesmo conteúdo de `runtipi-app/brunos` aqui). O formato é o `docker-compose.json` legado, igual aos outros apps da loja.
3. No Runtipi, atualize a loja e instale o **BrunOS**. Confira no formulário a pasta do Runtipi (`/home/ptbruno/runtipi`) e o UID/GID (`id -u`, `id -g`).
4. Abra o app pela porta 8790 ou pelo domínio local e crie seu usuário.

### Testar no servidor antes de instalar pela loja

```bash
git clone https://github.com/BrunnoFe/DashboardBrunOS && cd DashboardBrunOS
docker build -t brunos:teste .
FIX=/tmp/brunos-teste bash tests/smoke.sh brunos:teste   # mesmo teste do GitHub, com o Docker do servidor
```

## Montagens (docker-compose)

| No servidor | No container | Para quê |
|---|---|---|
| `${APP_DATA_DIR}/data` | `/data` | usuário, sessões, preferências, papéis de parede enviados |
| pasta do Runtipi | `/runtipi` (ro) | descobrir apps, logos, portas e domínios |
| `/home` | `/files/home` | gerenciador de arquivos |
| `/mnt` | `/files/mnt` | cada pasta em `/mnt/*` pode virar um disco no gerenciador. Em *Arquivos → Discos → Mapear* você escolhe quais aparecem (fica salvo em `/data/disks.json`); sem escolha, aparecem só as pastas com um disco montado |
| pasta do Runtipi | `/files<mesmo caminho>` (ro) | impede apagar o Runtipi pelo gerenciador |
| `/proc`, `/etc/os-release`, `/etc/hostname` | `/host/...` (ro) | rede do servidor, processos e informações do sistema |
| `/etc/passwd` | `/host/passwd` (ro) | nome do usuário dono de cada processo (só nomes; senhas ficam no `/etc/shadow`, que não é montado) |

O BrunOS roda com o seu UID/GID, então só lê e escreve o que o seu usuário já pode. Se os discos em `/mnt` pertencem ao root e você quer escrever neles pelo painel, rode `sudo chown -R ptbruno:ptbruno /mnt/1TB_HDD` (ou ajuste as permissões só das pastas que quiser).

## Variáveis de ambiente

| Variável | Padrão | |
|---|---|---|
| `BRUNOS_ADDR` | `:8080` | endereço do servidor |
| `BRUNOS_DATA` | `/data` | pasta de dados |
| `BRUNOS_FILES` | `/files` | cada subpasta vira um local (dentro de `mnt`/`media`, cada disco) |
| `BRUNOS_ROOTS` | — | lista manual: `Fotos=/files/mnt/1TB_HDD/Fotos;Backups=/files/mnt/1TB_HDD2:ro` |
| `BRUNOS_TERMINAL` | `true` | `false` esconde e desliga o terminal SSH |
| `BRUNOS_RUNTIPI` | `/runtipi` | pasta do Runtipi montada |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | ou `tcp://brunos-docker-proxy:2375`; `off` desliga |
| `BRUNOS_DOCKER_ACTIONS` | `false` | permite reiniciar apps |
| `BRUNOS_MAX_UPLOAD_GB` | `50` | tamanho máximo por arquivo enviado |
| `HOST_PROC` | `/host/proc` | `/proc` do servidor (rede) |
| `BRUNOS_ALLOW_PUBLIC_SETUP` | `false` | por padrão a primeira execução só aceita IPs da rede local/Tailscale |

## Desenvolvimento

```bash
go run . # com variáveis apontando para pastas locais:
BRUNOS_DATA=./tmp/data BRUNOS_FILES=./tmp/files BRUNOS_RUNTIPI=./tmp/runtipi DOCKER_HOST=off HOST_PROC=/proc go run .
```

Abrir `web/static/index.html` sem o servidor liga o **modo demonstração**, com dados simulados.

## API (resumo)

`GET /api/public` · `POST /api/auth/setup|login|logout` · `GET|PUT /api/settings` · `GET /api/stats` · `GET /api/stats/stream` (SSE) · `GET /api/system` · `GET /api/apps` · `GET /api/apps/logo?urn=` · `GET /api/usage` · `POST /api/apps/restart` · `GET|PUT|DELETE /api/wallpapers` · `/api/files/{roots,list,raw,zip,search,upload,mkdir,rename,paste,trash,delete}` · `GET /api/events` · `POST /api/events/{read,clear}` · `GET /api/power` · `PUT /api/power/config` · `POST /api/power/{cancel,test}` · `GET /api/power/setup` · `GET /api/disks/forecast` · `GET|PUT /api/files/text` · `POST /api/files/{newfile,extract}` · `/api/files/upload/{start,chunk,status,finish,cancel}` · `GET /api/files/{duplicates,usage}` · `POST /api/apps/{start,stop}` · `GET /api/apps/{containers,history,logs,logs/stream}` · `GET /api/stats/history` · `GET /api/processes` · `GET /api/internet`

## Créditos e licenças de terceiros

- **Papéis de parede** em `web/static/assets/wallpapers`: do repositório do [umbrelOS](https://github.com/getumbrel/umbrel), licença PolyForm Noncommercial 1.0.0. São para uso pessoal e não comercial; troque pelos seus se for distribuir.
- **Ícones dos apps** em `web/static/assets/icons`: projeto [dashboard-icons](https://github.com/homarr-labs/dashboard-icons). As marcas pertencem aos respectivos donos.
- **Fonte Inter**: SIL Open Font License 1.1 (`web/static/assets/fonts/LICENSE-Inter.txt`).
- **Editor de texto**: [CodeMirror 6](https://codemirror.net) (MIT), empacotado em `web/static/vendor/codemirror` pela receita de `tools/codemirror`.
- **Clima**: dados do [Open-Meteo](https://open-meteo.com) (CC BY 4.0), consultados pelo servidor só quando há um widget de clima.
- Inspiração visual: umbrelOS. A interface foi escrita do zero; os valores das sombras de vidro, da curva de ampliação do dock e da animação de abertura das janelas foram adaptados do `tailwind.config.ts` e do `index.css` do Umbrel.

## Terminal

Em *Configurações → Sistema → Terminal* (ou `Ctrl+K` → "terminal"), o BrunOS abre um terminal no servidor por SSH, com o seu usuário e senha (ou chave privada). Na primeira conexão ele mostra a impressão digital da chave do servidor para você conferir com `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`.

O terminal tem até 4 abas, botão de minimizar (as abas continuam rodando e o ícone aparece na dock), comandos salvos em botões e conexões salvas. Com a *Chave do Terminal* (no ⚙ da janela), ele entra direto no servidor sem pedir a senha do SSH: basta colar uma vez no servidor o comando que a janela mostra. Antes de fechar uma aba, o BrunOS confere se ainda tem algo rodando nela e pergunta; o tempo para fechar abas esquecidas fica em *Configurações → Sistema*.

Precisa do `openssh-server` ligado no servidor (`sudo apt install openssh-server`). Para desligar o recurso, use `BRUNOS_TERMINAL=false`.

O terminal usa o [xterm.js](https://github.com/xtermjs/xterm.js) (MIT) e o `golang.org/x/crypto/ssh` (BSD), ambos distribuídos junto com o código.

## Tela de bloqueio, relógio, clima e notas

`Ctrl+Shift+L` (ou a busca, ou o botão direito na tela inicial) bloqueia a tela, que mostra um relógio grande sobre o papel de parede. Em *Configurações → Conta* você escolhe se ela pede a senha do BrunOS para voltar e depois de quanto tempo sem uso ela bloqueia sozinha.

Na janela *Widgets* há os modelos Relógio (digital ou de ponteiros, com fuso horário), Bloco de notas e Lista de tarefas (escritos direto na tela inicial e iguais em todos os aparelhos) e, cadastrando a integração *Clima* com o nome da sua cidade, temperatura, ícone do tempo e previsão dos próximos dias.

## Energia e notificações

O sino no canto da tela inicial reúne os avisos do servidor: queda e volta da energia, desligamento seguro, disco mapeado que se desmontou, disco quente, disco quase cheio (ou que deve encher em breve, se a previsão estiver ligada em *Configurações → Sistema*) e acesso ao BrunOS de um navegador ou rede que nunca entrou.

A janela *Uso ao vivo* mostra a temperatura de cada disco e, se ligada, a previsão de quando ele enche, o histórico de CPU, memória, temperatura e rede (5 min, 24 h ou 7 dias) e a lista de processos do servidor. O widget *Internet* mostra a latência e quantas vezes a internet caiu (o sino avisa quando cai e quando volta).

## Apps

Clique com o botão direito num ícone → *Detalhes, logs e uso* para ver a disponibilidade e o consumo de CPU e memória nas últimas 24 h, os logs de cada container (inclusive ao vivo) e ligar, parar ou reiniciar o app (com *Permitir reiniciar apps* ligado no formulário do Runtipi). Ligar e parar pelo BrunOS não muda o estado no Runtipi. *Desinstalar* abre a página do app no painel do Runtipi. A cada minuto o BrunOS confere se cada app responde e avisa no sino quando algum cai (dá para desligar em *Configurações → Apps*). Arraste um app sobre outro para criar uma pasta; com muitos apps, a tela inicial vira páginas. Para a temperatura dos HDs, o módulo `drivetemp` do Linux precisa estar carregado; a própria janela mostra os comandos.

Em notebooks usados como servidor, *Configurações → Sistema → Energia* registra as quedas de energia e pode desligar o servidor com segurança quando a bateria chegar a um limite sem energia da tomada. Antes de desligar aparece uma contagem regressiva com botão para cancelar. O recurso começa em modo simulação e tem um assistente que mostra os comandos para preparar o servidor e testa a conexão.
