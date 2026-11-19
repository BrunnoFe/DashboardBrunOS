#!/bin/bash
# Testa os pedidos do menu de energia do brunos-host (script 17) de verdade, como root, num contêiner
# descartável. O systemctl, o systemd-run e o shutdown são falsos e anotam como foram chamados.
# Confere: reiniciar e desligar-agendado esperam 1 minuto num temporizador do systemd (e não com
# "systemctl --when", que cria o /run/nologin e barrava a chave do BrunOS: não dava para cancelar, 0.25.0);
# um segundo pedido com o primeiro agendado é recusado; cancelar para o temporizador e, sem ele, cai no
# --when=cancel / shutdown -c; o "desligar" da bateria continua igual; pedidos com sobra são recusados.
# Uso: bash tests/script-energia.sh   (precisa de Go e Docker)
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
  # o script fixa PATH=/usr/sbin:... : os falsos ficam em /usr/sbin, na frente dos de verdade
  fake() { printf "#!/bin/sh\n%s\n" "$2" >"/usr/sbin/$1"; chmod 755 "/usr/sbin/$1"; }
  fake systemctl '"'"'echo "systemctl $*" >>/tmp/chamadas
case "$*" in
  "is-active --quiet brunos-energia.timer") [ -f /tmp/agendado ] ;;
  "stop brunos-energia.timer") rm -f /tmp/agendado ;;
  "poweroff --when=cancel") [ ! -f /tmp/systemd-antigo ] ;;
  *) exit 0 ;;
esac'"'"'
  fake systemd-run '"'"'echo "systemd-run $*" >>/tmp/chamadas; touch /tmp/agendado'"'"'
  fake shutdown '"'"'echo "shutdown $*" >>/tmp/chamadas; [ -f /tmp/nada-agendado ] && exit 1; exit 0'"'"'
  limpa() { : >/tmp/chamadas; rm -f /tmp/agendado; }

  limpa
  [ "$($H reiniciar)" = agendado ] && grep -q "^systemd-run --unit=brunos-energia --on-active=60 .*systemctl reboot --check-inhibitors=no$" /tmp/chamadas \
    && ! grep -q -- "--when" /tmp/chamadas && ok "reiniciar num temporizador de 1 minuto (sem --when)" || erro "reiniciar: $(cat /tmp/chamadas)"
  if r=$($H desligar-agendado 2>&1); then erro "segundo pedido com o primeiro agendado deveria ser recusado: $r"; else case "$r" in *"já tem"*) ok "segundo pedido recusado" ;; *) erro "segundo pedido: $r" ;; esac; fi
  : >/tmp/chamadas
  [ "$($H cancelar-desligar)" = cancelado ] && grep -qx "systemctl stop brunos-energia.timer" /tmp/chamadas && [ ! -f /tmp/agendado ] && ok "cancelar para o temporizador" || erro "cancelar: $(cat /tmp/chamadas)"
  limpa
  [ "$($H desligar-agendado)" = agendado ] && grep -q "systemctl poweroff --check-inhibitors=no$" /tmp/chamadas && ok "desligar-agendado num temporizador" || erro "desligar-agendado: $(cat /tmp/chamadas)"
  [ ! -e /run/nologin ] && ok "nada cria o /run/nologin" || erro "/run/nologin existe"
  limpa
  $H desligar && grep -qx "systemctl poweroff --check-inhibitors=no --when=+1min" /tmp/chamadas && ok "desligar da bateria continua igual" || erro "desligar: $(cat /tmp/chamadas)"
  limpa
  [ "$($H cancelar-desligar)" = cancelado ] && grep -qx "systemctl poweroff --when=cancel" /tmp/chamadas && ok "sem temporizador, cancela pelo --when=cancel" || erro "cancelar antigo: $(cat /tmp/chamadas)"
  limpa; touch /tmp/systemd-antigo
  [ "$($H cancelar-desligar)" = cancelado ] && grep -qx "shutdown -c" /tmp/chamadas && ok "num systemd antigo cai no shutdown -c" || erro "shutdown -c: $(cat /tmp/chamadas)"
  touch /tmp/nada-agendado
  if r=$($H cancelar-desligar 2>&1); then erro "cancelar sem nada agendado deveria falhar: $r"; else case "$r" in *"não deu para cancelar"*) ok "cancelar sem nada agendado avisa" ;; *) erro "cancelar sem nada: $r" ;; esac; fi
  for bad in "reiniciar agora" "desligar-agendado 60" "cancelar-desligar ja"; do
    if r=$(SSH_ORIGINAL_COMMAND="$bad" $H 2>&1); then erro "\"$bad\" deveria ser recusado: $r"; else ok "recusa \"$bad\""; fi
  done
  [ "$($H versao)" = "brunos-host 18" ] && ok "versão 18" || erro "versão: $($H versao)"
  exit $falhou
'
echo "script-energia: tudo certo"
