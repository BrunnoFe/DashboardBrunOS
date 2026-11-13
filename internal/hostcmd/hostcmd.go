// Package hostcmd executa ações que precisam de root no servidor (desligar e limite de carga
// da bateria) sem dar nenhum privilégio ao container. O BrunOS tem uma chave SSH própria e
// entra num usuário dedicado do servidor (brunos-power) cuja linha no authorized_keys força
// um único programa, o script brunos-host (HostScript), que só aceita alguns pedidos:
//
//	restrict,command="/usr/local/sbin/brunos-host" ssh-ed25519 AAAA... brunos-power
//
// O script lê o pedido (SSH_ORIGINAL_COMMAND), recusa tudo o que não for "desligar",
// "limite 50..100", "protecao on|off" ou "versao", e chama a si mesmo pelo sudo (o sudoers
// libera só ele), conferindo o pedido de novo já como root.
//
// Por que systemctl e não "shutdown -h +1" para desligar: desde o systemd 257 (o Ubuntu 26.04
// usa o 259) uma trava de desligamento ("block inhibitor") vale até para o root. O
// unattended-upgrades segura uma o tempo todo, e o "shutdown -h +1" era recusado.
// --check-inhibitors=no passa por cima da trava: numa queda de energia, desligar é mais seguro
// que esperar a bateria acabar.
//
// Com command= forçado, QUALQUER sessão roda o comando. Por isso a configuração atual usa uma chave
// própria (/data/hostcmd_key_v2): um servidor ainda com a configuração antiga (até a 0.8.0) recusa
// essa chave já na autenticação, sem executar nada, e o teste de conexão (que só autentica) prova
// que o servidor foi atualizado. A chave da configuração antiga não é mais usada.
//
// Test só autentica e fecha a conexão SEM abrir sessão; só Run abre uma sessão.
package hostcmd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	_ "embed"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"brunos/internal/sshknown"
	"brunos/internal/store"
)

const (
	DefaultUser = "brunos-power"
	// ScriptPath é onde os comandos de configuração instalam o script no servidor.
	ScriptPath = "/usr/local/sbin/brunos-host"
	// SetupVersion sobe quando a CHAVE ou a linha do authorized_keys muda.
	// 1 = shutdown -h +1 com a chave antiga (até a 0.8.0, não é mais usada); 2 = script brunos-host.
	// Script novo com pedidos a mais (ScriptVersion) NÃO sobe isto: um servidor com o script 2 continua
	// desligando com a chave nova e só recusa os pedidos que não conhece.
	SetupVersion = 2
	// ScriptVersion é a versão do brunos-host (3 = pedido "tailscale", 0.9.2; 4 = "energia", "smart",
	// "temperatura" e "atualizacoes", 0.11.0; 5 = "discos" e SMART um disco por vez com o modo, 0.11.2;
	// 6 = "atualizar-linux", "atualizar-app <app>:<loja>" e "atualizacao-estado", 0.12.0;
	// 7 = backup antes de atualizar app, "tamanho-backup", repositório quebrado vira aviso e
	// "atualizar-script" (o script se troca sozinho por um assinado), 0.12.1; 8 = o andamento manda a
	// primeira linha do registro, 0.12.2; 9 = "atualizar-lojas" (runtipi-cli appstore update), 0.12.3).
	ScriptVersion = 9
	// SelfUpdateFrom: a partir desta versão o script do servidor sabe se atualizar sozinho.
	SelfUpdateFrom = 7
	maxOutput      = 1 << 20 // saída de um pedido (ex.: tailscale status --json)
	dialTimeout    = 10 * time.Second
	keyComment     = "brunos-power"
)

