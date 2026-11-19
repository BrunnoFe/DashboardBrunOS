#!/bin/bash
# Testa os pedidos da 0.20.0 do brunos-host (script 14) de verdade, como root, num contêiner descartável,
# com curl e docker falsos que anotam como foram chamados:
#  - instalar, editar a instalação, reiniciar e ler o app pela API do Runtipi: endereço certo, chave do
#    runtipi-cli (dentro do container no Runtipi 4.12+, JWT_SECRET do .env antes), token que confere
#    (HS256, sub "cli") e que NUNCA aparece na linha de comando, corpo do pedido e erros do Runtipi;
#  - Ajustes: ler, gravar só com a Senha de configuração, cópia do arquivo anterior, apagar, atalho recusado.
# Uso: bash tests/script-instalar.sh   (precisa de Go e Docker)
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
go run ./tools/hostscript >"$work/brunos-host"

cat >"$work/teste.sh" <<'TESTE'
set -u
apt-get update -qq >/dev/null && apt-get install -y -qq python3 >/dev/null || { echo "ERRO: não deu para instalar o python3"; exit 1; }
install -m 755 /w/brunos-host /usr/local/sbin/brunos-host
H=/usr/local/sbin/brunos-host
falhou=0
erro() { echo "ERRO: $*"; falhou=1; }
R=/opt/runtipi
mkdir -p $R/apps/casaos/alist $R/apps/migrated/plex $R/repos/casaos $R/repos/migrated
printf '#!/bin/sh\necho ok\n' >$R/runtipi-cli && chmod +x $R/runtipi-cli
printf 'INTERNAL_IP=192.168.1.50\nNGINX_PORT=8080\nJWT_SECRET="segredo-antigo"\n' >$R/.env
echo "v4.12.0" >$R/VERSION
echo "RUNTIPI=$R" >/etc/brunos-host.conf
SEG=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
# docker falso: só "docker exec runtipi cat /data/.secrets/cli-secret"
cat >/usr/bin/docker <<EOF
#!/bin/sh
echo "\$*" >>/tmp/docker.log
[ "\$*" = "exec runtipi cat /data/.secrets/cli-secret" ] && printf '%s' "$SEG" && exit 0
exit 1
EOF
chmod +x /usr/bin/docker
# curl falso: anota os argumentos, a configuração (-K) e o corpo; responde o que estiver em /tmp/resp.*
cat >/usr/bin/curl <<'EOF'
#!/bin/sh
echo "$*" >>/tmp/curl.args
out=""; cfg=""; corpo=""; url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -K) cfg=$2; shift ;;
    -o) out=$2; shift ;;
    --data-binary) corpo=${2#@}; shift ;;
    -X|-m|-w|-H) shift ;;
    http*) url=$1 ;;
  esac
  shift
done
cp "$cfg" /tmp/curl.cfg
echo "$url" >>/tmp/curl.urls
if [ -n "$corpo" ]; then cp "$corpo" /tmp/curl.corpo; else : >/tmp/curl.corpo; fi
cod=$(cat /tmp/resp.cod 2>/dev/null || echo 200)
cat /tmp/resp.body >"$out" 2>/dev/null || echo '{}' >"$out"
printf '%s' "$cod"
EOF
chmod +x /usr/bin/curl
pede() { # pede <código> <texto> <pedido...>
  quer=$1; texto=$2; shift 2
  saida=$("$H" "$@" 2>&1 </dev/null); codigo=$?
  if [ "$codigo" -eq "$quer" ] && echo "$saida" | grep -q -- "$texto"; then echo "ok: $* -> $codigo"; else erro "$* -> $codigo: $saida"; fi
}
token() { sed -n 's/^header = "Authorization: Bearer \(.*\)"$/\1/p' /tmp/curl.cfg; }
confere_token() { # confere_token <segredo>
  printf '%s' "$(token)" | python3 -c "
import base64,hashlib,hmac,json,sys,time
t=sys.stdin.read().split('.'); k=sys.argv[1].encode()
pad=lambda s: s+'='*(-len(s)%4)
ok=hmac.compare_digest(base64.urlsafe_b64encode(hmac.new(k,(t[0]+'.'+t[1]).encode(),hashlib.sha256).digest()).rstrip(b'=').decode(),t[2])
h=json.loads(base64.urlsafe_b64decode(pad(t[0]))); p=json.loads(base64.urlsafe_b64decode(pad(t[1])))
n=time.time()
sys.exit(0 if ok and h['alg']=='HS256' and p['sub']=='cli' and p['nbf']<=n<p['exp']<=n+300 else 1)" "$1"
}

