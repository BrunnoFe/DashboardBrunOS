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
// Por que systemctl e não "shutdown -h +1" para desligar: desde o systemd 257 (as versões novas
// do Ubuntu já usam) uma trava de desligamento ("block inhibitor") vale até para o root. O
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
	// primeira linha do registro, 0.12.2; 9 = "atualizar-lojas" (runtipi-cli appstore update), 0.12.3;
	// 10 = janela Discos: "discos-detalhe", "estado", "erros", "smart-teste", "velocidade", "ejetar" e o
	// "smart" com o andamento e o registro dos testes, 0.15.0; 11 = "montar", "desmontar", "rotulo",
	// "formatar" com a Senha de formatação, "discos-estado" e a trava de container no "ejetar", 0.16.0;
	// 12 = janela Loja: "iniciar-app", "parar-app", "desinstalar-app", "adicionar-loja" e "remover-loja",
	// todos pelo runtipi-cli, 0.18.0; 13 = a Senha de formatação passa a se chamar Senha de configuração,
	// com o comando "senha-config" (o antigo "senha-formatar" continua funcionando), 0.19.0;
	// 14 = "discos-estado" diz a data em que a Senha de configuração foi criada; instalar, editar a
	// instalação e reiniciar pela API do Runtipi ("info-app", "instalar-app", "reconfigurar-app",
	// "reiniciar-app") e os Ajustes ("ajustes-ler", "ajustes-gravar", com a Senha de configuração), 0.20.0).
	ScriptVersion = 14
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

// Script 10 (0.15.0): janela Discos. Leituras (não mudam nada no disco):
const (
	CmdDiskDetail = "discos-detalhe" // lsblk completo (partições, montagens, espaço), fstab (só UUID e pasta) e /dev/disk/by-id
	CmdDiskState  = "estado"         // "estado <disco>": dormindo ou acordado (hdparm -C, não acorda o disco)
	CmdDiskErrors = "erros"          // "erros <disco>": linhas do registro do kernel daquele disco (30 dias, até 200)
)

// Script 10 (0.15.0): ações seguras da janela Discos (não apagam nem gravam nada no disco).
const (
	CmdSelfTest = "smart-teste" // "smart-teste <disco> <sat|auto> <curto|longo>": o disco testa a si mesmo
	CmdSpeed    = "velocidade"  // "velocidade <disco>": lê 1 GiB direto do disco (dd para /dev/null)
	CmdEject    = "ejetar"      // "ejetar <disco>": só USB e só sem nada montado; sync + desliga o disco
	TestShort   = "curto"
	TestLong    = "longo"
)

// ReadCmds são os pedidos de leitura que o BrunOS pode fazer pelo HostOutput.
var ReadCmds = map[string]bool{CmdTailscale: true, CmdEnergy: true, CmdSmart: true, CmdTemps: true, CmdUpdates: true, CmdDisks: true,
	CmdVersion: true, CmdBackupSize: true, CmdDiskDetail: true, CmdDiskState: true, CmdDiskErrors: true, "discos-estado": true}

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
	case CmdDiskState, CmdDiskErrors:
		return len(f) == 2 && ValidDisk(f[1])
	}
	return len(f) == 1
}

// Script 11 (0.16.0): montar, desmontar, trocar o rótulo e formatar (decisão 54).
const (
	CmdMount      = "montar"        // "montar <uuid> <pasta>": /mnt/<pasta> + linha no fstab (marcada "# brunos")
	CmdUnmount    = "desmontar"     // "desmontar <pasta>": recusa se algum programa usa; tira só a linha do BrunOS
	CmdLabel      = "rotulo"        // "rotulo <uuid> <rótulo>": partição desmontada
	CmdFormat     = "formatar"      // "formatar <disco> <série> <fs> <rótulo>" + a Senha de configuração pela entrada padrão
	CmdDiskStatus = "discos-estado" // formatação rodando ou não, se a Senha de configuração existe, trava e o registro
)

// Sistemas de arquivos que o BrunOS sabe criar.
var FormatFS = map[string]bool{"ext4": true, "btrfs": true, "exfat": true, "ntfs": true}

var (
	uuidRe   = regexp.MustCompile(`^[A-Fa-f0-9-]{1,36}$`)
	folderRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	labelRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,16}$`)
	serialRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
)

func ValidUUID(s string) bool   { return uuidRe.MatchString(s) }
func ValidFolder(s string) bool { return folderRe.MatchString(s) }
func ValidSerial(s string) bool { return serialRe.MatchString(s) }

// ValidLabel confere o rótulo (no exFAT, até 11 letras).
func ValidLabel(s, fs string) bool { return labelRe.MatchString(s) && (fs != "exfat" || len(s) <= 11) }

// FormatAllowed confere o pedido de formatar (a senha vai pela entrada padrão, nunca aqui).
func FormatAllowed(cmd string) bool {
	f := strings.Fields(cmd)
	return len(f) == 5 && f[0] == CmdFormat && ValidDisk(f[1]) && ValidSerial(f[2]) && FormatFS[f[3]] && ValidLabel(f[4], f[3])
}

// DiskActAllowed confere um pedido de ação da janela Discos (o script confere de novo).
func DiskActAllowed(cmd string) bool {
	f := strings.Fields(cmd)
	switch {
	case len(f) == 2 && f[0] == CmdUnmount:
		return ValidFolder(f[1])
	case len(f) == 3 && f[0] == CmdMount:
		return ValidUUID(f[1]) && ValidFolder(f[2])
	case len(f) == 3 && f[0] == CmdLabel:
		return ValidUUID(f[1]) && labelRe.MatchString(f[2])
	case len(f) == 2:
		return (f[0] == CmdSpeed || f[0] == CmdEject) && ValidDisk(f[1])
	case len(f) == 4:
		return f[0] == CmdSelfTest && ValidDisk(f[1]) && (f[2] == ModeSAT || f[2] == ModeAuto) && (f[3] == TestShort || f[3] == TestLong)
	}
	return false
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

// Script 12 (0.18.0): janela Loja. Tudo pelo runtipi-cli, que é quem o Runtipi usa (ele fica sabendo).
const (
	CmdAppStart     = "iniciar-app"     // "iniciar-app <app>:<loja>": runtipi-cli app start
	CmdAppStop      = "parar-app"       // "parar-app <app>:<loja>": runtipi-cli app stop
	CmdAppUninstall = "desinstalar-app" // "desinstalar-app <app>:<loja>": runtipi-cli app uninstall
	CmdStoreAdd     = "adicionar-loja"  // "adicionar-loja <nome> <endereço https>": runtipi-cli appstore add
	CmdStoreRemove  = "remover-loja"    // "remover-loja <nome>": runtipi-cli appstore remove
	// 0.20.0 (script 14): pela API do Runtipi, com a chave do runtipi-cli, que não sai do servidor
	CmdAppInfo     = "info-app"         // "info-app <app>:<loja>": estado e respostas da instalação (JSON)
	CmdAppInstall  = "instalar-app"     // "instalar-app <app>:<loja>" + formulário (JSON) pela entrada padrão
	CmdAppReconfig = "reconfigurar-app" // "reconfigurar-app <app>:<loja>" + respostas novas (JSON); reinicia
	CmdAppRestart  = "reiniciar-app"    // "reiniciar-app <app>:<loja>"
	CmdAppCfgRead  = "ajustes-ler"      // "ajustes-ler <app>:<loja>": user-config/<loja>/<app>/docker-compose.yml
	CmdAppCfgWrite = "ajustes-gravar"   // "ajustes-gravar <app>:<loja>" + Senha de configuração e o arquivo novo
)

var (
	storeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	storeURLRe  = regexp.MustCompile(`^https://[A-Za-z0-9][A-Za-z0-9.-]*(/[A-Za-z0-9._-]+)+$`)
)

