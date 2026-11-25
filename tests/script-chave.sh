#!/bin/bash
# Testa o "trava-chave" do brunos-host (script 19, 0.30.0) de verdade, num contêiner descartável com uma
# ponte br-* de mentira (172.30.0.0/16), o usuário dedicado, sudo e o authorized_keys de verdade.
# Confere: pelo SSH (usuário dedicado + sudo) a linha ganha from="172.30.0.0/16" e continua restrita; rodar de
# novo não muda nada; um from= antigo é trocado; pedido que não veio de rede do Docker, authorized_keys com
# outra linha ou com duas linhas ficam como estão; o endereço nunca vem do pedido.
# Uso: bash tests/script-chave.sh   (precisa de Go e Docker)
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
go run ./tools/hostscript >"$work/brunos-host"

docker run --rm --cap-add NET_ADMIN -v "$work:/w:ro" debian:bookworm-slim bash -c '
  set -u
  apt-get update -qq >/dev/null && apt-get install -y -qq iproute2 sudo >/dev/null
  install -m 755 /w/brunos-host /usr/local/sbin/brunos-host
  H=/usr/local/sbin/brunos-host
  falhou=0
  ok() { echo "ok: $1"; }
  erro() { echo "ERRO: $1"; falhou=1; }
  ip link add br-0123abcd type bridge && ip addr add 172.30.0.1/16 dev br-0123abcd && ip link set br-0123abcd up
  useradd --system --create-home --shell /bin/sh brunos-power
  install -d -m 700 -o brunos-power -g brunos-power /home/brunos-power/.ssh
  echo "brunos-power ALL=(root) NOPASSWD: $H" >/etc/sudoers.d/brunos-power
  chmod 440 /etc/sudoers.d/brunos-power
  AK=/home/brunos-power/.ssh/authorized_keys
  CHAVE="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITESTE brunos-power"
  LINHA="restrict,command=\"$H\" $CHAVE"
  grava() { printf "%s\n" "$1" >"$AK"; chown brunos-power:brunos-power "$AK"; chmod 600 "$AK"; }
  # como o sshd chamaria: usuário dedicado, pedido em SSH_ORIGINAL_COMMAND e o endereço em SSH_CLIENT
  pede() { sudo -u brunos-power env SSH_ORIGINAL_COMMAND="$1" SSH_CLIENT="$2 40000 22" "$H" 2>&1; }

  grava "$LINHA"
  r=$(pede trava-chave 172.30.0.11)
  [ "$r" = "trava 172.30.0.0/16" ] && [ "$(cat $AK)" = "restrict,from=\"172.30.0.0/16\",command=\"$H\" $CHAVE" ] \
    && ok "pelo SSH a chave fica presa à rede do Docker" || erro "trava: $r / $(cat $AK)"
  [ "$(stat -c "%U %a" $AK)" = "brunos-power 600" ] && ok "authorized_keys continua do usuário, 600" || erro "dono/permissão: $(stat -c "%U %a" $AK)"
  antes=$(stat -c %i $AK)
  r=$(pede trava-chave 172.30.0.11)
  [ "$r" = "trava 172.30.0.0/16" ] && [ "$(stat -c %i $AK)" = "$antes" ] && ok "de novo: nada muda" || erro "repetir: $r"
  grava "restrict,from=\"10.9.0.0/16\",command=\"$H\" $CHAVE"
  r=$(pede trava-chave 172.30.0.99)
  [ "$(cat $AK)" = "restrict,from=\"172.30.0.0/16\",command=\"$H\" $CHAVE" ] && ok "from= antigo é trocado pelo novo" || erro "troca: $r / $(cat $AK)"

  grava "$LINHA"
  if r=$(pede trava-chave 127.0.0.1); then erro "pedido de fora do Docker deveria falhar: $r"; else
    case "$r" in *"não veio de uma rede do Docker"*) ok "pedido que não veio do Docker: recusa" ;; *) erro "fora do Docker: $r" ;; esac; fi
  [ "$(cat $AK)" = "$LINHA" ] && ok "e a chave fica como estava" || erro "mudou: $(cat $AK)"
  if r=$(pede "trava-chave 172.30.0.5" 127.0.0.1); then erro "endereço no pedido deveria ser recusado: $r"; else ok "endereço no pedido: recusa"; fi
  [ "$(cat $AK)" = "$LINHA" ] || erro "mudou com endereço no pedido: $(cat $AK)"

  grava "ssh-ed25519 AAAAOUTRA alguem"
  if r=$(pede trava-chave 172.30.0.11); then erro "linha que não é do BrunOS deveria ficar: $r"; else ok "linha que não é do BrunOS: fica como está"; fi
  printf "%s\n%s\n" "$LINHA" "ssh-ed25519 AAAAOUTRA alguem" >"$AK"
  if r=$(pede trava-chave 172.30.0.11); then erro "duas linhas deveriam ficar: $r"; else ok "authorized_keys com duas linhas: fica como está"; fi
  [ "$(grep -c . $AK)" = 2 ] || erro "duas linhas mudaram"

  # como root direto (sem SUDO_USER): recusa, não adivinha de quem é a chave
  grava "$LINHA"
  if r=$($H trava-chave 172.30.0.11 2>&1); then erro "root sem SUDO_USER deveria falhar: $r"; else ok "root direto: recusa"; fi
  exit $falhou
'
echo "script-chave: tudo certo"
