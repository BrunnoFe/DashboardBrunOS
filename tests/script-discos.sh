#!/bin/bash
# Testa os pedidos da janela Discos do brunos-host (script 10) de verdade, como root, num contêiner
# descartável. lsblk, hdparm, journalctl, smartctl e dd são falsos (ficam em /usr/sbin, que vem antes no
# PATH do script) e anotam como foram chamados; os discos são nós de bloco criados com mknod.
# Confere: o que cada pedido devolve, que o fstab sai sem as opções, que a velocidade só lê (dd para
# /dev/null, iflag=direct) e roda um por vez, e as travas do ejetar (só USB, só desmontado).
# Uso: bash tests/script-discos.sh   (precisa de Go e Docker)
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
go run ./tools/hostscript >"$work/brunos-host"

docker run --rm -v "$work:/w:ro" debian:bookworm-slim bash -c '
  set -u
  install -m 755 /w/brunos-host /usr/local/sbin/brunos-host
  H=/usr/local/sbin/brunos-host
  falhou=0
  ok() { echo "ok: $1"; }
  erro() { echo "ERRO: $1"; falhou=1; }
  for d in sdb:200 sdc:201 sdd:202; do mknod "/dev/${d%%:*}" b 7 "${d##*:}"; done
  mknod /dev/nvme0n1 b 7 203
  mkdir -p /dev/disk/by-id && ln -s ../../sdb /dev/disk/by-id/usb-Case_HD_X1-0:0 && ln -s ../../sdb /dev/disk/by-id/usb-Case_HD_X1-0:0-part1
  printf "# comentário\nUUID=abc /mnt/disco1 ext4 defaults,nofail 0 2\nLABEL=Backup /mnt/backup ntfs ro 0 0\n/dev/sdc1 /mnt/x ext4 defaults 0 0\n" >/etc/fstab

  cat >/usr/sbin/lsblk <<"EOF"
#!/bin/sh
echo "$*" >>/tmp/lsblk.args
case "$*" in
  *-J*) echo "{\"blockdevices\":[{\"name\":\"sdb\",\"type\":\"disk\"}]}" ;;
  "-dno TRAN /dev/sdb"|"-dno TRAN /dev/sdd") echo "usb" ;;
  "-dno TRAN /dev/sdc") echo "sata" ;;
  "-nro MOUNTPOINT /dev/sdd") printf "\n/mnt/x\n" ;;
  "-nro MOUNTPOINT "*) printf "\n\n" ;;
esac
EOF
  cat >/usr/sbin/hdparm <<"EOF"
#!/bin/sh
echo "$*" >>/tmp/hdparm.args
printf "\n/dev/sdb:\n drive state is:  standby\n"
EOF
  cat >/usr/sbin/journalctl <<"EOF"
#!/bin/sh
echo "$*" >>/tmp/journalctl.args
echo "2026-11-01T10:00:00-0300 h kernel: sd 6:0:0:0: [sdb] tag#3 FAILED Result: hostbyte=DID_ERROR"
echo "2026-11-01T10:00:01-0300 h kernel: I/O error, dev sdb1, sector 2048 op 0x0:(READ)"
echo "2026-11-01T10:00:02-0300 h kernel: sd 6:0:0:0: [sdb] Attached SCSI disk"
echo "2026-11-01T10:00:03-0300 h kernel: I/O error, dev sdc, sector 99"
echo "2026-11-01T10:00:04-0300 h kernel: I/O error, dev sdba, sector 1"
EOF
  cat >/usr/sbin/smartctl <<"EOF"
#!/bin/sh
echo "$*" >>/tmp/smartctl.args
case "$*" in
  *"-t short"*) echo "Testing has begun." ;;
  *"-t long"*) echo "Can'"'"'t start self-test without aborting current test"; exit 4 ;;
  *) echo "{\"smartctl\":{\"exit_status\":0}}" ;;
esac
EOF
  cat >/usr/sbin/dd <<"EOF"
