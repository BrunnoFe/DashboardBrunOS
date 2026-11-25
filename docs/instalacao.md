# Instalação e configuração

[← Voltar ao README](../README.md)

## Instalação no Runtipi

1. **Imagem:** cada push na `main` roda o teste com Docker de verdade (`tests/smoke.sh`). Quando o arquivo `VERSION` traz uma versão nova e o teste passa, o GitHub Actions publica `ghcr.io/brunnofe/brunos:<versão>` (amd64 e arm64). Na primeira publicação, abra o pacote em *Your profile → Packages → brunos → Package settings* e mude a visibilidade para **Public**; assim o servidor baixa a imagem sem login.
2. **Loja de apps:** o app fica em [`BrunnoFe/RuntipiAppStore`](https://github.com/BrunnoFe/RuntipiAppStore), na pasta `apps/brunos` (mesmo conteúdo de `runtipi-app/brunos` aqui). O formato é o `docker-compose.json` legado, igual aos outros apps da loja.
3. No Runtipi, atualize a loja e instale o **BrunOS**. Confira no formulário a pasta do Runtipi (por exemplo `/home/<usuário>/runtipi`) e o UID/GID (`id -u`, `id -g`).
4. Abra o app pela porta 8790 ou pelo domínio local e crie seu usuário.

Detalhes e pegadinhas do formato da loja: [`projeto/runtipi-e-loja.md`](projeto/runtipi-e-loja.md).

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
| `/etc/passwd` | `/host/passwd` (ro) | nome do usuário dono de cada processo |

O BrunOS roda com o seu UID/GID, então só lê e escreve o que o seu usuário já pode. Se os discos em `/mnt` pertencem ao root e você quer escrever neles pelo painel, ajuste as permissões só das pastas que quiser (por exemplo `sudo chown -R <usuário>:<usuário> /mnt/<disco>`).

## Variáveis de ambiente

| Variável | Padrão | |
|---|---|---|
| `BRUNOS_ADDR` | `:8080` | endereço do servidor |
| `BRUNOS_DATA` | `/data` | pasta de dados |
| `BRUNOS_FILES` | `/files` | cada subpasta vira um local (dentro de `mnt`/`media`, cada disco) |
| `BRUNOS_ROOTS` | — | lista manual: `Fotos=/files/mnt/disco1/Fotos;Backups=/files/mnt/disco2:ro` |
| `BRUNOS_TERMINAL` | `true` | `false` esconde e desliga o terminal SSH |
| `BRUNOS_RUNTIPI` | `/runtipi` | pasta do Runtipi montada |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | no Runtipi: `unix:///sock/docker.sock` (socket do proxy); também aceita `tcp://…`; `off` desliga |
| `BRUNOS_DOCKER_ACTIONS` | `false` | permite reiniciar apps |
| `BRUNOS_MAX_UPLOAD_GB` | `50` | tamanho máximo por arquivo enviado |
| `HOST_PROC` | `/host/proc` | `/proc` do servidor (rede) |
| `BRUNOS_ALLOW_PUBLIC_SETUP` | `false` | deixe `false`: o cadastro do primeiro usuário só é aceito de dentro da rede de casa |

## Preparar o servidor (recursos extras)

Alguns recursos (energia, saúde dos discos, atualizações, instalar apps pela Loja) usam um pequeno script instalado no servidor. Em *Configurações → Sistema → Energia*, copie os comandos e cole no servidor uma vez; depois o BrunOS mantém o script atualizado sozinho. Para o SMART, instale também o `smartmontools`. Para a temperatura dos HDs, o módulo `drivetemp` do Linux precisa estar carregado (a própria janela mostra os comandos).

O terminal precisa do `openssh-server` ligado no servidor (`sudo apt install openssh-server`). Para desligar o terminal, use `BRUNOS_TERMINAL=false`.
