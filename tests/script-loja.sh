#!/bin/bash
# Testa os pedidos da janela Loja do brunos-host (script 12) de verdade, como root, num contêiner
# descartável, com um runtipi-cli falso que anota como foi chamado.
# Confere: que iniciar, parar, desinstalar, adicionar loja e remover loja chegam ao runtipi-cli com os
# argumentos certos (e só eles), que o erro do runtipi-cli volta para o BrunOS, que nome e endereço
# estranhos são recusados antes de chegar lá e que sem a pasta do Runtipi o erro explica o que fazer.
# Uso: bash tests/script-loja.sh   (precisa de Go e Docker)
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
go run ./tools/hostscript >"$work/brunos-host"

docker run --rm -v "$work:/w:ro" debian:bookworm-slim bash -c '
  set -u
  apt-get update -qq >/dev/null && apt-get install -y -qq python3 >/dev/null || { echo "ERRO: não deu para instalar o python3"; exit 1; }
  install -m 755 /w/brunos-host /usr/local/sbin/brunos-host
  H=/usr/local/sbin/brunos-host
  falhou=0
  mkdir -p /opt/runtipi
  cat >/opt/runtipi/runtipi-cli <<"EOF"
#!/bin/sh
echo "$*" >>/tmp/chamadas
case "$*" in *falha*) echo "Error: app not found" >&2; exit 1 ;; esac
echo "ok"
EOF
  chmod +x /opt/runtipi/runtipi-cli
  echo "RUNTIPI=/opt/runtipi" >/etc/brunos-host.conf
  # pede <código esperado> <texto esperado na saída> <pedido...>
  pede() {
    quer=$1; texto=$2; shift 2
    saida=$("$H" "$@" 2>&1); codigo=$?
    if [ "$codigo" -eq "$quer" ] && echo "$saida" | grep -q "$texto"; then echo "ok: $* -> $codigo"; else echo "ERRO: $* -> $codigo: $saida"; falhou=1; fi
  }
  # com_senha <senha> <código> <texto> <pedido...>: manda a senha pela entrada padrão
  com_senha() {
    s=$1; quer=$2; texto=$3; shift 3
    saida=$(echo "$s" | "$H" "$@" 2>&1); codigo=$?
    if [ "$codigo" -eq "$quer" ] && echo "$saida" | grep -q "$texto"; then echo "ok: $* (senha) -> $codigo"; else echo "ERRO: $* (senha) -> $codigo: $saida"; falhou=1; fi
  }
  pede 0 feito iniciar-app immich:migrated
  pede 0 feito parar-app immich:migrated
  # desinstalar só com a Senha de formatação: sem senha criada, com senha errada e com a certa
  com_senha x 5 "ainda não existe Senha de formatação" desinstalar-app nextcloud_mini:minha-loja
  mkdir -p /etc/brunos-host
  printf "certa-12345" | python3 -c "
import hashlib,os,sys
s=sys.stdin.buffer.read();sal=os.urandom(16)
h=hashlib.scrypt(s,salt=sal,n=32768,r=8,p=1,maxmem=67108864,dklen=32)
print(\"scrypt\$32768\$8\$1\$%s\$%s\$agora\"%(sal.hex(),h.hex()))" >/etc/brunos-host/formatar
  com_senha errada 7 "tentativa 1 de 5" desinstalar-app nextcloud_mini:minha-loja
  com_senha "" 7 "tentativa 2 de 5" desinstalar-app nextcloud_mini:minha-loja
  if grep -q uninstall /tmp/chamadas; then echo "ERRO: desinstalou sem a senha certa"; falhou=1; else echo "ok: sem a senha certa nada é desinstalado"; fi
  com_senha certa-12345 0 feito desinstalar-app nextcloud_mini:minha-loja
  [ ! -e /etc/brunos-host/formatar.trava ] && echo "ok: a senha certa zera os erros" || { echo "ERRO: a trava ficou"; falhou=1; }
  for i in 1 2 3 4; do echo errada | "$H" desinstalar-app immich:migrated >/dev/null 2>&1 || true; done
  com_senha errada 6 "bloqueado por 1 hora" desinstalar-app immich:migrated
  com_senha certa-12345 6 "bloqueado até" desinstalar-app immich:migrated
  rm -f /etc/brunos-host/formatar.trava
  pede 0 feito adicionar-loja minha-loja https://github.com/usuario/minha-loja
  pede 0 feito remover-loja minha-loja
  pede 1 "app not found" parar-app falha:migrated
  pede 2 "inválido" adicionar-loja minha-loja "https://github.com/a/b;reboot"
  pede 2 "inválido" adicionar-loja minha-loja https://u:p@github.com/a/b
  pede 2 "inválido" adicionar-loja minha-loja http://github.com/a/b
  pede 2 "inválido" adicionar-loja minha-loja https://github.com/a/../b
  pede 2 "inválido" adicionar-loja Minha https://github.com/a/b
  pede 2 "inválido" desinstalar-app ../x:y
  pede 2 "inválido" desinstalar-app immich:migrated agora
  pede 2 "inválido" remover-loja -rf
  quer="app start immich:migrated
app stop immich:migrated
app uninstall nextcloud_mini:minha-loja
appstore add minha-loja https://github.com/usuario/minha-loja
appstore remove minha-loja
app stop falha:migrated"
  if [ "$(cat /tmp/chamadas)" = "$quer" ]; then echo "ok: só os pedidos válidos chegaram ao runtipi-cli"; else echo "ERRO: chamadas ao runtipi-cli:"; cat /tmp/chamadas; falhou=1; fi
  echo "RUNTIPI=/nao/existe" >/etc/brunos-host.conf
  pede 3 "runtipi-cli" iniciar-app immich:migrated
  exit "$falhou"
'
echo "script-loja: ok"