#!/bin/sh
echo "$*" >>/tmp/dd.args
sleep 2
echo "1024+0 records in" >&2
echo "1073741824 bytes (1.1 GB, 1.0 GiB) copied, 7.5 s, 143 MB/s" >&2
EOF
  chmod 755 /usr/sbin/lsblk /usr/sbin/hdparm /usr/sbin/journalctl /usr/sbin/smartctl /usr/sbin/dd

  # discos-detalhe: lsblk, o fstab só com identificador e pasta, e o by-id sem as partições
  out=$($H discos-detalhe)
  case "$out" in *"=== lsblk"*"blockdevices"*"=== fstab"*"=== by-id"*) ok "discos-detalhe tem as três partes" ;; *) erro "discos-detalhe: $out" ;; esac
  echo "$out" | grep -qx "UUID=abc /mnt/disco1" && echo "$out" | grep -qx "LABEL=Backup /mnt/backup" && ok "fstab: UUID, rótulo e pasta" || erro "fstab: $out"
  if echo "$out" | grep -q -e defaults -e nofail -e comentário; then erro "o fstab não pode levar as opções nem comentários"; else ok "fstab sem opções nem comentários"; fi
  # (as ligações das partições também vêm; o BrunOS escolhe a do disco inteiro, testado no Go)
  echo "$out" | grep -qx "usb-Case_HD_X1-0:0 /dev/sdb" && ok "by-id do disco" || erro "by-id: $out"
  grep -q "MOUNTPOINTS" /tmp/lsblk.args && ok "lsblk com MOUNTPOINTS" || erro "lsblk: $(cat /tmp/lsblk.args)"

  # estado: hdparm -C (não acorda o disco); NVMe sem perguntar; sem hdparm, desconhecido
  [ "$($H estado sdb)" = "estado dormindo" ] && grep -qx -- "-C /dev/sdb" /tmp/hdparm.args && ok "estado: hdparm -C, dormindo" || erro "estado sdb: $($H estado sdb)"
  [ "$($H estado nvme0n1)" = "estado acordado" ] && ok "estado do NVMe" || erro "estado nvme0n1"
  mv /usr/sbin/hdparm /tmp/hdparm.off
  [ "$($H estado sdb)" = "estado desconhecido sem-hdparm" ] && ok "estado sem hdparm" || erro "estado sem hdparm: $($H estado sdb)"
  mv /tmp/hdparm.off /usr/sbin/hdparm

  # erros: só as linhas de erro daquele disco (sdb e sdb1; não sdc, não sdba, não a linha sem erro)
  out=$($H erros sdb)
  if [ "$(echo "$out" | wc -l)" = 2 ] && echo "$out" | grep -q "\[sdb\] tag#3" && echo "$out" | grep -q "dev sdb1"; then ok "erros: só as do sdb"; else erro "erros: $out"; fi
  grep -q -- "-k --since=-30d" /tmp/journalctl.args && ok "erros: registro do kernel, 30 dias" || erro "journalctl: $(cat /tmp/journalctl.args)"

  # smart com o andamento dos testes; teste curto começa; longo recusado pelo disco vira erro
  $H smart sdb sat >/dev/null && grep -q -- "-c -l selftest -d sat /dev/sdb" /tmp/smartctl.args && ok "smart com -c -l selftest" || erro "smart: $(cat /tmp/smartctl.args)"
  [ "$($H smart-teste sdb sat curto)" = "teste iniciado" ] && grep -q -- "-t short -d sat /dev/sdb" /tmp/smartctl.args && ok "teste curto iniciado" || erro "teste curto: $(cat /tmp/smartctl.args)"
  if r=$($H smart-teste sdb auto longo 2>&1); then erro "teste recusado pelo disco deveria falhar: $r"; else case "$r" in *"aborting current test"*) ok "teste recusado: mensagem do disco" ;; *) erro "teste longo: $r" ;; esac; fi

  # velocidade: só lê (dd para /dev/null, direto do disco) e um por vez
  $H velocidade sdb >/tmp/v1 2>&1 &
  sleep 0.5
  if r=$($H velocidade sdc 2>&1); then erro "segundo teste de velocidade deveria esperar: $r"; else case "$r" in *"já tem um teste"*) ok "velocidade: um por vez" ;; *) erro "velocidade 2: $r" ;; esac; fi
  wait
  grep -q "^velocidade 1073741824 bytes .* copied, 7.5 s" /tmp/v1 && ok "velocidade: resultado do dd" || erro "velocidade: $(cat /tmp/v1)"
  grep -qx "if=/dev/sdb of=/dev/null bs=1M count=1024 iflag=direct" /tmp/dd.args && ok "velocidade só lê (of=/dev/null)" || erro "dd: $(cat /tmp/dd.args)"

  # ejetar: só USB, só desmontado; aqui não há /sys do disco, então para antes de desligar
  for c in "sdc:só dá para ejetar disco USB" "sdd:ainda está montado" "sdb:não pode ser ejetado por aqui"; do
    if r=$($H ejetar "${c%%:*}" 2>&1); then erro "ejetar ${c%%:*} deveria recusar: $r"; else case "$r" in *"${c#*:}"*) ok "ejetar ${c%%:*}: ${c#*:}" ;; *) erro "ejetar ${c%%:*}: $r" ;; esac; fi
  done

  # pedidos ruins recusados mesmo como root; disco que não existe
  for bad in "velocidade sda1" "ejetar ../sdb" "estado /dev/sdb" "erros sdb;id" "smart-teste sdb sat medio" "discos-detalhe x"; do
    if $H $bad >/dev/null 2>&1; then erro "pedido \"$bad\" deveria ser recusado"; fi
  done
  ok "pedidos inválidos recusados"
  r=$($H estado sdz 2>&1 || true); case "$r" in *"não existe"*) ok "disco que não existe" ;; *) erro "sdz: $r" ;; esac
  exit $falhou
'
echo "pedidos da janela Discos: tudo certo"
