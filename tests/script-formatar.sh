#!/bin/bash
# Testa montar, desmontar, rótulo e formatar do brunos-host (script 11) de verdade, como root, num
# contêiner descartável. As ferramentas que mexem em disco (mount, umount, mkfs, sgdisk, wipefs...) são
# falsas e anotam como foram chamadas; a Senha de formatação usa o python3 de verdade (scrypt).
# Confere: fstab volta como estava quando o mount falha; desmontar recusa disco em uso e só tira a linha
# do BrunOS; formatar recusa sem senha, com senha errada (trava depois de 5), série que não bate, disco do
# sistema e disco no fstab, e só apaga depois de conferir tudo de novo; a senha nunca pela chave do BrunOS.
# Uso: bash tests/script-formatar.sh   (precisa de Go e Docker)
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
go run ./tools/hostscript >"$work/brunos-host"

docker run --rm -v "$work:/w:ro" debian:bookworm-slim bash -c '
  set -u
  apt-get update -qq >/dev/null && apt-get install -y -qq python3 >/dev/null
  install -m 755 /w/brunos-host /usr/local/sbin/brunos-host
  H=/usr/local/sbin/brunos-host
  falhou=0
  ok() { echo "ok: $1"; }
  erro() { echo "ERRO: $1"; falhou=1; }
  recusa() { # recusa <nome> <trecho da mensagem> <pedido...>
    nome=$1; trecho=$2; shift 2
    if r=$("$@" 2>&1); then erro "$nome deveria ser recusado: $r"; else case "$r" in *"$trecho"*) ok "$nome" ;; *) erro "$nome: $r" ;; esac; fi
  }
  for d in sdb:200 sdb1:201 sdc:202 sdc2:203; do mknod "/dev/${d%%:*}" b 7 "${d##*:}"; done
  printf "# fstab de teste\nUUID=raiz / ext4 defaults 0 1\nUUID=antigo /mnt/antigo ext4 defaults 0 2\n" >/etc/fstab
  cp /etc/fstab /tmp/fstab.original
  mkdir -p /mnt/antigo /mnt/cheia && touch /mnt/cheia/arquivo
  : >/tmp/montados

  fake() { printf "#!/bin/sh\n%s\n" "$2" >"/usr/sbin/$1"; chmod 755 "/usr/sbin/$1"; }
  fake blkid '"'"'case "$*" in "-U 1111-aaaa") echo /dev/sdb1 ;; "-U 2222-bbbb") echo /dev/sdc2 ;; "-U 3333-cccc") exit 2 ;; *"-s TYPE /dev/sdb1") echo ext4 ;; *"-s TYPE /dev/sdc2") echo ext4 ;; *"-s UUID"*) echo uuid-novo ;; esac'"'"'
  fake findmnt '"'"'case "$*" in "-no SOURCE /") echo /dev/sdc2 ;; "-no SOURCE /mnt/"*) echo /dev/sdb1 ;; *) exit 1 ;; esac'"'"'
  fake swapon '"'"'exit 0'"'"'
  fake lsblk '"'"'echo "$*" >>/tmp/lsblk.args
case "$*" in
  "-nso NAME /dev/sdc2") printf "sdc2\nsdc\n" ;;
  "-nso NAME /dev/sdb1") printf "sdb1\nsdb\n" ;;
  "-nso NAME "*) : ;;
  "-dnro SERIAL /dev/sdb") echo SERIE1 ;;
  "-dnro SERIAL /dev/sdc") echo SERIE2 ;;
  "-nro UUID /dev/sdb") cat /tmp/uuid-sdb 2>/dev/null || true ;;
  "-nro MOUNTPOINT "*|"-nro FSTYPE "*) : ;;
esac'"'"'
  fake mountpoint '"'"'shift; grep -qx "$1" /tmp/montados'"'"'
  fake mount '"'"'echo "$*" >>/tmp/mount.args