// Pedidos aceitos pelo script do servidor.
const (
	CmdShutdown  = "desligar"
	CmdVersion   = "versao"
	CmdTailscale = "tailscale" // só lê: tailscale status --json (script 3)
	// script 4 (0.11.0) e 5 (0.11.2), todos só leitura:
	CmdEnergy  = "energia"      // contador do RAPL (energy_uj) e o limite dele
	CmdUpdates = "atualizacoes" // apt-check, reboot-required e a lista de pacotes
	// script 5 (0.11.2), só leitura:
	CmdDisks = "discos"      // lsblk: nome, transporte (usb, sata, nvme) e número de série de cada disco
	CmdSmart = "smart"       // "smart <disco> <sat|auto>": smartctl -j -n standby -H -i -A de UM disco
	CmdTemps = "temperatura" // "temperatura <disco> <sat|auto>": smartctl -j -n standby -A de UM disco
)

// Modos de leitura do SMART: sat = "-d sat" (disco SATA atrás de um adaptador USB); auto = o smartctl
// decide pelo próprio banco de adaptadores (inclusive as pontes USB de NVMe).
const (
	ModeSAT  = "sat"
	ModeAuto = "auto"
)

// Script 7 (0.12.1).
const (
	CmdBackupSize   = "tamanho-backup"   // "tamanho-backup <app>:<loja>": bytes da pasta do app + app-data e o espaço livre
	CmdUpdateScript = "atualizar-script" // "atualizar-script <assinatura base64>" + o script novo pela entrada padrão
)

// ReleasePublicKey é a chave pública de publicação: o script do servidor só se troca por outro assinado
// pela chave privada correspondente, que fica só nos segredos do GitHub Actions (BRUNOS_SCRIPT_KEY).
const ReleasePublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIGRDff29cdWSeHDGjTkfKLD4+N9hfuZrexuEp8Y7var"

// assinatura.txt: assinatura (ssh-keygen -Y sign, namespace brunos-host) do HostScript, em base64.
// Gerada pelo CI na publicação; vazia nas compilações locais (aí o script não se atualiza sozinho).
//
//go:embed assinatura.txt
var scriptSig string

// ScriptSignature devolve a assinatura do HostScript desta compilação ("" se não houver).
func ScriptSignature() string { return strings.TrimSpace(scriptSig) }

// ReadCmds são os pedidos de leitura que o BrunOS pode fazer pelo HostOutput.
var ReadCmds = map[string]bool{CmdTailscale: true, CmdEnergy: true, CmdSmart: true, CmdTemps: true, CmdUpdates: true, CmdDisks: true,
	CmdVersion: true, CmdBackupSize: true}

// ParseVersion lê "brunos-host 7" (0 se não der).
func ParseVersion(out []byte) int {
	f := strings.Fields(string(out))
	if len(f) == 2 && f[0] == "brunos-host" {
		n, _ := strconv.Atoi(f[1])
		return n
	}
	return 0
}

var diskRe = regexp.MustCompile(`^(sd[a-z]{1,2}|nvme[0-9]n1)$`)

// ValidDisk diz se o nome é um disco que o script aceita (sda, sdab, nvme0n1).
func ValidDisk(dev string) bool { return diskRe.MatchString(dev) }

// ReadAllowed confere um pedido de leitura inteiro, com os argumentos (o script confere de novo).
func ReadAllowed(cmd string) bool {
	f := strings.Fields(cmd)
	if len(f) == 0 || !ReadCmds[f[0]] {
		return false
	}
	switch f[0] {
	case CmdSmart, CmdTemps:
		return len(f) == 3 && ValidDisk(f[1]) && (f[2] == ModeSAT || f[2] == ModeAuto)
	case CmdBackupSize:
		return len(f) == 2 && ValidApp(f[1])
	}
	return len(f) == 1
}

// DiskCmd monta "smart sdb sat" / "temperatura sdb auto".
func DiskCmd(kind, dev, mode string) string { return kind + " " + dev + " " + mode }

// Script 6 (0.12.0): atualizar pela interface (decisão 47). Rodam soltos no servidor (systemd-run).
const (
	CmdUpdateOS     = "atualizar-linux"    // apt-get update + upgrade sem perguntas
	CmdUpdateApp    = "atualizar-app"      // "atualizar-app <app>:<loja> <sim|nao>": backup (sim) + runtipi-cli app update
	CmdUpdateStatus = "atualizacao-estado" // rodando|parado + as últimas linhas do registro
	// script 9 (0.12.3): o "atualizar lojas" do Runtipi, antes de conferir as versões dos apps
	CmdRefreshStores = "atualizar-lojas"
)

var appRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}:[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidApp diz se o nome é um app do Runtipi que o script aceita ("immich:migrated").
func ValidApp(urn string) bool { return appRe.MatchString(urn) }

// ActAllowed confere um pedido de atualização inteiro (o script confere de novo).
func ActAllowed(cmd string) bool {
	f := strings.Fields(cmd)
	switch {
	case len(f) == 1:
		return f[0] == CmdUpdateOS || f[0] == CmdUpdateStatus || f[0] == CmdRefreshStores
	case len(f) == 3:
		return f[0] == CmdUpdateApp && ValidApp(f[1]) && (f[2] == "sim" || f[2] == "nao")
	}
	return false
}

var sigRe = regexp.MustCompile(`^[A-Za-z0-9+/=]+$`)

// UpdateScriptCmd monta o pedido "atualizar-script <assinatura>" (vazio se não houver assinatura válida).
func UpdateScriptCmd() string {
	s := ScriptSignature()
	if len(s) > 4000 || !sigRe.MatchString(s) {
		return ""
	}
	return CmdUpdateScript + " " + s
}

// CmdLimit e CmdProtect montam os pedidos do limite de carga (o script confere de novo).
func CmdLimit(pct int) string { return fmt.Sprintf("limite %d", pct) }
func CmdProtect(on bool) string {
	if on {
		return "protecao on"
	}
	return "protecao off"
}

// HostScript é o script instalado em ScriptPath. Indentado com espaços (não tabs): colado
// num terminal, um tab poderia virar autocompletar.
const HostScript = `#!/bin/sh
# brunos-host 9: o único programa que a chave do BrunOS pode rodar neste servidor.
# Aceita só: desligar | limite <50-100> | protecao <on|off> | tailscale | energia | atualizacoes |
# discos | smart <disco> <sat|auto> | temperatura <disco> <sat|auto> | atualizar-lojas | atualizar-linux |
# atualizar-app <app>:<loja> <sim|nao> | atualizacao-estado | tamanho-backup <app>:<loja> |
# atualizar-script <assinatura> | versao. Recusa todo o resto.
# As atualizações rodam soltas pelo systemd-run (seguem mesmo se o BrunOS reiniciar), registradas em
# /var/log/brunos-atualizar.log. O script só se troca por outro assinado pela chave de publicação do
# BrunOS (abaixo) e de versão maior. Instalado pelos comandos de Configurações -> Energia do BrunOS.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
VERSAO=9
CHAVE_ASSINATURA='` + ReleasePublicKey + `'
if [ "$(id -u)" -ne 0 ]; then
  # chamado pelo SSH (usuário brunos-power): o pedido vem em SSH_ORIGINAL_COMMAND
  set -f
  set -- ${SSH_ORIGINAL_COMMAND:-}
  set +f
fi
falha() { echo "$1" >&2; exit "${2:-2}"; }
# app do Runtipi: "immich:migrated" (letras minúsculas, números, ponto, traço e sublinhado)
app_ok() {
  case "$1" in [a-z0-9]*:[a-z0-9]*) ;; *) return 1 ;; esac
  case "$1" in *[!a-z0-9._:-]*|*:*:*) return 1 ;; esac
  [ ${#1} -le 129 ]
}
if [ $# -lt 1 ] || [ $# -gt 4 ]; then falha "pedido inválido"; fi
acao=$1
valor=${2:-}
modo=${3:-}
extra=${4:-}
case "$acao" in
  versao) [ $# -eq 1 ] || falha "pedido inválido"; echo "brunos-host $VERSAO"; exit 0 ;;
  desligar|tailscale|energia|atualizacoes|discos|atualizar-linux|atualizacao-estado|atualizar-lojas) [ $# -eq 1 ] || falha "pedido inválido" ;;
  atualizar-app)
    [ $# -eq 3 ] || falha "pedido inválido"
    app_ok "$valor" || falha "app inválido"
    case "$modo" in sim|nao) ;; *) falha "pedido inválido" ;; esac ;;
  tamanho-backup)
    [ $# -eq 2 ] || falha "pedido inválido"
    app_ok "$valor" || falha "app inválido" ;;
  atualizar-script)
    # a assinatura vem no pedido (base64, uma palavra só); o script novo vem pela entrada padrão
    [ $# -eq 2 ] || falha "pedido inválido"
    case "$valor" in *[!A-Za-z0-9+/=]*|"") falha "assinatura inválida" ;; esac
    [ ${#valor} -le 4000 ] || falha "assinatura inválida" ;;
  executar)
    # só o próprio script, como root, rodando dentro do systemd (o systemd-run abaixo); nunca pelo SSH
    [ "$(id -u)" -eq 0 ] && [ -n "${INVOCATION_ID:-}" ] || falha "pedido inválido"
    case "$valor" in
      atualizar-linux) [ $# -eq 2 ] || falha "pedido inválido" ;;
      atualizar-app)
        [ $# -eq 4 ] && app_ok "$modo" || falha "pedido inválido"
        case "$extra" in sim|nao) ;; *) falha "pedido inválido" ;; esac ;;
      *) falha "pedido inválido" ;;
    esac ;;
  smart|temperatura)
    [ $# -eq 3 ] || falha "pedido inválido"
    case "$valor" in sd[a-z]|sd[a-z][a-z]|nvme[0-9]n1) ;; *) falha "disco inválido" ;; esac
    case "$modo" in sat|auto) ;; *) falha "modo inválido (use sat ou auto)" ;; esac ;;
  limite)
    [ $# -eq 2 ] || falha "pedido inválido"
    case "$valor" in [5-9][0-9]|100) ;; *) falha "limite inválido (use de 50 a 100)" ;; esac ;;
  protecao)
    [ $# -eq 2 ] || falha "pedido inválido"
    case "$valor" in on|off) ;; *) falha "valor inválido (use on ou off)" ;; esac ;;
  *) falha "pedido inválido" ;;
esac
if [ "$(id -u)" -ne 0 ]; then
  # o pedido já foi conferido acima: só palavras da lista, sem espaços nem curingas
  exec sudo -n /usr/local/sbin/brunos-host "$@"
fi
# daqui para baixo roda como root (pelo sudo), com o pedido já conferido duas vezes
baterias() {
  for b in /sys/class/power_supply/*; do
    if [ "$(cat "$b/type" 2>/dev/null)" = Battery ]; then echo "$b"; fi
  done
}
# pasta do Runtipi (onde fica o runtipi-cli), gravada pelos comandos de configuração
pasta_runtipi() {
  d=$(sed -n 's/^RUNTIPI=//p' /etc/brunos-host.conf 2>/dev/null | head -n 1)
  case "$d" in /*) ;; *) return 1 ;; esac
  case "$d" in *..*) return 1 ;; esac
  [ -x "$d/runtipi-cli" ] || return 1
  echo "$d"
}
LOG=/var/log/brunos-atualizar.log
feito=0
case "$acao" in
  desligar)
    exec systemctl poweroff --check-inhibitors=no --when=+1min ;;
  tailscale)
    command -v tailscale >/dev/null 2>&1 || falha "o Tailscale não está instalado neste servidor" 3
    exec tailscale status --json ;;
  energia)
    for z in /sys/class/powercap/intel-rapl:0 /sys/class/powercap/intel-rapl/intel-rapl:0; do
      if [ -r "$z/energy_uj" ]; then
        echo "energia $(cat "$z/energy_uj") $(cat "$z/max_energy_range_uj" 2>/dev/null || echo 0)"
        exit 0
      fi
    done
    falha "este processador não tem medidor de energia (RAPL)" 3 ;;
  discos)
    # só o que o Linux já sabe (udev): não fala com o disco
    exec lsblk -dnP -o NAME,TRAN,SERIAL ;;
  smart|temperatura)
    command -v smartctl >/dev/null 2>&1 || falha "o smartmontools não está instalado (sudo apt install smartmontools)" 3
    [ -b "/dev/$valor" ] || falha "o disco $valor não existe" 3
    # um disco por pedido; só o que o BrunOS usa (saúde, identificação e atributos)
    # -n standby: não acorda HD parado; sat = disco SATA atrás de um adaptador USB
    if [ "$acao" = smart ]; then o="-H -i -A"; else o="-A"; fi
    if [ "$modo" = sat ]; then o="$o -d sat"; fi
    smartctl -j -n standby $o "/dev/$valor" 2>/dev/null || true
    exit 0 ;;
  atualizacoes)
    if [ -x /usr/lib/update-notifier/apt-check ]; then
      echo "apt $(/usr/lib/update-notifier/apt-check 2>&1 | tail -n 1)"
    fi
    if [ -f /var/run/reboot-required ]; then echo "reiniciar"; fi
    echo "=== pacotes"
    apt list --upgradable 2>/dev/null | sed -n '2,301p' || true
    exit 0 ;;
  atualizar-linux|atualizar-app)
    command -v systemd-run >/dev/null 2>&1 || falha "este servidor não tem systemd-run" 3
    if systemctl is-active --quiet brunos-atualizar; then falha "já tem uma atualização rodando" 4; fi
    if [ "$acao" = atualizar-app ]; then
      pasta_runtipi >/dev/null || falha "não achei o runtipi-cli: em Configurações -> Energia, copie os comandos de atualização e cole no servidor" 3
    fi
    # solta do SSH: se a atualização reiniciar o Docker (e o BrunOS junto), ela continua
    systemd-run --unit=brunos-atualizar --collect --quiet --description="Atualização pedida pelo BrunOS" /usr/local/sbin/brunos-host executar "$acao" $valor $modo
    echo "iniciado"
    exit 0 ;;
  executar)
    set +e
    exec >"$LOG" 2>&1
    echo "inicio $(date +%s) $valor $modo $extra"
    if [ "$valor" = atualizar-linux ]; then
      # sem perguntas; mantém os arquivos de configuração; needrestart só lista (não reinicia serviços)
      export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l
      # um repositório extra quebrado não impede de instalar o resto (vira só um aviso)
      apt-get update
      u=$?
      apt-get -y --with-new-pkgs -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold upgrade
      s=$?
      if [ "$s" -eq 0 ] && [ "$u" -ne 0 ]; then echo "aviso-repositorio"; fi
    else
      d=$(pasta_runtipi) && cd "$d"
      s=$?
      if [ "$s" -ne 0 ]; then echo "não achei o runtipi-cli"; echo "fim 3"; exit 3; fi
      if [ "$extra" = sim ]; then
        # o mesmo backup do painel do Runtipi (pasta do app + app-data, em runtipi/backups)
        echo "backup-inicio"
        ./runtipi-cli app backup "$modo"
        b=$?
        if [ "$b" -ne 0 ]; then echo "backup-falhou $b"; echo "fim $b"; exit "$b"; fi
        echo "backup-ok"
      fi
      ./runtipi-cli app update "$modo"
      s=$?
    fi
    echo "fim $s"
    exit "$s" ;;
  atualizar-lojas)
    # o mesmo "atualizar lojas" das configurações do Runtipi: baixa a lista nova de apps de cada loja
    d=$(pasta_runtipi) || falha "não achei o runtipi-cli: em Configurações -> Energia, copie os comandos de atualização e cole no servidor" 3
    cd "$d"
    if ! r=$(timeout 150 ./runtipi-cli appstore update 2>&1); then
      echo "$r" | tail -n 3 >&2
      exit 1
    fi
    echo "lojas atualizadas"
    exit 0 ;;
  tamanho-backup)
    d=$(pasta_runtipi) || falha "não achei a pasta do Runtipi" 3
    id=${valor%%:*}
    loja=${valor#*:}
    t=0
    for p in "$d/apps/$loja/$id" "$d/app-data/$loja/$id"; do
      if [ -d "$p" ]; then
        n=$(timeout 50 du -sb "$p" 2>/dev/null | cut -f1)
        t=$((t + ${n:-0}))
      fi
    done
    echo "tamanho $t livre $(df -B1 --output=avail "$d" | tail -n 1 | tr -d ' ')"
    exit 0 ;;
  atualizar-script)
    command -v ssh-keygen >/dev/null 2>&1 || falha "falta o ssh-keygen no servidor (pacote openssh-client)" 3
    t=$(mktemp -d)
    trap 'rm -rf "$t"' EXIT
    head -c 262145 >"$t/novo"
    [ "$(wc -c <"$t/novo")" -le 262144 ] || falha "o script novo é grande demais" 3
    printf '%s' "$valor" | base64 -d >"$t/novo.sig" 2>/dev/null || falha "assinatura ilegível" 3
    printf 'brunos-release %s\n' "$CHAVE_ASSINATURA" >"$t/assinantes"
    ssh-keygen -Y verify -f "$t/assinantes" -I brunos-release -n brunos-host -s "$t/novo.sig" <"$t/novo" >/dev/null 2>&1 || falha "assinatura inválida: este script não foi publicado pelo BrunOS" 3
    nova=$(sed -n 's/^VERSAO=\([0-9][0-9]*\)$/\1/p' "$t/novo" | head -n 1)
    [ -n "$nova" ] && [ "$nova" -gt "$VERSAO" ] || falha "o script recebido não é mais novo que o atual (versão $VERSAO)" 3
    sh -n "$t/novo" || falha "o script novo tem erro de sintaxe" 3
    install -m 755 -o root -g root "$t/novo" /usr/local/sbin/brunos-host.novo
    mv -f /usr/local/sbin/brunos-host.novo /usr/local/sbin/brunos-host
    echo "script atualizado para a versão $nova"
    exit 0 ;;
  atualizacao-estado)
    if systemctl is-active --quiet brunos-atualizar; then echo "rodando"; else echo "parado"; fi
    echo "=== registro"
    # a primeira linha ("inicio <hora>") diz de qual atualização é o registro; depois, o fim dele
    head -n 1 "$LOG" 2>/dev/null || true
    tail -n 30 "$LOG" 2>/dev/null || true
    exit 0 ;;
  limite)
    for b in $(baterias); do
      fim=$b/charge_control_end_threshold
      ini=$b/charge_control_start_threshold
      [ -e "$fim" ] || continue
      if [ -e "$ini" ]; then
        # a carga volta a subir 5 pontos abaixo do limite; a ordem evita início >= fim
        if [ "$valor" -ge "$(cat "$fim")" ]; then
          echo "$valor" >"$fim" || falha "o notebook recusou o limite de $valor%" 1
          echo $((valor - 5)) >"$ini" 2>/dev/null || true
        else
          echo $((valor - 5)) >"$ini" 2>/dev/null || true
          echo "$valor" >"$fim" || falha "o notebook recusou o limite de $valor%" 1
        fi
      else
        echo "$valor" >"$fim" || falha "o notebook recusou o limite de $valor%" 1
      fi
      feito=1
      echo "limite $(cat "$fim")"
    done
    [ "$feito" = 1 ] || falha "este notebook não tem limite de carga" 3 ;;
  protecao)
    if [ "$valor" = on ]; then tipo=Long_Life; num=1; else tipo=Standard; num=0; fi
    for b in $(baterias); do
      t=$b/charge_types
      [ -e "$t" ] && grep -q Long_Life "$t" || continue
      echo "$tipo" >"$t" || falha "o notebook recusou a proteção de carga" 1
      feito=1
    done
    if [ "$feito" = 0 ]; then
      for c in /sys/bus/platform/drivers/ideapad_acpi/*/conservation_mode; do
        [ -e "$c" ] || continue
        echo "$num" >"$c" || falha "o notebook recusou a proteção de carga" 1
        feito=1
      done
    fi
    [ "$feito" = 1 ] || falha "este notebook não tem proteção de carga" 3
    echo "protecao $valor" ;;
esac
`

var (
	ErrNotConfigured = errors.New("o desligamento ainda não foi configurado (falta o endereço do servidor)")
	ErrOldSetup      = errors.New("o servidor ainda está com a configuração antiga: em Configurações → Energia, cole os comandos novos no servidor e clique em Testar")
)

type Target struct {
	Host string
	Port int
	User string
}

func (t Target) addr() string { return sshknown.Addr(t.Host, t.Port) }

type Runner struct {
	KeyFile string // chave (criada sob demanda)
	Known   *sshknown.Store

	mu     sync.Mutex
	signer ssh.Signer
}

func New(keyFile string, known *sshknown.Store) *Runner {
	return &Runner{KeyFile: keyFile, Known: known}
}

// key lê a chave atual ou cria uma nova (ED25519, gravada com permissão 0600).
func (r *Runner) key() (ssh.Signer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.signer != nil {
		return r.signer, nil
	}
	if b, err := os.ReadFile(r.KeyFile); err == nil {
		s, err := ssh.ParsePrivateKey(b)
		if err != nil {
			return nil, fmt.Errorf("a chave em %s está ilegível: %w", r.KeyFile, err)
		}
		r.signer = s
		return s, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, keyComment)
	if err != nil {
		return nil, err
	}
	if err := store.WriteFile(r.KeyFile, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	r.signer = s
	return s, nil
}

// HasKey diz se a chave atual já foi criada (sem criá-la).
func (r *Runner) HasKey() bool {
	_, err := os.Stat(r.KeyFile)
	return err == nil
}

// PublicKey devolve a chave pública atual no formato do authorized_keys (sem quebra de linha)
// e a impressão digital. Cria a chave se ainda não existir.
func (r *Runner) PublicKey() (line, fingerprint string, err error) {
	s, err := r.key()
	if err != nil {
		return "", "", err
	}
	pub := s.PublicKey()
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " " + keyComment, ssh.FingerprintSHA256(pub), nil
}

// AuthorizedLine é a linha completa para o authorized_keys do usuário dedicado.
func (r *Runner) AuthorizedLine() (string, error) {
	line, _, err := r.PublicKey()
	if err != nil {
		return "", err
	}
	return `restrict,command="` + ScriptPath + `" ` + line, nil
}

func (r *Runner) dial(ctx context.Context, t Target, trust string, signer ssh.Signer) (*ssh.Client, error) {
	if strings.TrimSpace(t.Host) == "" {
		return nil, ErrNotConfigured
	}
	if t.User == "" {
		t.User = DefaultUser
	}
	addr := t.addr()
	known, _ := r.Known.Load()
	keyOK := false
	check := r.Known.Callback(addr, trust, known)
	cfg := &ssh.ClientConfig{
		User:    t.User,
		Auth:    []ssh.AuthMethod{ssh.PublicKeys(signer)},
		Timeout: dialTimeout,
		HostKeyCallback: func(h string, a net.Addr, k ssh.PublicKey) error {
			err := check(h, a, k)
			keyOK = err == nil
			return err
		},
		HostKeyAlgorithms: sshknown.Algorithms(known, addr),
		ClientVersion:     "SSH-2.0-BrunOS",
	}
	d := net.Dialer{Timeout: dialTimeout}
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, sshknown.FriendlyDialError(err, addr)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = nc.SetDeadline(dl)
	}
	c, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		nc.Close()
		if keyOK && !sshknown.IsNetErr(err) {
			return nil, fmt.Errorf("o servidor recusou a chave do BrunOS para o usuário %s: cole os comandos de Configurações → Energia no servidor", t.User)
		}
		return nil, sshknown.FriendlyDialError(err, addr)
	}
	_ = nc.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// Test confere a conexão e a chave atual: conecta, autentica e fecha SEM abrir sessão
// (abrir sessão executaria o programa forçado). Passar prova que o servidor tem a linha da
// chave atual, ou seja, a configuração da versão SetupVersion.
// trust é a impressão digital do servidor confirmada pelo usuário na primeira vez.
func (r *Runner) Test(ctx context.Context, t Target, trust string) error {
	signer, err := r.key()
	if err != nil {
		return err
	}
	c, err := r.dial(ctx, t, trust, signer)
	if err != nil {
		return err
	}
	return c.Close()
}

// Run abre uma sessão e faz um pedido ao script do servidor (CmdShutdown, CmdLimit...).
// Só aceita servidor já confirmado.
func (r *Runner) Run(ctx context.Context, t Target, cmd string) (string, error) {
	out, errOut, err := r.exec(ctx, t, cmd, nil)
	text := strings.TrimSpace(string(out) + "\n" + errOut)
	if len(text) > 2000 {
		text = text[:2000]
	}
	if err != nil {
		var exit *ssh.ExitError
		if errors.As(err, &exit) {
			return text, fmt.Errorf("o comando saiu com código %d: %s", exit.ExitStatus(), text)
		}
		return text, err
	}
	return text, nil
}

// ErrOldScript indica que o servidor tem um brunos-host antigo, que não conhece o pedido.
var ErrOldScript = errors.New("o script do servidor é antigo: em Configurações → Energia, copie os comandos de atualização e cole no servidor")

// Output faz um pedido de leitura (ex.: CmdTailscale) e devolve a saída inteira (até 1 MB), sem
// misturar com os erros. Usa sempre a chave atual.
func (r *Runner) Output(ctx context.Context, t Target, cmd string) ([]byte, error) {
	return r.OutputInput(ctx, t, cmd, nil)
}

// OutputInput é o Output mandando input pela entrada padrão do script (o script novo no
// "atualizar-script").
func (r *Runner) OutputInput(ctx context.Context, t Target, cmd string, input []byte) ([]byte, error) {
	out, errOut, err := r.exec(ctx, t, cmd, input)
	if err != nil {
		var exit *ssh.ExitError
		if errors.As(err, &exit) {
			msg := strings.TrimSpace(errOut)
			if exit.ExitStatus() == 2 && strings.Contains(msg, "pedido inválido") {
				return nil, ErrOldScript
			}
			if msg == "" {
				msg = fmt.Sprintf("o comando saiu com código %d", exit.ExitStatus())
			}
			return nil, errors.New(msg)
		}
		return nil, err
	}
	return out, nil
}

// limitedBuffer guarda até max bytes e descarta o resto.
type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); room > 0 {
		if len(p) > room {
			b.Buffer.Write(p[:room])
		} else {
			b.Buffer.Write(p)
		}
	}
	return len(p), nil
}

func (r *Runner) exec(ctx context.Context, t Target, cmd string, input []byte) ([]byte, string, error) {
	signer, err := r.key()
	if err != nil {
		return nil, "", err
	}
	c, err := r.dial(ctx, t, "", signer)
	if err != nil {
		return nil, "", err
	}
	defer c.Close()
	sess, err := c.NewSession()
	if err != nil {
		return nil, "", fmt.Errorf("o servidor recusou abrir a sessão: %w", err)
	}
	defer sess.Close()
	// buffers separados: as duas cópias rodam ao mesmo tempo
	stdout, stderr := &limitedBuffer{max: maxOutput}, &limitedBuffer{max: 8 << 10}
	sess.Stdout, sess.Stderr = stdout, stderr
	if input != nil {
		sess.Stdin = bytes.NewReader(input)
	}
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }() // vira o SSH_ORIGINAL_COMMAND que o script confere
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = sess.Close()
		_ = c.Close()
		<-done // espera a sessão soltar os buffers
		err = ctx.Err()
	}
	return stdout.Bytes(), stderr.String(), err
}
