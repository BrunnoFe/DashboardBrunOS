# Desenvolvimento

[← Voltar ao README](../README.md)

O BrunOS é um servidor em Go (só biblioteca padrão, mais `x/crypto` e `yaml.v3` vendorizados) com uma interface em HTML, CSS e JavaScript puros, sem etapa de build, embutida no binário. A explicação completa de cada parte está em [`projeto/arquitetura.md`](projeto/arquitetura.md) e o vocabulário em [`CONTEXT.md`](../CONTEXT.md).

## Rodando localmente

```bash
# servidor com pastas locais
BRUNOS_DATA=./tmp/data BRUNOS_FILES=./tmp/files BRUNOS_RUNTIPI=./tmp/runtipi DOCKER_HOST=off HOST_PROC=/proc go run .
```

**Modo demonstração:** abrir `web/static/index.html` por um servidor de arquivos qualquer (sem o backend) liga dados simulados. Qualquer usuário e senha entram.

```bash
cd web/static && python3 -m http.server 8765
```

## Testes

```bash
go vet ./... && go test -race -count=1 ./...                  # backend
bash tests/fixture.sh /tmp/bt                                 # servidor fictício para testes
BASE=http://127.0.0.1:8099 FIX=/tmp/bt python3 tests/apitest.py   # testes da API
node --test tests/layout.test.mjs tests/windows.test.mjs      # encaixe dos widgets e contas das janelas
docker build -t brunos:teste . && FIX=/tmp/brunos-teste bash tests/smoke.sh brunos:teste   # container real
```

## Estrutura do código

```
main.go                  servidor HTTP, rotas, autenticação, preferências, apps, estatísticas
internal/auth            usuário, senha, sessões, limite de tentativas
internal/sysinfo         CPU, memória, temperatura, ventoinha, bateria, discos, rede (lê /proc e /sys)
internal/runtipi         descobre os apps instalados (apps/<loja>/<app>/config.json + app.env)
internal/dockerc         cliente mínimo da API do Docker (listar, estatísticas, ligar/parar/reiniciar, logs)
internal/appmon          uso e disponibilidade de cada app nas últimas 24 h
internal/history         histórico de uso do servidor (24 h por minuto, 7 dias por hora)
internal/procs           processos do servidor (como o htop), só leitura
internal/netmon          latência e quedas da internet
internal/files           gerenciador de arquivos
internal/events          central de notificações (o sino)
internal/power           quedas de energia e desligamento seguro pela bateria
internal/diskhist        uso diário dos discos e previsão de disco cheio
internal/logins          aparelhos que já entraram (aviso de acesso novo)
internal/hostcmd         comandos no servidor por SSH
internal/terminal        terminal SSH embutido
internal/termkey         Chave do Terminal
internal/notes           texto dos widgets de Notas
web/static               interface (HTML/CSS/JS puros, sem build), embutida no binário
runtipi-app/brunos       pacote para a loja do Runtipi (config.json + docker-compose.json)
tools/codemirror         receita do pacote do CodeMirror usado pelo editor
tests/                   fixture, testes da API e teste de fumaça com Docker
```

## API

Todas as rotas ficam em `/api/*` e exigem login (menos as públicas de entrada). A lista completa está em [`projeto/arquitetura.md`](projeto/arquitetura.md).

## Mais documentação

| Documento | Conteúdo |
|---|---|
| [`projeto/visao-geral.md`](projeto/visao-geral.md) | objetivo e princípios |
| [`projeto/arquitetura.md`](projeto/arquitetura.md) | backend, API, dados em `/data`, frontend, preferências |
| [`projeto/decisoes.md`](projeto/decisoes.md) | decisões técnicas e o porquê |
| [`projeto/runtipi-e-loja.md`](projeto/runtipi-e-loja.md) | integração com o Runtipi, formato da loja, CI |
| [`projeto/historico.md`](projeto/historico.md) | resumo de cada versão |
| [`projeto/roadmap.md`](projeto/roadmap.md) | o que está combinado e ainda não terminou |
| [`projeto/ideias-futuras.md`](projeto/ideias-futuras.md) | ideias adiadas ou recusadas |

## Créditos e licenças de terceiros

- **Papéis de parede** em `web/static/assets/wallpapers`: do repositório do [umbrelOS](https://github.com/getumbrel/umbrel), licença PolyForm Noncommercial 1.0.0. São para uso pessoal e não comercial; troque pelos seus se for distribuir.
- **Ícones dos apps** em `web/static/assets/icons`: projeto [dashboard-icons](https://github.com/homarr-labs/dashboard-icons). As marcas pertencem aos respectivos donos.
- **Fonte Inter**: SIL Open Font License 1.1 (`web/static/assets/fonts/LICENSE-Inter.txt`).
- **Editor de texto**: [CodeMirror 6](https://codemirror.net) (MIT), empacotado em `web/static/vendor/codemirror` pela receita de `tools/codemirror`.
- **Terminal**: [xterm.js](https://github.com/xtermjs/xterm.js) (MIT) e `golang.org/x/crypto/ssh` (BSD), distribuídos junto com o código.
- **Clima**: dados do [Open-Meteo](https://open-meteo.com) (CC BY 4.0), consultados pelo servidor só quando há um widget de clima.
- Inspiração visual: umbrelOS. A interface foi escrita do zero; os valores das sombras de vidro, da curva de ampliação do dock e da animação de abertura das janelas foram adaptados do `tailwind.config.ts` e do `index.css` do Umbrel.