[ -f /tmp/mount-falha ] && { echo "wrong fs type"; exit 32; }
case "$1" in /mnt/*) grep -q " $1 " /etc/fstab || exit 1; echo "$1" >>/tmp/montados ;; esac'"'"'
  fake umount '"'"'echo "$*" >>/tmp/umount.args; grep -vx "$1" /tmp/montados >/tmp/m2 || true; mv /tmp/m2 /tmp/montados'"'"'
  fake fuser '"'"'case "$1" in -m) cat /tmp/fuser-pids 2>/dev/null || true ;; -s) exit 1 ;; esac'"'"'
  fake systemctl '"'"'exit 1'"'"'
  fake systemd-run '"'"'while [ "${1#--}" != "$1" ]; do shift; done; INVOCATION_ID=teste "$@"'"'"'
  fake e2label '"'"'echo "$*" >>/tmp/e2label.args'"'"'
  for t in wipefs sgdisk partprobe udevadm mkfs.ext4 mkfs.exfat mkfs.btrfs mkfs.ntfs; do fake $t "echo \"$t \$*\" >>/tmp/disco.args"; done

  # ── montar ────────────────────────────────────────────────────
  [ "$($H montar 1111-aaaa Fotos)" = "montado /mnt/Fotos ext4" ] && ok "montar" || erro "montar"
  grep -qx "UUID=1111-aaaa /mnt/Fotos ext4 defaults,nofail 0 2 # brunos" /etc/fstab && ok "linha do fstab marcada # brunos e com nofail" || erro "fstab: $(cat /etc/fstab)"
  ls /etc/fstab.brunos-* >/dev/null 2>&1 && ok "cópia do fstab antes de mexer" || erro "sem cópia do fstab"
  recusa "montar de novo a mesma partição" "já tem uma linha" $H montar 1111-aaaa Outra
  recusa "montar partição do disco do sistema" "disco do sistema" $H montar 2222-bbbb X
  recusa "montar UUID que não existe" "não achei" $H montar 3333-cccc X
  cp /tmp/fstab.original /etc/fstab
  : >/tmp/montados; rmdir /mnt/Fotos   # volta ao começo (o mount falso esquece a montagem)
  recusa "montar em pasta que não está vazia" "não está vazia" $H montar 1111-aaaa cheia
  touch /tmp/mount-falha
  recusa "mount falhou" "voltou como estava" $H montar 1111-aaaa Nova
  rm -f /tmp/mount-falha
  cmp -s /etc/fstab /tmp/fstab.original && [ ! -d /mnt/Nova ] && ok "fstab restaurado e pasta apagada quando o mount falha" || erro "não voltou atrás: $(cat /etc/fstab)"

  # ── desmontar ─────────────────────────────────────────────────
  $H montar 1111-aaaa Fotos >/dev/null
  sleep 300 & pid=$!
  echo "$pid" >/tmp/fuser-pids
  recusa "desmontar disco em uso" "em uso por: sleep" $H desmontar Fotos
  kill $pid; rm -f /tmp/fuser-pids
  out=$($H desmontar Fotos)
  case "$out" in *"fstab tirado"*"desmontado /mnt/Fotos"*) ok "desmontar tira a linha do BrunOS" ;; *) erro "desmontar: $out" ;; esac
  grep -q "/mnt/Fotos" /etc/fstab && erro "a linha do BrunOS ficou no fstab" || ok "linha do BrunOS saiu"
  grep -q "UUID=antigo /mnt/antigo" /etc/fstab && ok "linhas que não são do BrunOS ficaram" || erro "mexeu em linha que não era do BrunOS"
  echo /mnt/antigo >>/tmp/montados
  out=$($H desmontar antigo)
  case "$out" in *"fstab ficou"*) ok "disco montado por fora: avisa que a linha ficou" ;; *) erro "desmontar antigo: $out" ;; esac
  recusa "desmontar pasta sem disco" "não há disco montado" $H desmontar Nada

  # ── rótulo ────────────────────────────────────────────────────
  [ "$($H rotulo 1111-aaaa Fotos2)" = "rotulo Fotos2" ] && grep -qx "/dev/sdb1 Fotos2" /tmp/e2label.args && ok "rótulo com e2label" || erro "rótulo"

  # ── Senha de formatação ───────────────────────────────────────
  recusa "formatar sem Senha de formatação" "ainda não existe Senha de formatação" sh -c "echo x | $H formatar sdb SERIE1 ext4 Novo"
  recusa "criar a senha fora do Terminal" "rode no Terminal" sh -c "$H senha-formatar </dev/null"
  recusa "criar a senha pelo sudo da chave do BrunOS" "Terminal do servidor" env SUDO_USER=brunos-power $H senha-formatar
  install -d -m 700 /etc/brunos-host
  # mesmo formato que o "senha-formatar" grava
  printf "certa-12345" | python3 -c "import hashlib,os,sys;s=sys.stdin.buffer.read();sal=os.urandom(16);h=hashlib.scrypt(s,salt=sal,n=32768,r=8,p=1,maxmem=67108864,dklen=32);print(\"scrypt\$32768\$8\$1\$%s\$%s\$2026-11-13T10:00:00\"%(sal.hex(),h.hex()))" >/etc/brunos-host/formatar
  chmod 600 /etc/brunos-host/formatar
  recusa "senha errada" "tentativa 1 de 5" sh -c "echo errada | $H formatar sdb SERIE1 ext4 Novo"
  [ ! -s /tmp/disco.args ] && ok "senha errada não apaga nada" || erro "apagou com senha errada"
  recusa "série que não bate" "não confere" sh -c "echo certa-12345 | $H formatar sdb OUTRA ext4 Novo"
  recusa "disco do sistema" "disco do sistema" sh -c "echo certa-12345 | $H formatar sdc SERIE2 ext4 Novo"
  echo 1111-aaaa >/tmp/uuid-sdb
  echo "UUID=1111-aaaa /mnt/x ext4 defaults 0 2" >>/etc/fstab
  recusa "disco no fstab" "fstab" sh -c "echo certa-12345 | $H formatar sdb SERIE1 ext4 Novo"
  cp /tmp/fstab.original /etc/fstab
  [ ! -s /tmp/disco.args ] && ok "nenhuma recusa apagou nada" || erro "apagou numa recusa: $(cat /tmp/disco.args)"
  for i in 1 2 3 4; do echo errada | $H formatar sdb SERIE1 ext4 Novo >/dev/null 2>&1 || true; done
  recusa "5ª senha errada trava" "bloqueado por 1 hora" sh -c "echo errada | $H formatar sdb SERIE1 ext4 Novo"
  recusa "travado mesmo com a senha certa" "bloqueado até" sh -c "echo certa-12345 | $H formatar sdb SERIE1 ext4 Novo"
  rm -f /etc/brunos-host/formatar.trava

  # ── formatar de verdade (com as ferramentas falsas) ───────────
  out=$(echo certa-12345 | $H formatar sdb SERIE1 ext4 Novo 2>&1)
  [ "$out" = "iniciado" ] && ok "formatar começou" || erro "formatar: $out"
  grep -q "^wipefs -a /dev/sdb" /tmp/disco.args && grep -q "^sgdisk -o -n 1:0:0 -t 1:8300 /dev/sdb" /tmp/disco.args && grep -q "^mkfs.ext4 -F -L Novo /dev/sdb1" /tmp/disco.args \
    && ok "apagou, particionou e formatou o disco certo" || erro "passos: $(cat /tmp/disco.args)"
  out=$($H discos-estado)
  case "$out" in *"senha sim"*"=== registro"*"etapa formatando"*"uuid uuid-novo"*"fim 0"*) ok "discos-estado com o andamento e o fim" ;; *) erro "discos-estado: $out" ;; esac
  case "$(grep -c certa-12345 /var/log/brunos-discos.log /tmp/*.args 2>/dev/null | grep -v ":0" || true)" in "") ok "a senha não aparece em registro nenhum" ;; *) erro "a senha vazou para um registro" ;; esac
  : >/tmp/disco.args
  out=$(echo certa-12345 | $H formatar sdb SERIE1 exfat Pen 2>&1)
  grep -q "^sgdisk -o -n 1:0:0 -t 1:0700 /dev/sdb" /tmp/disco.args && grep -q "^mkfs.exfat -L Pen /dev/sdb1" /tmp/disco.args && ok "exFAT" || erro "exfat: $out $(cat /tmp/disco.args)"
  exit $falhou
'
echo "montar, desmontar e formatar: tudo certo"