# ── API do Runtipi (4.12+: chave de dentro do container) ──────────────────
echo '{"app":{"status":"running"}}' >/tmp/resp.body
pede 0 running info-app alist:casaos
[ "$(tail -n 1 /tmp/curl.urls)" = "http://192.168.1.50:8080/api/apps/alist:casaos" ] && echo "ok: endereço da API" || erro "endereço: $(tail -n 1 /tmp/curl.urls)"
confere_token "$SEG" && echo "ok: token com a chave do CLI" || erro "token não confere com a chave do CLI"
grep -q "exec runtipi cat /data/.secrets/cli-secret" /tmp/docker.log && echo "ok: chave lida pelo docker exec" || erro "não leu a chave pelo docker"
if grep -q "Bearer\|$SEG" /tmp/curl.args; then erro "token ou chave apareceram na linha de comando"; else echo "ok: token e chave fora da linha de comando"; fi
[ ! -s /tmp/curl.corpo ] && echo "ok: GET sem corpo" || erro "GET mandou corpo"
echo '{"requestId":"abc"}' >/tmp/resp.body
saida=$(printf '{"port":8123,"exposed":false}' | "$H" instalar-app alist:casaos 2>&1); c=$?
[ $c -eq 0 ] && echo "$saida" | grep -q abc && echo "ok: instalar" || erro "instalar -> $c: $saida"
[ "$(cat /tmp/curl.corpo)" = '{"port":8123,"exposed":false}' ] && echo "ok: formulário chegou inteiro" || erro "corpo: $(cat /tmp/curl.corpo)"
grep -q -- "-X POST" /tmp/curl.args && tail -n 1 /tmp/curl.urls | grep -q "/api/app-lifecycle/alist:casaos/install$" && echo "ok: POST install" || erro "install: $(tail -n 1 /tmp/curl.urls)"
: >/tmp/curl.urls
printf '{"port":8124}' | "$H" reconfigurar-app alist:casaos >/dev/null 2>&1 && [ "$(cat /tmp/curl.urls)" = "http://192.168.1.50:8080/api/app-lifecycle/alist:casaos/update-config
http://192.168.1.50:8080/api/app-lifecycle/alist:casaos/restart" ] && echo "ok: editar e reiniciar" || erro "reconfigurar: $(cat /tmp/curl.urls)"
echo 400 >/tmp/resp.cod; echo '{"statusCode":400,"message":"APP_ERROR_PORT_ALREADY_IN_USE"}' >/tmp/resp.body
saida=$(printf '{}' | "$H" instalar-app alist:casaos 2>&1); c=$?
[ $c -eq 1 ] && echo "$saida" | grep -q PORT_ALREADY && echo "ok: erro do Runtipi volta" || erro "erro 400 -> $c: $saida"
rm -f /tmp/resp.cod
# auditoria 19/11 (script 18): sem senha, só instala de loja confiável (as que já existiam ou foram
# adicionadas pelo BrunOS); de outra loja, só pelo instalar-app-senha
grep -qx casaos /etc/brunos-host/lojas && echo "ok: lojas que já existiam viraram confiáveis" || erro "lista de lojas: $(cat /etc/brunos-host/lojas 2>&1)"
: >/tmp/curl.urls
saida=$(printf '{}' | "$H" instalar-app alist:outra-loja 2>&1); c=$?
[ $c -eq 8 ] && echo "$saida" | grep -q "não foi adicionada pelo BrunOS" && [ ! -s /tmp/curl.urls ] && echo "ok: loja não confiável não instala sem senha" || erro "loja não confiável -> $c: $saida"
# Runtipi antigo (antes da 4.12): chave é o JWT_SECRET do .env, sem docker
echo "v4.11.2" >$R/VERSION; : >/tmp/docker.log
pede 0 "" reiniciar-app alist:casaos
confere_token "segredo-antigo" && [ ! -s /tmp/docker.log ] && echo "ok: Runtipi antigo usa o JWT_SECRET" || erro "token do Runtipi antigo"
echo "v3.7.0" >$R/VERSION
pede 3 "não é aceita" info-app alist:casaos
echo "v4.12.0" >$R/VERSION
# curl que não conecta
cp /usr/bin/curl /tmp/curl.bom
printf '#!/bin/sh\necho "connection refused" >&2\nprintf 000\nexit 7\n' >/usr/bin/curl
pede 1 "não respondeu" info-app alist:casaos
cp /tmp/curl.bom /usr/bin/curl

# ── Ajustes ──────────────────────────────────────────────────────────────
U=$R/user-config/migrated/plex
pede 0 "" ajustes-ler plex:migrated
pede 0 "" ajustes-ler nada:migrated   # script 15: lê também de app não instalado (sobra de instalação antiga)
printf 'x\nservices: {}\n' | "$H" ajustes-gravar plex:migrated >/tmp/s 2>&1; c=$?
[ $c -eq 5 ] && grep -q "ainda não existe Senha de configuração" /tmp/s && echo "ok: gravar sem senha criada" || erro "sem senha -> $c: $(cat /tmp/s)"
mkdir -p /etc/brunos-host
printf "certa-12345" | python3 -c "
import hashlib,os,sys
s=sys.stdin.buffer.read();sal=os.urandom(16)
h=hashlib.scrypt(s,salt=sal,n=32768,r=8,p=1,maxmem=67108864,dklen=32)
print(\"scrypt\$32768\$8\$1\$%s\$%s\$2026-11-18T09:30:00\"%(sal.hex(),h.hex()))" >/etc/brunos-host/formatar
printf 'errada\nservices: {}\n' | "$H" ajustes-gravar plex:migrated >/tmp/s 2>&1; c=$?
[ $c -eq 7 ] && [ ! -e $U/docker-compose.yml ] && echo "ok: senha errada não grava" || erro "senha errada -> $c: $(cat /tmp/s)"
printf 'certa-12345\nservices:\n  plex:\n    volumes:\n      - /mnt/disco1/Filmes:/movies\n' | "$H" ajustes-gravar plex:migrated >/tmp/s 2>&1; c=$?
[ $c -eq 0 ] && grep -q "/mnt/disco1/Filmes:/movies" $U/docker-compose.yml && echo "ok: ajustes gravados" || erro "gravar -> $c: $(cat /tmp/s)"
pede 0 "/mnt/disco1/Filmes" ajustes-ler plex:migrated
sleep 1
printf 'certa-12345\nservices:\n  plex:\n    mem_limit: 2G\n' | "$H" ajustes-gravar plex:migrated >/dev/null 2>&1
ls $U/docker-compose.yml.brunos-*.bak >/dev/null 2>&1 && grep -q Filmes $U/docker-compose.yml.brunos-*.bak && grep -q "mem_limit: 2G" $U/docker-compose.yml && echo "ok: cópia do arquivo anterior" || erro "sem cópia: $(ls $U)"
printf 'certa-12345\n' | "$H" ajustes-gravar plex:migrated >/dev/null 2>&1
[ ! -e $U/docker-compose.yml ] && echo "ok: ajustes vazios apagam o arquivo" || erro "arquivo vazio ficou"
"$H" discos-estado | grep -q "senha-data 2026-11-18T09:30:00" && echo "ok: data da senha" || erro "discos-estado sem a data da senha"
ln -s /etc/passwd $U/docker-compose.yml
pede 3 "atalho" ajustes-ler plex:migrated
printf 'certa-12345\nservices: {}\n' | "$H" ajustes-gravar plex:migrated >/dev/null 2>&1
[ -L $U/docker-compose.yml ] && grep -q root /etc/passwd && echo "ok: atalho não é seguido ao gravar" || erro "seguiu o atalho"
{ echo certa-12345; head -c 70000 /dev/zero | tr '\0' 'a'; } | "$H" ajustes-gravar alist:casaos >/tmp/s 2>&1; c=$?
[ $c -eq 2 ] && grep -q "grande demais" /tmp/s && echo "ok: arquivo grande recusado" || erro "grande -> $c: $(cat /tmp/s)"

# ── Instalar com senha e Ajustes (script 15) ─────────────────────────────
N=$R/user-config/casaos/novo
rm -f /tmp/resp.cod; echo '{"requestId":"x1"}' >/tmp/resp.body; : >/tmp/curl.urls
printf 'errada\n{"port":8200}\nservices:\n  novo:\n    mem_limit: 1G\n' | "$H" instalar-app-senha novo:casaos >/tmp/s 2>&1; c=$?
[ $c -eq 7 ] && [ ! -e $N/docker-compose.yml ] && [ ! -s /tmp/curl.urls ] && echo "ok: instalar com senha errada não faz nada" || erro "senha errada -> $c: $(cat /tmp/s)"
printf 'certa-12345\n{"port":8200}\nservices:\n  novo:\n    mem_limit: 1G\n' | "$H" instalar-app-senha novo:casaos >/tmp/s 2>&1; c=$?
[ $c -eq 0 ] && grep -q "mem_limit: 1G" $N/docker-compose.yml && [ "$(cat /tmp/curl.corpo)" = '{"port":8200}' ] && tail -n 1 /tmp/curl.urls | grep -q "/novo:casaos/install$" \
  && echo "ok: Ajustes gravados antes de instalar" || erro "instalar com ajustes -> $c: $(cat /tmp/s) $(cat /tmp/curl.corpo)"
printf 'certa-12345\n{"port":8200}\n' | "$H" instalar-app-senha alist:casaos >/tmp/s 2>&1; c=$?
[ $c -eq 4 ] && echo "ok: app já instalado recusado" || erro "já instalado -> $c: $(cat /tmp/s)"
# Runtipi recusa: o arquivo anterior volta
echo 400 >/tmp/resp.cod; echo '{"message":"APP_ERROR_PORT_ALREADY_IN_USE"}' >/tmp/resp.body
sleep 1
printf 'certa-12345\n{"port":8200}\nservices:\n  novo:\n    mem_limit: 4G\n' | "$H" instalar-app-senha novo:casaos >/tmp/s 2>&1; c=$?
[ $c -eq 1 ] && grep -q PORT_ALREADY /tmp/s && grep -q "mem_limit: 1G" $N/docker-compose.yml && echo "ok: recusado pelo Runtipi, arquivo anterior volta" || erro "recusa -> $c: $(cat /tmp/s) $(cat $N/docker-compose.yml)"
rm -rf $N; printf 'certa-12345\n{"port":8200}\nservices:\n  novo:\n    mem_limit: 4G\n' | "$H" instalar-app-senha novo:casaos >/dev/null 2>&1
[ ! -e $N/docker-compose.yml ] && echo "ok: recusado sem arquivo anterior, nada fica" || erro "sobrou arquivo"
rm -f /tmp/resp.cod; echo '{}' >/tmp/resp.body
printf 'certa-12345\n{"port":8200}\n' | "$H" instalar-app-senha novo:casaos >/dev/null 2>&1 && [ ! -e $N/docker-compose.yml ] && echo "ok: só senha (privilégios), sem Ajustes" || erro "só senha"
# desfazer: só de app não instalado
mkdir -p $N && echo "services: {}" >$N/docker-compose.yml
pede 0 "desfeitos" ajustes-desfazer novo:casaos
[ ! -e $N/docker-compose.yml ] && ls $N/docker-compose.yml.brunos-*.bak >/dev/null 2>&1 && echo "ok: desfazer guarda cópia" || erro "desfazer: $(ls $N)"
pede 4 "instalado" ajustes-desfazer alist:casaos
exit "$falhou"
TESTE

docker run --rm -v "$work:/w:ro" debian:bookworm-slim bash /w/teste.sh
echo "script-instalar: ok"
