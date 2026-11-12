#!/bin/bash
# Testa o "atualizar-script" do brunos-host de verdade, como root, num contêiner descartável (decisão 49):
#   assinatura certa + versão maior            → troca
#   versão igual, assinatura de outra chave,
#   script alterado depois de assinado,
#   assinatura ilegível                        → recusa (e o script instalado continua o mesmo)
# O script de teste confia numa chave criada aqui, no lugar da chave de publicação.
# Uso: bash tests/script-selfupdate.sh   (precisa de Go, ssh-keygen e Docker)
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

go run ./tools/hostscript >"$work/atual"
grep -q '^VERSAO=[0-9][0-9]*$' "$work/atual" || { echo "o script não tem a linha VERSAO="; exit 1; }
ver=$(sed -n 's/^VERSAO=\([0-9][0-9]*\)$/\1/p' "$work/atual")

ssh-keygen -q -t ed25519 -N '' -C teste -f "$work/chave"
ssh-keygen -q -t ed25519 -N '' -C outra -f "$work/outra"
pub=$(cut -d' ' -f1,2 "$work/chave.pub")
sed "s|^CHAVE_ASSINATURA='.*'\$|CHAVE_ASSINATURA='$pub'|" "$work/atual" >"$work/instalado"
grep -q "CHAVE_ASSINATURA='$pub'" "$work/instalado" || { echo "não troquei a chave do script de teste"; exit 1; }

sign() { # sign <arquivo> <chave> → assinatura em base64 numa linha
  ssh-keygen -q -Y sign -f "$2" -n brunos-host "$1" >/dev/null
  base64 -w0 "$1.sig"
  rm -f "$1.sig"
}
sed "s/^VERSAO=$ver\$/VERSAO=$((ver + 1))/" "$work/instalado" >"$work/novo"
cp "$work/instalado" "$work/igual"
sign "$work/novo" "$work/chave" >"$work/sig-novo"
sign "$work/igual" "$work/chave" >"$work/sig-igual"
sign "$work/novo" "$work/outra" >"$work/sig-outra"
cp "$work/novo" "$work/alterado"
echo '# mudado depois de assinado' >>"$work/alterado"

docker run --rm -v "$work:/w:ro" debian:bookworm-slim bash -c '
  set -u
  apt-get update -qq >/dev/null && apt-get install -y -qq openssh-client >/dev/null
  install -m 755 /w/instalado /usr/local/sbin/brunos-host
  falhou=0
  recusa() { # recusa <nome> <assinatura> <script>
    if /usr/local/sbin/brunos-host atualizar-script "$2" <"$3" >/tmp/out 2>&1; then
      echo "ERRO: $1 deveria ser recusado: $(cat /tmp/out)"; falhou=1
    else echo "ok: $1 recusado ($(cat /tmp/out))"; fi
  }
  recusa "versão igual" "$(cat /w/sig-igual)" /w/igual
  recusa "assinatura de outra chave" "$(cat /w/sig-outra)" /w/novo
  recusa "script alterado depois de assinado" "$(cat /w/sig-novo)" /w/alterado
  recusa "assinatura ilegível" "QUJDRA==" /w/novo
  cmp -s /usr/local/sbin/brunos-host /w/instalado || { echo "ERRO: um pedido recusado mudou o script"; falhou=1; }
  if /usr/local/sbin/brunos-host atualizar-script "$(cat /w/sig-novo)" </w/novo; then
    cmp -s /usr/local/sbin/brunos-host /w/novo && echo "ok: script assinado e mais novo instalado ($(/usr/local/sbin/brunos-host versao))" \
      || { echo "ERRO: instalou outra coisa"; falhou=1; }
    [ "$(stat -c %a:%U /usr/local/sbin/brunos-host)" = 755:root ] || { echo "ERRO: permissões erradas"; falhou=1; }
  else echo "ERRO: o script assinado deveria ser aceito"; falhou=1; fi
  exit $falhou
'
echo "atualizar-script: tudo certo"
