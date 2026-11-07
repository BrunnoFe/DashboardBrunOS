#!/usr/bin/env bash
# Teste de fumaça com Docker de verdade: sobe o BrunOS + o proxy do Docker com as mesmas
# restrições do Runtipi (somente leitura, sem capabilities, usuário comum) e roda a bateria
# de testes da API contra o container.
#   uso: bash tests/smoke.sh <imagem>     (ex.: brunos:ci)
set -euo pipefail
IMAGE="${1:-brunos:ci}"
FIX="${FIX:-/tmp/bt}"
PORT=8099
NET=brunos-smoke
ALLOW_GET='/v1\..{1,2}/(version|containers/json|containers/[a-zA-Z0-9_.-]+/stats)'
ALLOW_POST='/v1\..{1,2}/containers/[a-zA-Z0-9_.-]+/restart'

cleanup() {
  status=$?
  if [ $status -ne 0 ]; then
    echo "::group::logs do brunos"; docker logs brunos 2>&1 | tail -n 80 || true; echo "::endgroup::"
    echo "::group::logs do proxy"; docker logs brunos-docker-proxy 2>&1 | tail -n 40 || true; echo "::endgroup::"
  fi
  docker rm -f brunos brunos-docker-proxy >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
  exit $status
}
trap cleanup EXIT

bash "$(dirname "$0")/fixture.sh" "$FIX"
docker network create "$NET" >/dev/null

docker run -d --name brunos-docker-proxy --network "$NET" \
  --user 0:0 --read-only --cap-drop ALL --security-opt no-new-privileges:true --memory 32m \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  wollomatic/socket-proxy:1 \
  -loglevel=info -listenip=0.0.0.0 -allowfrom=brunos \
  "-allowGET=$ALLOW_GET" "-allowPOST=$ALLOW_POST" -shutdowngracetime=5 >/dev/null

docker run -d --name brunos --network "$NET" -p "127.0.0.1:$PORT:8080" \
  --user "$(id -u):$(id -g)" --read-only --cap-drop ALL --security-opt no-new-privileges:true --memory 128m \
  -e DOCKER_HOST=tcp://brunos-docker-proxy:2375 -e BRUNOS_DOCKER_ACTIONS=true \
  -v "$FIX/data:/data" -v "$FIX/runtipi:/runtipi:ro" -v "$FIX/files:/files" \
  -v "$FIX/outside:$FIX/outside" \
  -v /proc:/host/proc:ro -v /etc/os-release:/host/os-release:ro -v /etc/hostname:/host/hostname:ro \
  "$IMAGE" >/dev/null

echo "Aguardando o healthcheck do container…"
for i in $(seq 1 75); do
  st=$(docker inspect -f '{{.State.Health.Status}}' brunos 2>/dev/null || echo starting)
  [ "$st" = "healthy" ] && break
  [ "$st" = "unhealthy" ] && { echo "container ficou unhealthy"; exit 1; }
  sleep 1
done
[ "$st" = "healthy" ] || { echo "healthcheck não ficou verde (estado: $st)"; exit 1; }
echo "✓ healthcheck verde"

BASE="http://127.0.0.1:$PORT" FIX="$FIX" python3 "$(dirname "$0")/apitest.py"

# Integração com Docker, sistema e limites, com uma sessão nova
python3 - "$PORT" <<'EOF'
import json, sys, time, urllib.request, http.cookiejar
B = f"http://127.0.0.1:{sys.argv[1]}"
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
def req(m, p, b=None):
    r = urllib.request.Request(B + p, data=json.dumps(b).encode() if b is not None else None, method=m,
                               headers={"X-BrunOS": "1", "Content-Type": "application/json"})
    with op.open(r) as x:
        return json.loads(x.read() or b"null")
time.sleep(61)  # o bloqueio de 1 min do teste de força bruta precisa expirar
req("POST", "/api/auth/login", {"username": "bruno", "password": "nova-senha-123"})
apps = req("GET", "/api/apps")
assert apps["docker"] is True, f"Docker não conectou pelo proxy: {apps}"
print("✓ Docker conectado pelo proxy")
req("GET", "/api/usage"); time.sleep(3)
usage = req("GET", "/api/usage")
assert any(u["name"] in ("brunos", "brunos-docker-proxy") for u in usage), f"uso por container vazio: {usage}"
print("✓ estatísticas por container:", ", ".join(f"{u['name']} {u['cpu']}% {u['memory']//2**20} MiB" for u in usage))
sysinfo = req("GET", "/api/system")
assert sysinfo["info"]["os"], "não leu /etc/os-release do host"
print("✓ sistema:", sysinfo["info"]["os"], "·", sysinfo["info"]["cpuModel"])
st = req("GET", "/api/stats")["snapshot"]
assert st["mem"]["total"] > 0 and st["disks"], "estatísticas incompletas"
print(f"✓ estatísticas: CPU {st['cpu']['percent']}% · {len(st['disks'])} disco(s) · interfaces {st['net']['ifaces']}")
EOF

# O proxy precisa recusar quem não é o BrunOS e qualquer rota fora da lista
code=$(docker run --rm --network "$NET" curlimages/curl:8.10.1 -s -o /dev/null -w '%{http_code}' http://brunos-docker-proxy:2375/v1.41/containers/json || true)
[ "$code" = "403" ] || { echo "proxy deveria recusar outros containers (HTTP $code)"; exit 1; }
echo "✓ proxy recusa containers que não são o BrunOS"

mem=$(docker stats --no-stream --format '{{.MemUsage}}' brunos | cut -d/ -f1 | tr -d ' ')
echo "✓ memória do BrunOS em uso: $mem"
size=$(docker image inspect "$IMAGE" --format '{{.Size}}')
echo "✓ tamanho da imagem: $((size / 1024 / 1024)) MB"
echo "Tudo certo."
