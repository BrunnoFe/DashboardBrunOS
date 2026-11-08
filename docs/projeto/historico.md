# Histórico de versões e problemas resolvidos

## v0 (protótipo, 11/2026)
- Interface estática com tema tirado do papel de parede, inspirada no código e num vídeo do umbrelOS.
- Entregue como zip e como artefato de demonstração.

## 0.1.0 (07/11/2026): primeira versão no Runtipi
- Backend Go (auth, sessões, settings, wallpapers, stats SSE, apps do Runtipi, Docker via proxy,
  gerenciador de arquivos com lixeira), interface completa, CI com teste de fumaça em Docker real,
  imagem no GHCR, app na loja (PR #1).
- O Bruno confirmou: **"Funcionou no Runtipi!"**

Problemas encontrados no caminho:
- Fuga por link simbólico em upload/colar (gravava fora da raiz) → `Root.realDir()` nas escritas.
- CI: lista de discos vazia no runner (raiz aparece como `/dev/root`) → nome do disco pelo `st_dev`.
- Logo parecia uma carinha triste → redesenhado (órbita com lua).
- Widgets claros demais e texto vazando; dock largo demais no celular (tamanho dinâmico); app sem ícone
  virava quadrado branco (agora gradiente da cor de destaque); painel de envios persistente (some
  sozinho); partições minúsculas listadas (filtro de 1 GiB).
- O ambiente da nuvem não cria tags no GitHub → publicação pelo arquivo `VERSION`.

## 0.2.0 (07/11/2026): ajustes pedidos pelo Bruno depois de usar
Pedidos (com prints):
1. Bolinha de status cortada pela metade → inteira, sem anel, sombra leve (o `overflow: hidden` dos
   ícones com logo cortava).
2. "X" de fechar da janela Arquivos desalinhado → mesma altura dos botões da barra (classe `sheet-files`).
3. Pastas de `/mnt` sem disco apareciam → seção **Discos** com "Mapear", recolhível, salva no servidor.
4. **Widget de bateria** (porcentagem, autonomia, Wh, ícone com cor por nível, tomada) + janela com
   histórico, saúde, ciclos.
5. Apps no dock → lista suspensa com caixas de seleção e busca.
6. (Terminal → feito na 0.3.0.)
7. Ícone da **loja de apps do Runtipi** no dock.
8. Descrição da cor de destaque no modo manual quebrava palavra por palavra → linhas de configuração
   quebram os controles para baixo.

## 0.3.0 (07/11/2026): terminal SSH
- Janela Terminal com xterm.js, SSH com senha ou chave, TOFU, sessões com SSE, fechamento automático.
- `golang.org/x/crypto` vendorizado e conferido no CI.
- Testes unitários com servidor SSH em processo e teste de fumaça com OpenSSH real.
- Problemas: a biblioteca preferia a chave ECDSA do servidor (impressão digital diferente da que o
  usuário costuma ver) → ordem do OpenSSH (ED25519 primeiro). Senha errada no OpenSSH real virava erro
  genérico → erro depois da chave aceita = recusa de login.
- Medido no CI: 9,7 MiB de RAM, imagem de 4 MB, 63 checagens da API.

## 0.4.0 (07/11/2026): energia, discos e central de notificações (etapa 1 do plano)
- **Central de notificações** (sino no canto da tela inicial): `internal/events`, `/data/events.json`
  (500 avisos / 30 dias), avisos repetidos não se acumulam enquanto o problema continua aberto; os
  avisos novos chegam pelo mesmo SSE das estatísticas (`event: notify`) e viram toast.
- **Quedas de energia**: `internal/power` registra tomada → bateria → tomada em
  `/data/power-events.json`, com duração e % gasto; o último consumo medido agora sobrevive a reinícios.
  Lista na janela da bateria e na janela Energia.
- **Desligamento seguro**: limite %, espera (1–10 min), faixa vermelha com contagem regressiva e
  "Cancelar", modo simulação ligado por padrão. Desliga por SSH com a chave própria do BrunOS
  (`/data/hostcmd_key`) num usuário dedicado (`brunos-power`) com `command=` forçado e sudoers de uma
  linha. Assistente em Configurações → Energia mostra os comandos e testa a conexão. Sair da
  simulação exige um teste de conexão que passou. Até 3 tentativas se o servidor não responder.
- **Disco mapeado desmontado**: verificação a cada minuto, aviso crítico e aviso de volta.
- TOFU do SSH extraído para `internal/sshknown` (terminal e desligamento usam o mesmo `known_hosts.json`).
- Testes: unitários de events/power (relógio falso)/hostcmd (servidor SSH em processo, conferindo que o
  "Testar" não abre sessão); apitest com as rotas novas; smoke com OpenSSH real e `command=` que só
  cria um arquivo (prova que o teste não executa o comando).
- Problema no caminho: `bytes.Buffer` compartilhado entre stdout e stderr da sessão SSH perdia a saída
  (as duas cópias rodam ao mesmo tempo) → buffers separados.

## 0.5.0 (08/11/2026): login novo, temperatura dos HDs e previsão de disco cheio (etapa 2)
- **Aviso de login novo** (`internal/logins`, `/data/logins.json`): navegador + sistema + rede; rede
  local conta como um lugar só, internet por faixa (/24, /48). Rede local = aviso informativo; internet
  = aviso. Também avisa quando um endereço é bloqueado por errar a senha (máx. 1 aviso a cada 6 h por
  endereço). As sessões já abertas antes da atualização são aprendidas sem avisar.
- **Temperatura dos discos**: hwmon `drivetemp` (HD/SSD SATA, precisa de `modprobe drivetemp`; a
  interface mostra os comandos) e `nvme`. Para não acordar HD parado, só mede de novo quando o disco
  teve atividade; a leitura de sensores a cada 2 s passou a ignorar o `drivetemp`. Aviso de disco
  quente (HD ≥ 55 °C, SSD ≥ 70 °C) e de volta ao normal.
- **Previsão de disco cheio** (`internal/diskhist`, `/data/disk-history.json`): um ponto por dia por
  disco, regressão sobre 30 dias, `GET /api/disks/forecast`. Avisos "deve encher em ~N dias" (≤ 30) e
  "quase cheio" (≥ 90%). Na janela Uso ao vivo: temperatura colorida e "+X/dia · cheio em ~N".
  A pedido do Bruno, a previsão é **opcional e vem desligada** (Configurações → Sistema → Previsão de
  disco cheio): desligada, não aparece nem avisa "vai encher"; o histórico continua sendo gravado e o
  aviso de "quase cheio" vale sempre.
- No celular, a lista de discos põe a barra de uso na linha de baixo (o nome cortava).

## 0.6.0 (08/11/2026): arquivos (etapa 4 do plano; a etapa 3 foi adiada)
- O Bruno pediu para adiar a etapa 3 (dois passos, sessões ativas); o link público (F3) foi junto.
- **Vídeo e áudio**: a imagem `scratch` não tem `/etc/mime.types` e a tabela do Go só conhece imagens e
  arquivos da web, então `.mp4`/`.mp3` iam como download. Agora há uma tabela própria de tipos de
  mídia (avançar/retroceder funciona pelo Range), `.mkv` liberado e um aviso quando o navegador não
  toca o codec.
- **Editor de texto** (CodeMirror 6, escolha do Bruno): "Editar" no menu e na pré-visualização,
  Ctrl+S, procurar/substituir, cores por linguagem, aviso ao sair sem salvar. Salva de forma atômica
  e recusa se o arquivo mudou desde que abriu. "Novo arquivo de texto".
- **Envio com retomada**: arquivos acima de 16 MB vão em pedaços de 8 MB; queda de conexão ou página
  recarregada continuam de onde pararam.
- **Extrair** `.zip`, `.tar`, `.tar.gz` (seguro contra zip-slip e zip-bomba) e **encontrar
  duplicados** (com seleção pronta para mandar à lixeira).
- **Mapa do espaço**: treemap das pastas, com cache de 10 min para navegar nas subpastas na hora.
- `openDialog` ganhou `beforeClose` (o editor pergunta antes de fechar com alterações).

## Próximas
Veja `roadmap.md`.