// ValidStoreName diz se o nome serve para uma loja do Runtipi (minúsculas, números e traço).
func ValidStoreName(s string) bool { return storeNameRe.MatchString(s) }

// ValidStoreURL aceita só endereço https de um repositório, sem usuário, senha, porta, "?" nem "..".
func ValidStoreURL(s string) bool {
	return len(s) <= 200 && storeURLRe.MatchString(s) && !strings.Contains(s, "..")
}

// StoreActAllowed confere um pedido da janela Loja inteiro (o script confere de novo).
func StoreActAllowed(cmd string) bool {
	f := strings.Fields(cmd)
	switch {
	case len(f) == 2 && (f[0] == CmdAppStart || f[0] == CmdAppStop || f[0] == CmdAppUninstall || f[0] == CmdAppInfo ||
		f[0] == CmdAppInstall || f[0] == CmdAppReconfig || f[0] == CmdAppRestart || f[0] == CmdAppCfgRead || f[0] == CmdAppCfgWrite):
		return ValidApp(f[1])
	case len(f) == 2 && f[0] == CmdStoreRemove:
		return ValidStoreName(f[1])
	case len(f) == 3 && f[0] == CmdStoreAdd:
		return ValidStoreName(f[1]) && ValidStoreURL(f[2])
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
# brunos-host 14: o único programa que a chave do BrunOS pode rodar neste servidor.
# Aceita só: desligar | limite <50-100> | protecao <on|off> | tailscale | energia | atualizacoes |
# discos | smart <disco> <sat|auto> | temperatura <disco> <sat|auto> | atualizar-lojas | atualizar-linux |
# atualizar-app <app>:<loja> <sim|nao> | atualizacao-estado | tamanho-backup <app>:<loja> |
# discos-detalhe | estado <disco> | erros <disco> | smart-teste <disco> <sat|auto> <curto|longo> |
# velocidade <disco> | ejetar <disco> | montar <uuid> <pasta> | desmontar <pasta> |
# rotulo <uuid> <rotulo> | formatar <disco> <serie> <ext4|btrfs|exfat|ntfs> <rotulo> | discos-estado |
# iniciar-app <app>:<loja> | parar-app <app>:<loja> | desinstalar-app <app>:<loja> |
# adicionar-loja <nome> <https://...> | remover-loja <nome> | info-app <app>:<loja> |
# instalar-app <app>:<loja> | reconfigurar-app <app>:<loja> | reiniciar-app <app>:<loja> |
# ajustes-ler <app>:<loja> | ajustes-gravar <app>:<loja> (Senha de configuração + arquivo pela entrada padrão) |
# atualizar-script <assinatura> | versao. Recusa todo o resto.
# Só pelo Terminal, com sudo (nunca pela chave do BrunOS): senha-config | esquecer-senha-config (os nomes
# antigos senha-formatar | esquecer-senha-formatar continuam valendo).
# Formatar e desinstalar app só com a Senha de configuração (pela entrada padrão), conferida AQUI, com trava
# depois de 5 erros. Formatar, além disso, só depois de conferir de novo o disco: número de série, nada
# montado em lugar nenhum (nem dentro de containers), não é o disco do sistema, não é RAID/LVM/ZFS e não
# está no /etc/fstab.
# As atualizações rodam soltas pelo systemd-run (seguem mesmo se o BrunOS reiniciar), registradas em
# /var/log/brunos-atualizar.log. O script só se troca por outro assinado pela chave de publicação do
# BrunOS (abaixo) e de versão maior. Instalado pelos comandos de Configurações -> Energia do BrunOS.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
VERSAO=14
CHAVE_ASSINATURA='` + ReleasePublicKey + `'
via_ssh=0
if [ "$(id -u)" -ne 0 ]; then
  # chamado pelo SSH (usuário brunos-power): o pedido vem em SSH_ORIGINAL_COMMAND
  via_ssh=1
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
# disco inteiro: sda, sdab, nvme0n1 (nunca partição, caminho ou curinga)
disco_ok() {
  case "$1" in sd[a-z]|sd[a-z][a-z]|nvme[0-9]n1) return 0 ;; esac
  return 1
}
# palavra só com os caracteres permitidos e até N letras: so_com <texto> <caracteres> <máximo>
so_com() {
  case "$1" in ""|*[!$2]*) return 1 ;; esac
  [ ${#1} -le "$3" ]
}
uuid_ok() { so_com "$1" "A-Fa-f0-9-" 36; }
pasta_ok() { so_com "$1" "A-Za-z0-9_-" 32; }
rotulo_ok() { so_com "$1" "A-Za-z0-9_-" 16; }
serie_ok() { so_com "$1" "A-Za-z0-9_.:-" 64; }
# nome de loja do Runtipi: minúsculas, números e traço (nunca começando com traço)
loja_ok() {
  case "$1" in -*) return 1 ;; esac
  so_com "$1" "a-z0-9-" 32
}
# endereço de loja: só https://servidor/caminho, sem usuário, senha, porta, "?" nem ".."
url_ok() {
  case "$1" in https://[A-Za-z0-9]*/?*) ;; *) return 1 ;; esac
  case "${1#https://}" in *:*|*//*|*..*|*/) return 1 ;; esac
  so_com "$1" "A-Za-z0-9._/:-" 200
}
if [ $# -lt 1 ] || [ $# -gt 5 ]; then falha "pedido inválido"; fi
acao=$1
valor=${2:-}
modo=${3:-}
extra=${4:-}
extra2=${5:-}
case "$acao" in
  montar)
    [ $# -eq 3 ] || falha "pedido inválido"
    uuid_ok "$valor" || falha "UUID inválido"
    pasta_ok "$modo" || falha "nome de pasta inválido (use letras, números, - e _)" ;;
  desmontar)
    [ $# -eq 2 ] || falha "pedido inválido"
    pasta_ok "$valor" || falha "nome de pasta inválido" ;;
  rotulo)
    [ $# -eq 3 ] || falha "pedido inválido"
    uuid_ok "$valor" || falha "UUID inválido"
    rotulo_ok "$modo" || falha "rótulo inválido (até 16 letras, números, - e _)" ;;
  formatar|formatar-executar)
    [ $# -eq 5 ] || falha "pedido inválido"
    disco_ok "$valor" || falha "disco inválido"
    serie_ok "$modo" || falha "número de série inválido"
    case "$extra" in ext4|btrfs|exfat|ntfs) ;; *) falha "sistema de arquivos inválido" ;; esac
    rotulo_ok "$extra2" || falha "rótulo inválido (até 16 letras, números, - e _)"
    if [ "$extra" = exfat ] && [ ${#extra2} -gt 11 ]; then falha "no exFAT o rótulo tem no máximo 11 letras"; fi
    if [ "$acao" = formatar-executar ]; then
      # só o próprio script, como root, dentro do systemd; nunca pelo SSH
      [ "$(id -u)" -eq 0 ] && [ -n "${INVOCATION_ID:-}" ] || falha "pedido inválido"
    fi ;;
  discos-estado) [ $# -eq 1 ] || falha "pedido inválido" ;;
  senha-config|esquecer-senha-config|senha-formatar|esquecer-senha-formatar)
    [ $# -eq 1 ] || falha "pedido inválido"
    # nunca pela chave do BrunOS: quem invadisse o BrunOS cadastraria a senha dele
    [ "$via_ssh" = 0 ] || falha "a Senha de configuração só pode ser criada ou apagada no Terminal do servidor, com sudo" ;;
  versao) [ $# -eq 1 ] || falha "pedido inválido"; echo "brunos-host $VERSAO"; exit 0 ;;
  desligar|tailscale|energia|atualizacoes|discos|discos-detalhe|atualizar-linux|atualizacao-estado|atualizar-lojas) [ $# -eq 1 ] || falha "pedido inválido" ;;
  estado|erros|velocidade|ejetar)
    [ $# -eq 2 ] || falha "pedido inválido"
    disco_ok "$valor" || falha "disco inválido" ;;
  smart-teste)
    [ $# -eq 4 ] || falha "pedido inválido"
    disco_ok "$valor" || falha "disco inválido"
    case "$modo" in sat|auto) ;; *) falha "modo inválido (use sat ou auto)" ;; esac
    case "$extra" in curto|longo) ;; *) falha "teste inválido (use curto ou longo)" ;; esac ;;
  atualizar-app)
    [ $# -eq 3 ] || falha "pedido inválido"
    app_ok "$valor" || falha "app inválido"
    case "$modo" in sim|nao) ;; *) falha "pedido inválido" ;; esac ;;
  tamanho-backup|iniciar-app|parar-app|desinstalar-app|info-app|instalar-app|reconfigurar-app|reiniciar-app|ajustes-ler|ajustes-gravar)
    [ $# -eq 2 ] || falha "pedido inválido"
    app_ok "$valor" || falha "app inválido" ;;
  adicionar-loja)
    [ $# -eq 3 ] || falha "pedido inválido"
    loja_ok "$valor" || falha "nome de loja inválido (use letras minúsculas, números e -)"
    url_ok "$modo" || falha "endereço de loja inválido (use https://...)" ;;
  remover-loja)
    [ $# -eq 2 ] || falha "pedido inválido"
    loja_ok "$valor" || falha "nome de loja inválido" ;;
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
    disco_ok "$valor" || falha "disco inválido"
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
LOGD=/var/log/brunos-discos.log
SENHA=/etc/brunos-host/formatar
TRAVA=/etc/brunos-host/formatar.trava
# disco inteiro e as partições dele (nomes do /sys)
partes() {
  echo "$1"
  for p in /sys/block/"$1"/"$1"*; do
    if [ -e "$p" ]; then echo "${p##*/}"; fi
  done
}
# o disco tem a raiz, o /boot ou a swap (inclusive por baixo de LVM/criptografia)
e_sistema() {
  for s in "$(findmnt -no SOURCE / 2>/dev/null)" "$(findmnt -no SOURCE /boot 2>/dev/null)" $(swapon --show=NAME --noheadings 2>/dev/null); do
    [ -n "$s" ] || continue
    if lsblk -nso NAME "$s" 2>/dev/null | grep -qx "$1"; then return 0; fi
  done
  return 1
}
# processos (do servidor ou de qualquer container) que ainda têm o disco montado. Vazio = livre. Olha
# o número do dispositivo em todos os /proc/*/mountinfo: um container guarda a própria cópia da
# montagem, e o lsblk do servidor não mostra isso.
preso() {
  for p in $(partes "$1"); do
    mm=$(cat "/sys/class/block/$p/dev" 2>/dev/null) || continue
    for f in $(grep -ls " $mm " /proc/[0-9]*/mountinfo 2>/dev/null); do
      pid=${f#/proc/}
      cat "/proc/${pid%%/*}/comm" 2>/dev/null || true
    done
  done | sort -u | tr '\n' ' ' | sed 's/ $//'
}
# Senha de configuração: resumo scrypt (sal aleatório, n=2^15) pelo python3; a senha vem pela entrada padrão
PY_GERA='import hashlib,os,sys,datetime
s=sys.stdin.buffer.read();sal=os.urandom(16)
h=hashlib.scrypt(s,salt=sal,n=32768,r=8,p=1,maxmem=67108864,dklen=32)
print("scrypt$32768$8$1$%s$%s$%s"%(sal.hex(),h.hex(),datetime.datetime.now().isoformat(timespec="seconds")))'
PY_CONFERE='import hashlib,hmac,sys
f=open(sys.argv[1]).read().strip().split("$")
s=sys.stdin.buffer.read()
h=hashlib.scrypt(s,salt=bytes.fromhex(f[4]),n=int(f[1]),r=int(f[2]),p=int(f[3]),maxmem=67108864,dklen=32)
sys.exit(0 if hmac.compare_digest(h.hex(),f[5]) else 1)'
le_senha() { # pede uma senha no Terminal sem mostrar o que é digitado
  printf '%s' "$1" >&2
  stty -echo 2>/dev/null || true
  IFS= read -r s || s=""
  stty echo 2>/dev/null || true
  echo >&2
  printf '%s' "$s"
}
# Confere a Senha de configuração, que protege as ações sensíveis do servidor (formatar um disco,
# desinstalar um app). Ela vem pela entrada padrão (uma linha), nunca na linha de comando. Os erros contam
# juntos para todas essas ações: 5 erros = tudo bloqueado por 1 hora. Uso: confere_senha <formatar|desinstalar>
confere_senha() {
  [ -f "$SENHA" ] || falha "ainda não existe Senha de configuração: crie no Terminal do servidor com sudo brunos-host senha-config" 5
  command -v python3 >/dev/null 2>&1 || falha "falta o python3 no servidor" 3
  agora=$(date +%s)
  n=0; ate=0
  if [ -f "$TRAVA" ]; then read -r n ate <"$TRAVA" || true; fi
  case "$n$ate" in *[!0-9]*|"") n=0; ate=0 ;; esac
  if [ "$ate" -gt "$agora" ]; then
    falha "$1 está bloqueado até $(date -d "@$ate" +%H:%M) por causa de senhas erradas" 6
  fi
  IFS= read -r senha || senha=""
  if ! printf '%s' "$senha" | python3 -c "$PY_CONFERE" "$SENHA"; then
    n=$((n + 1))
    if [ "$n" -ge 5 ]; then
      echo "0 $((agora + 3600))" >"$TRAVA"
      falha "Senha de configuração errada 5 vezes: $1 ficou bloqueado por 1 hora" 6
    fi
    echo "$n 0" >"$TRAVA"
    falha "Senha de configuração errada (tentativa $n de 5)" 7
  fi
  rm -f "$TRAVA"
  senha=""
}
# Chave de acesso à API do Runtipi, igual à do runtipi-cli (JWT HS256, sub "cli"), válida por 5 minutos.
# O segredo vem pela entrada padrão (nunca na linha de comando, que outros usuários podem ver).
PY_JWT='import base64,hashlib,hmac,json,sys,time
k=sys.stdin.read().strip().encode()
b=lambda x: base64.urlsafe_b64encode(x).rstrip(b"=").decode()
n=int(time.time())
h=b(json.dumps({"alg":"HS256","typ":"JWT"},separators=(",",":")).encode())
p=b(json.dumps({"sub":"cli","iat":n,"nbf":n-5,"exp":n+300},separators=(",",":")).encode())
print(h+"."+p+"."+b(hmac.new(k,(h+"."+p).encode(),hashlib.sha256).digest()))'
# api_runtipi <método> <caminho>: chama a API do Runtipi deste servidor como o runtipi-cli faz (0.20.0:
# instalar, editar a instalação e reiniciar pelo BrunOS). O corpo (JSON) vem pela entrada padrão; a
# resposta sai na saída padrão. A chave é a do CLI: no Runtipi 4.12+ fica dentro do container (o
# runtipi-cli lê com docker exec); antes, era o JWT_SECRET do .env. Chave e token só passam por arquivos
# temporários com permissão 600, nunca pela linha de comando.
api_runtipi() {
  d=$(pasta_runtipi) || falha "não achei o runtipi-cli: em Configurações -> Energia, copie os comandos de atualização e cole no servidor" 3
  command -v curl >/dev/null 2>&1 || falha "falta o curl no servidor (sudo apt install curl)" 3
  command -v python3 >/dev/null 2>&1 || falha "falta o python3 no servidor" 3
  ip=$(sed -n 's/^INTERNAL_IP=//p' "$d/.env" 2>/dev/null | head -n 1 | tr -d '"\r ')
  porta=$(sed -n 's/^NGINX_PORT=//p' "$d/.env" 2>/dev/null | head -n 1 | tr -d '"\r ')
  [ -n "$ip" ] || ip=localhost
  [ -n "$porta" ] || porta=80
  so_com "$ip" "A-Za-z0-9.-" 100 && so_com "$porta" "0-9" 5 || falha "endereço do Runtipi estranho no .env" 3
  v=$(tr -d 'v \r\n' <"$d/VERSION" 2>/dev/null || true)
  maj=${v%%.*}
  min=${v#*.}
  min=${min%%.*}
  case "$maj$min" in ""|*[!0-9]*) falha "não deu para ler a versão do Runtipi" 3 ;; esac
  umask 077
  t=$(mktemp -d)
  if [ "$maj" -gt 4 ] || { [ "$maj" -eq 4 ] && [ "$min" -ge 12 ]; }; then
    timeout 20 docker exec runtipi cat /data/.secrets/cli-secret >"$t/k" 2>/dev/null || { rm -rf "$t"; falha "não deu para ler a chave do Runtipi (o container runtipi está rodando?)" 3; }
  elif [ "$maj" -eq 4 ]; then
    sed -n 's/^JWT_SECRET=//p' "$d/.env" | head -n 1 | tr -d '"\r' >"$t/k"
  else
    rm -rf "$t"
    falha "esta versão do Runtipi ($v) não é aceita: atualize o Runtipi" 3
  fi
  [ -s "$t/k" ] || { rm -rf "$t"; falha "a chave do Runtipi está vazia" 3; }
  tok=$(python3 -c "$PY_JWT" <"$t/k")
  rm -f "$t/k"
  printf 'header = "Authorization: Bearer %s"\n' "$tok" >"$t/cfg"
  tok=""
  head -c 65537 >"$t/corpo"
  [ "$(wc -c <"$t/corpo")" -le 65536 ] || { rm -rf "$t"; falha "pedido grande demais" 2; }
  if [ -s "$t/corpo" ]; then set -- "$1" "$2" -H "Content-Type: application/json" --data-binary "@$t/corpo"; else set -- "$1" "$2"; fi
  m=$1
  c=$2
  shift 2
  cod=$(curl -sS -m 120 -K "$t/cfg" -X "$m" "$@" -o "$t/resp" -w '%{http_code}' "http://$ip:$porta/api$c" 2>"$t/err") || cod=000
  if [ "$cod" = 000 ]; then
    e=$(head -c 300 "$t/err")
    rm -rf "$t"
    falha "o Runtipi não respondeu em $ip:$porta ($e)" 1
  fi
  if [ "$cod" -ge 400 ]; then
    head -c 2000 "$t/resp" >&2
    echo >&2
    rm -rf "$t"
    exit 1
  fi
  head -c 1000000 "$t/resp"
  rm -rf "$t"
}
# tudo o que precisa ser verdade antes de apagar um disco (conferido de novo logo antes de apagar)
checa_formatar() { # <disco> <série> <fs>
  [ -b "/dev/$1" ] || falha "o disco $1 não existe" 3
  s=$(lsblk -dnro SERIAL "/dev/$1" 2>/dev/null)
  [ -n "$s" ] && [ "$s" = "$2" ] || falha "o número de série de /dev/$1 não confere (o disco mudou de nome?): nada foi apagado" 3
  if e_sistema "$1"; then falha "este é o disco do sistema: nada foi apagado" 3; fi
  u=$(preso "$1")
  [ -z "$u" ] || falha "o disco ainda está montado (em uso por: $u). Desmonte, reinicie o BrunOS e pare os apps que usam o disco: nada foi apagado" 3
  if lsblk -nro FSTYPE "/dev/$1" 2>/dev/null | grep -qE 'linux_raid_member|LVM2_member|zfs_member|crypto_LUKS'; then
    falha "o disco faz parte de RAID, LVM, ZFS ou criptografia: nada foi apagado" 3
  fi
  for h in /sys/block/"$1"/holders/* /sys/block/"$1"/"$1"*/holders/*; do
    if [ -e "$h" ]; then falha "o disco está em uso por baixo (RAID, LVM ou criptografia): nada foi apagado" 3; fi
  done
  for u in $(lsblk -nro UUID "/dev/$1" 2>/dev/null); do
    if grep -qs "$u" /etc/fstab; then falha "uma partição deste disco está no /etc/fstab: desmonte pelo BrunOS antes (nada foi apagado)" 3; fi
  done
  if grep -qsE "^/dev/$1" /etc/fstab; then falha "este disco está no /etc/fstab: nada foi apagado" 3; fi
  if command -v fuser >/dev/null 2>&1; then
    for p in $(partes "$1"); do
      if fuser -s "/dev/$p" 2>/dev/null; then falha "um programa está usando o disco agora: nada foi apagado" 3; fi
    done
  fi
  case "$3" in
    ext4) t=mkfs.ext4; pk=e2fsprogs ;;
    btrfs) t=mkfs.btrfs; pk=btrfs-progs ;;
    exfat) t=mkfs.exfat; pk=exfatprogs ;;
    ntfs) t=mkfs.ntfs; pk=ntfs-3g ;;
  esac
  for f in "$t:$pk" sgdisk:gdisk wipefs:util-linux partprobe:parted; do
    command -v "${f%%:*}" >/dev/null 2>&1 || falha "falta o ${f%%:*} no servidor: instale com sudo apt install ${f#*:}" 3
  done
}
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
    # -c e -l selftest (script 10): andamento do teste do próprio disco e os últimos resultados
    if [ "$acao" = smart ]; then o="-H -i -A -c -l selftest"; else o="-A"; fi
    if [ "$modo" = sat ]; then o="$o -d sat"; fi
    smartctl -j -n standby $o "/dev/$valor" 2>/dev/null || true
    exit 0 ;;
  discos-detalhe)
    # só o que o Linux já sabe (udev e montagens): não fala com o disco
    c=NAME,PATH,TYPE,SIZE,MODEL,VENDOR,SERIAL,WWN,REV,TRAN,ROTA,RM,FSTYPE,LABEL,UUID,PARTUUID,FSUSED,FSAVAIL,PTTYPE
    echo "=== lsblk"
    lsblk -J -b -o "$c,MOUNTPOINTS" 2>/dev/null || lsblk -J -b -o "$c,MOUNTPOINT"
    echo "=== fstab"
    # de cada linha, só o que identifica o disco e a pasta (opções e o resto ficam de fora)
    awk '$1 !~ /^#/ && NF >= 2 && $1 ~ /^(UUID=|LABEL=|PARTUUID=|\/dev\/)/ { print $1, $2 }' /etc/fstab 2>/dev/null || true
    echo "=== by-id"
    for l in /dev/disk/by-id/*; do
      [ -L "$l" ] || continue
      echo "${l##*/} $(readlink -f "$l")"
    done
    exit 0 ;;
  estado)
    [ -b "/dev/$valor" ] || falha "o disco $valor não existe" 3
    # NVMe não para de girar como um HD: está sempre pronto
    case "$valor" in nvme*) echo "estado acordado"; exit 0 ;; esac
    command -v hdparm >/dev/null 2>&1 || { echo "estado desconhecido sem-hdparm"; exit 0; }
    # -C só pergunta o estado: não acorda o disco
    r=$(timeout 15 hdparm -C "/dev/$valor" 2>/dev/null | sed -n 's/.*drive state is: *//p' | head -n 1)
    case "$r" in
      *standby*|*sleeping*) echo "estado dormindo" ;;
      *active*|*idle*) echo "estado acordado" ;;
      *) echo "estado desconhecido" ;;
    esac
    exit 0 ;;
  erros)
    [ -b "/dev/$valor" ] || falha "o disco $valor não existe" 3
    command -v journalctl >/dev/null 2>&1 || falha "este servidor não tem o journalctl" 3
    # a porta USB do disco (ex.: 2-1) aparece nas linhas de reset e de desconexão do USB
    porta=$(readlink -f "/sys/block/$valor/device" 2>/dev/null | tr '/' '\n' | grep -E '^[0-9]+-[0-9]+(\.[0-9]+)*$' | tail -n 1)
    re="\\b${valor}p?[0-9]*\\b"
    if [ -n "$porta" ]; then re="$re|usb $porta:"; fi
    timeout 40 journalctl -k --since=-30d --no-pager -q -o short-iso 2>/dev/null | grep -E "$re" \
      | grep -iE 'error|fail|reset|disconnect|offline|timeout|timed out|abort|critical|sense key|i/o' | tail -n 200 || true
    exit 0 ;;
  smart-teste)
    command -v smartctl >/dev/null 2>&1 || falha "o smartmontools não está instalado (sudo apt install smartmontools)" 3
    [ -b "/dev/$valor" ] || falha "o disco $valor não existe" 3
    if [ "$extra" = curto ]; then t=short; else t=long; fi
    o=""
    if [ "$modo" = sat ]; then o="-d sat"; fi
    # o disco testa a si mesmo em segundo plano; o andamento vem no pedido "smart"
    r=$(LC_ALL=C timeout 60 smartctl -t "$t" $o "/dev/$valor" 2>&1 || true)
    case "$r" in
      *"has begun"*) echo "teste iniciado" ;;
      *) echo "$r" | tail -n 4 >&2; exit 1 ;;
    esac
    exit 0 ;;
  velocidade)
    [ -b "/dev/$valor" ] || falha "o disco $valor não existe" 3
    command -v flock >/dev/null 2>&1 || falha "falta o flock no servidor (pacote util-linux)" 3
    exec 9>/run/brunos-velocidade.lock
    flock -n 9 || falha "já tem um teste de velocidade rodando" 4
    # lê 1 GiB do começo do disco, direto do disco (sem a memória do Linux no meio); não grava nada
    r=$(LC_ALL=C timeout 150 dd if="/dev/$valor" of=/dev/null bs=1M count=1024 iflag=direct 2>&1 | tail -n 1)
    case "$r" in
      *copied*) echo "velocidade $r" ;;
      "") falha "o disco demorou mais de 150 s para ler 1 GiB" 1 ;;
      *) falha "$r" 1 ;;
    esac
    exit 0 ;;
  ejetar)
    [ -b "/dev/$valor" ] || falha "o disco $valor não existe" 3
    [ "$(lsblk -dno TRAN "/dev/$valor" 2>/dev/null | tr -d ' ')" = usb ] || falha "só dá para ejetar disco USB" 3
    if lsblk -nro MOUNTPOINT "/dev/$valor" 2>/dev/null | grep -q .; then
      falha "o disco ainda está montado: desmonte antes de ejetar" 3
    fi
    for h in /sys/block/"$valor"/holders/* /sys/block/"$valor"/"$valor"*/holders/*; do
      if [ -e "$h" ]; then falha "o disco está em uso (RAID, LVM ou criptografia): não dá para ejetar" 3; fi
    done
    # script 11: um container (o próprio BrunOS ou um app) pode ainda segurar uma cópia da montagem
    u=$(preso "$valor")
    [ -z "$u" ] || falha "o disco ainda está montado dentro de um app (em uso por: $u): reinicie o BrunOS e pare os apps que usam o disco antes de ejetar" 3
    [ -w "/sys/block/$valor/device/delete" ] || falha "este disco não pode ser ejetado por aqui" 3
    sync
    echo 1 >"/sys/block/$valor/device/delete" || falha "o Linux recusou ejetar o disco" 1
    echo "ejetado"
    exit 0 ;;
  montar)
    dev=$(blkid -U "$valor" 2>/dev/null) || falha "não achei nenhuma partição com esse UUID" 3
    case "$dev" in /dev/*) ;; *) falha "não achei nenhuma partição com esse UUID" 3 ;; esac
    d=$(lsblk -nso NAME "$dev" 2>/dev/null | tail -n 1)
    if [ -n "$d" ] && e_sistema "$d"; then falha "essa partição é do disco do sistema" 3; fi
    fs=$(blkid -o value -s TYPE "$dev" 2>/dev/null || true)
    case "$fs" in
      ext4|ext3|ext2|btrfs|xfs) op="defaults,nofail" ;;
      ntfs) fs=ntfs3; op="uid=1000,gid=1000,nofail" ;;
      exfat|vfat) op="uid=1000,gid=1000,nofail" ;;
      *) falha "não sei montar o sistema de arquivos \"$fs\"" 3 ;;
    esac
    if lsblk -nro MOUNTPOINT "$dev" 2>/dev/null | grep -q .; then falha "essa partição já está montada" 3; fi
    if grep -qs "$valor" /etc/fstab; then falha "essa partição já tem uma linha no /etc/fstab" 3; fi
    alvo="/mnt/$modo"
    criou=0
    if [ -e "$alvo" ]; then
      if mountpoint -q "$alvo"; then falha "já tem um disco montado em $alvo" 3; fi
      [ -d "$alvo" ] && [ -z "$(ls -A "$alvo" 2>/dev/null)" ] || falha "a pasta $alvo já existe e não está vazia: escolha outro nome" 3
    else
      mkdir "$alvo"
      criou=1
    fi
    copia="/etc/fstab.brunos-$(date +%Y%m%d-%H%M%S)"
    cp -p /etc/fstab "$copia"
    printf 'UUID=%s %s %s %s 0 2 # brunos\n' "$valor" "$alvo" "$fs" "$op" >>/etc/fstab
    if ! r=$(mount "$alvo" 2>&1); then
      # volta tudo como estava
      cp -p "$copia" /etc/fstab
      if [ "$criou" = 1 ]; then rmdir "$alvo" 2>/dev/null || true; fi
      falha "não deu para montar (o /etc/fstab voltou como estava): $r" 1
    fi
    systemctl daemon-reload 2>/dev/null || true
    echo "montado $alvo $fs"
    exit 0 ;;
  desmontar)
    alvo="/mnt/$valor"
    mountpoint -q "$alvo" || falha "não há disco montado em $alvo" 3
    src=$(findmnt -no SOURCE "$alvo" 2>/dev/null)
    d=$(lsblk -nso NAME "$src" 2>/dev/null | tail -n 1)
    if [ -n "$d" ] && e_sistema "$d"; then falha "esse é o disco do sistema" 3; fi
    command -v fuser >/dev/null 2>&1 || falha "falta o fuser no servidor: instale com sudo apt install psmisc" 3
    u=$(fuser -m "$alvo" 2>/dev/null | tr -s ' ' '\n' | sed 's/[^0-9]//g' | grep . | while read -r p; do cat "/proc/$p/comm" 2>/dev/null; done | sort -u | tr '\n' ' ' | sed 's/ $//')
    [ -z "$u" ] || falha "o disco está em uso por: $u. Feche ou pare esses programas (no Runtipi, pare o app) e tente de novo" 4
    r=$(umount "$alvo" 2>&1) || falha "o Linux recusou desmontar: $r" 1
    if grep -qsE "[[:space:]]$alvo[[:space:]].*# brunos\$" /etc/fstab; then
      cp -p /etc/fstab "/etc/fstab.brunos-$(date +%Y%m%d-%H%M%S)"
      grep -vE "[[:space:]]$alvo[[:space:]].*# brunos\$" /etc/fstab >/etc/fstab.brunos-novo || true
      cat /etc/fstab.brunos-novo >/etc/fstab
      rm -f /etc/fstab.brunos-novo
      systemctl daemon-reload 2>/dev/null || true
      echo "fstab tirado"
    elif grep -qsE "[[:space:]]$alvo[[:space:]]" /etc/fstab; then
      echo "fstab ficou"
    fi
    rmdir "$alvo" 2>/dev/null || true
    # quem ainda segura uma cópia da montagem (o BrunOS e apps com pasta deste disco)
    if [ -n "$d" ]; then echo "preso $(preso "$d")"; fi
    echo "desmontado $alvo"
    exit 0 ;;
  rotulo)
    dev=$(blkid -U "$valor" 2>/dev/null) || falha "não achei nenhuma partição com esse UUID" 3
    case "$dev" in /dev/*) ;; *) falha "não achei nenhuma partição com esse UUID" 3 ;; esac
    if lsblk -nro MOUNTPOINT "$dev" 2>/dev/null | grep -q .; then falha "desmonte a partição antes de trocar o rótulo" 3; fi
    case "$(blkid -o value -s TYPE "$dev" 2>/dev/null || true)" in
      ext4|ext3|ext2) r=$(e2label "$dev" "$modo" 2>&1) ;;
      btrfs) r=$(btrfs filesystem label "$dev" "$modo" 2>&1) ;;
      ntfs) r=$(ntfslabel "$dev" "$modo" 2>&1) ;;
      exfat) r=$(exfatlabel "$dev" "$modo" 2>&1) ;;
      vfat) r=$(fatlabel "$dev" "$modo" 2>&1) ;;
      *) falha "não sei trocar o rótulo deste sistema de arquivos" 3 ;;
    esac || falha "não deu para trocar o rótulo: $r" 1
    echo "rotulo $modo"
    exit 0 ;;
  senha-config|esquecer-senha-config|senha-formatar|esquecer-senha-formatar)
    [ "${SUDO_USER:-}" != brunos-power ] || falha "a Senha de configuração só pode ser criada ou apagada no Terminal do servidor, com sudo"
    case "$acao" in esquecer-*)
      rm -f "$SENHA" "$TRAVA"
      echo "Senha de configuração apagada. Formatar e desinstalar apps pelo BrunOS ficam desligados até você criar outra."
      exit 0 ;;
    esac
    [ -t 0 ] || falha "rode no Terminal do servidor: sudo brunos-host senha-config"
    command -v python3 >/dev/null 2>&1 || falha "falta o python3 no servidor" 3
    install -d -m 700 -o root -g root /etc/brunos-host
    if [ -f "$SENHA" ]; then
      a=$(le_senha "Senha de configuração ATUAL: ")
      printf '%s' "$a" | python3 -c "$PY_CONFERE" "$SENHA" || falha "senha atual errada" 7
    fi
    s1=$(le_senha "Nova Senha de configuração (mínimo 8 caracteres, diferente da senha do BrunOS): ")
    [ ${#s1} -ge 8 ] || falha "a senha precisa ter pelo menos 8 caracteres"
    s2=$(le_senha "Digite de novo: ")
    [ "$s1" = "$s2" ] || falha "as duas senhas não são iguais"
    printf '%s' "$s1" | python3 -c "$PY_GERA" >"$SENHA.novo"
    chmod 600 "$SENHA.novo"
    mv -f "$SENHA.novo" "$SENHA"
    rm -f "$TRAVA"
    echo "Senha de configuração criada. Ela é pedida pelo BrunOS nas ações sensíveis do servidor: formatar um disco e desinstalar um app."
    exit 0 ;;
  formatar)
    confere_senha formatar
    checa_formatar "$valor" "$modo" "$extra"
    command -v systemd-run >/dev/null 2>&1 || falha "este servidor não tem systemd-run" 3
    if systemctl is-active --quiet brunos-discos; then falha "já tem uma formatação rodando" 4; fi
    systemd-run --unit=brunos-discos --collect --quiet --description="Formatação pedida pelo BrunOS" /usr/local/sbin/brunos-host formatar-executar "$valor" "$modo" "$extra" "$extra2"
    echo "iniciado"
    exit 0 ;;
  formatar-executar)
    exec >"$LOGD" 2>&1
    echo "inicio $(date +%s) $valor $extra $extra2"
    # confere tudo de novo imediatamente antes de apagar (falha aqui = nada foi apagado)
    ( checa_formatar "$valor" "$modo" "$extra" ) || { echo "fim 3"; exit 3; }
    case "$valor" in nvme*) part="${valor}p1" ;; *) part="${valor}1" ;; esac
    echo "etapa apagando"
    for p in $(partes "$valor" | sort -r); do wipefs -a "/dev/$p" || { echo "fim 1"; exit 1; }; done
    echo "etapa particionando"
    case "$extra" in ext4|btrfs) tipo=8300 ;; *) tipo=0700 ;; esac
    sgdisk -o -n "1:0:0" -t "1:$tipo" "/dev/$valor" || { echo "fim 1"; exit 1; }
    partprobe "/dev/$valor" || true
    udevadm settle 2>/dev/null || sleep 2
    [ -b "/dev/$part" ] || { echo "a partição nova não apareceu"; echo "fim 1"; exit 1; }
    echo "etapa formatando"
    case "$extra" in
      ext4) mkfs.ext4 -F -L "$extra2" "/dev/$part" ;;
      btrfs) mkfs.btrfs -f -L "$extra2" "/dev/$part" ;;
      exfat) mkfs.exfat -L "$extra2" "/dev/$part" ;;
      ntfs) mkfs.ntfs -Q -F -L "$extra2" "/dev/$part" ;;
    esac || { echo "fim 1"; exit 1; }
    if [ "$extra" = ext4 ] || [ "$extra" = btrfs ]; then
      # o dono da raiz do disco novo é o usuário do BrunOS (1000), para dar para gravar pelo painel
      t=$(mktemp -d)
      if mount "/dev/$part" "$t"; then chown 1000:1000 "$t"; umount "$t"; fi
      rmdir "$t" 2>/dev/null || true
    fi
    echo "uuid $(blkid -o value -s UUID "/dev/$part" 2>/dev/null)"
    echo "fim 0"
    exit 0 ;;
  discos-estado)
    if systemctl is-active --quiet brunos-discos 2>/dev/null; then echo "rodando"; else echo "parado"; fi
    if [ -f "$SENHA" ]; then
      echo "senha sim"
      # a data em que a senha foi criada (último campo do resumo); nunca o resumo em si
      d=$(cut -d'$' -f7 "$SENHA" 2>/dev/null | tr -cd '0-9T:-' | cut -c1-19)
      [ -z "$d" ] || echo "senha-data $d"
    else echo "senha nao"; fi
    if [ -f "$TRAVA" ]; then read -r n ate <"$TRAVA" || true; echo "trava ${n:-0} ${ate:-0}"; fi
    echo "=== registro"
    tail -n 40 "$LOGD" 2>/dev/null || true
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
  iniciar-app|parar-app|desinstalar-app|adicionar-loja|remover-loja)
    # janela Loja: os mesmos comandos do painel do Runtipi, pelo runtipi-cli (o Runtipi fica sabendo)
    d=$(pasta_runtipi) || falha "não achei o runtipi-cli: em Configurações -> Energia, copie os comandos de atualização e cole no servidor" 3
    # desinstalar apaga os dados do app: só com a Senha de configuração (pela entrada padrão), conferida aqui
    if [ "$acao" = desinstalar-app ]; then confere_senha desinstalar; fi
    cd "$d"
    case "$acao" in
      iniciar-app) set -- app start "$valor" ;;
      parar-app) set -- app stop "$valor" ;;
      desinstalar-app) set -- app uninstall "$valor" ;;
      adicionar-loja) set -- appstore add "$valor" "$modo" ;;
      remover-loja) set -- appstore remove "$valor" ;;
    esac
    if ! r=$(timeout 280 ./runtipi-cli "$@" 2>&1); then
      echo "$r" | tail -n 3 >&2
      exit 1
    fi
    echo "feito"
    exit 0 ;;
  info-app)
    # o que o Runtipi sabe do app (estado, respostas da instalação): para acompanhar e editar
    api_runtipi GET "/apps/$valor" </dev/null
    exit 0 ;;
  instalar-app)
    # o formulário (JSON) vem pela entrada padrão; o Runtipi instala sozinho, em segundo plano
    api_runtipi POST "/app-lifecycle/$valor/install"
    exit 0 ;;
  reconfigurar-app)
    # as respostas novas (JSON) pela entrada padrão; depois reinicia para valerem
    api_runtipi PATCH "/app-lifecycle/$valor/update-config" >/dev/null
    api_runtipi POST "/app-lifecycle/$valor/restart" </dev/null
    exit 0 ;;
  reiniciar-app)
    api_runtipi POST "/app-lifecycle/$valor/restart" </dev/null
    exit 0 ;;
  ajustes-ler|ajustes-gravar)
    # Ajustes do app: user-config/<loja>/<app>/docker-compose.yml, que o Runtipi junta ao compose do app
    d=$(pasta_runtipi) || falha "não achei o runtipi-cli: em Configurações -> Energia, copie os comandos de atualização e cole no servidor" 3
    id=${valor%%:*}
    loja=${valor#*:}
    [ -d "$d/apps/$loja/$id" ] || falha "esse app não está instalado" 4
    dir="$d/user-config/$loja/$id"
    arq="$dir/docker-compose.yml"
    if [ -L "$dir" ] || [ -L "$arq" ]; then falha "o arquivo de ajustes é um atalho: não mexo nele" 3; fi
    if [ "$acao" = ajustes-ler ]; then
      if [ -f "$arq" ]; then head -c 65537 "$arq"; fi
      exit 0
    fi
    # gravar: primeira linha = Senha de configuração (conferida aqui), o resto = o arquivo novo
    confere_senha ajustes
    umask 022
    t=$(mktemp)
    head -c 65537 >"$t"
    [ "$(wc -c <"$t")" -le 65536 ] || { rm -f "$t"; falha "arquivo de ajustes grande demais" 2; }
    install -d -m 755 "$dir"
    if [ -f "$arq" ]; then
      # cópia do arquivo anterior (as 5 últimas ficam)
      cp -p "$arq" "$dir/docker-compose.yml.brunos-$(date +%Y%m%d-%H%M%S).bak"
      ls -1t "$dir"/docker-compose.yml.brunos-*.bak 2>/dev/null | tail -n +6 | while read -r b; do rm -f "$b"; done
    fi
    if [ -s "$t" ]; then
      install -m 644 "$t" "$arq.novo"
      chown --reference="$d/apps" "$arq.novo" 2>/dev/null || true
      mv -f "$arq.novo" "$arq"
    else
      rm -f "$arq"
    fi
    rm -f "$t"
    echo "ajustes gravados"
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
