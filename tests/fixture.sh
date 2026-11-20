#!/usr/bin/env bash
# Monta um "servidor de mentira" em $1: pastas do usuário, discos, um link simbólico
# que aponta para fora (para testar segurança) e uma pasta do Runtipi com dois apps.
set -euo pipefail
T="${1:?uso: fixture.sh <pasta>}"
rm -rf "$T"
mkdir -p "$T"/data "$T"/files/home/usuario/{Documentos,Fotos} "$T"/files/mnt/Disco1/Filmes "$T"/files/mnt/Disco2 "$T"/outside \
  "$T"/runtipi/apps/migrated/syncthing/metadata "$T"/runtipi/app-data/migrated/syncthing \
  "$T"/runtipi/apps/personal-store/dashdot-lite/metadata "$T"/runtipi/app-data/personal-store/dashdot-lite "$T"/runtipi/state
echo segredo > "$T"/outside/secret.txt
echo "oi" > "$T"/files/home/usuario/Documentos/notas.md
ln -s "$T"/outside "$T"/files/home/usuario/fuga
# ocultos da pasta pessoal (revisão de segurança, lote 3): só leitura; a chave privada nem aparece
mkdir -p "$T"/files/home/usuario/.ssh
echo 'CHAVE-PRIVADA-DE-MENTIRA' > "$T"/files/home/usuario/.ssh/id_ed25519
echo 'ssh-ed25519 AAAA-de-mentira usuario@exemplo' > "$T"/files/home/usuario/.ssh/id_ed25519.pub
echo 'ssh-ed25519 AAAA-de-mentira outro@exemplo' > "$T"/files/home/usuario/.ssh/authorized_keys
echo 'alias ll="ls -l"' > "$T"/files/home/usuario/.bashrc
echo '{"name":"Syncthing","id":"syncthing","port":8384,"short_desc":"Sincronização de arquivos","categories":["utilities"],"version":"1.27"}' > "$T"/runtipi/apps/migrated/syncthing/config.json
printf 'APP_PORT=8384\nAPP_LOCAL_DOMAIN=syncthing-migrated.server.lan\nAPP_PROTOCOL=https\nSOME_PASSWORD=supersecreto\nLOCAL_DOMAIN=server.lan\nINTERNAL_IP=192.168.1.50\n' > "$T"/runtipi/app-data/migrated/syncthing/app.env
echo '{"name":"Dashdot Lite","id":"dashdot-lite","port":3001}' > "$T"/runtipi/apps/personal-store/dashdot-lite/config.json
# logo de app instalado que é atalho para fora (loja hostil): não pode ser servido
ln -s "$T"/outside/secret.txt "$T"/runtipi/apps/personal-store/dashdot-lite/metadata/logo.jpg
echo '{"localDomain":"server.lan"}' > "$T"/runtipi/state/settings.json
# JPEG mínimo (16×16) usado como logo de app e como foto
JPEG='/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAA0JCgsKCA0LCgsODg0PEyAVExISEyccHhcgLikxMC4pLSwzOko+MzZGNywtQFdBRkxOUlNSMj5aYVpQYEpRUk//2wBDAQ4ODhMREyYVFSZPNS01T09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT0//wAARCAAQABADASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwDLooorhPqz/9k='
echo "$JPEG" | base64 -d > "$T"/runtipi/apps/migrated/syncthing/metadata/logo.jpg
echo "$JPEG" | base64 -d > "$T"/files/home/usuario/Fotos/foto.jpg
# Loja (0.18.0): a loja "migrated" baixada pelo Runtipi, com um app instalado, um por instalar, um
# escondido e um "malicioso" cujo logo e descrição são atalhos para fora
R="$T"/runtipi/repos/migrated
mkdir -p "$R"/.git "$R"/apps/{syncthing,jellyfin,oculto,atalho}/metadata
printf '[remote "origin"]\n\turl = https://usuario:segredo@github.com/exemplo/loja.git\n' > "$R"/.git/config
echo '{"name":"Syncthing","id":"syncthing","port":8384,"short_desc":"Sincronização de arquivos","categories":["utilities"],"version":"1.27","tipi_version":3,"available":true}' > "$R"/apps/syncthing/config.json
echo "$JPEG" | base64 -d > "$R"/apps/syncthing/metadata/logo.jpg
echo '{"name":"Jellyfin","id":"jellyfin","port":8096,"short_desc":"Filmes e séries","categories":["media"],"version":"10.9","tipi_version":7,"available":true,"author":"jellyfin","source":"https://github.com/jellyfin/jellyfin","created_at":1700000000000,"updated_at":1750000000000,"form_fields":[{"type":"text","label":"Pasta dos filmes","required":true,"env_variable":"MEDIA"},{"type":"random","label":"Segredo","env_variable":"S"}]}' > "$R"/apps/jellyfin/config.json
echo "$JPEG" | base64 -d > "$R"/apps/jellyfin/metadata/logo.jpg
printf '# Jellyfin\n\nServidor de **mídia** livre.\n' > "$R"/apps/jellyfin/metadata/description.md
echo '{"name":"Oculto","id":"oculto","version":"1","tipi_version":1,"available":false}' > "$R"/apps/oculto/config.json
echo '{"name":"Atalho","id":"atalho","version":"1","tipi_version":1,"available":true}' > "$R"/apps/atalho/config.json
ln -s "$T"/outside/secret.txt "$R"/apps/atalho/metadata/logo.jpg
ln -s "$T"/outside/secret.txt "$R"/apps/atalho/metadata/description.md
# Loja do Garimpo (0.19.0): o ramo "casaos" do repositório, com o garimpo.json da origem
G="$T"/runtipi/repos/casaos
mkdir -p "$G"/.git "$G"/apps/alist/metadata
printf '[remote "origin"]\n\turl = https://github.com/BrunnoFe/Garimpo\n' > "$G"/.git/config
echo 'ref: refs/heads/casaos' > "$G"/.git/HEAD
echo '{"brunos":1,"nome":"CasaOS","selo":"via CasaOS","fonte":"https://github.com/IceWhaleTech/CasaOS-AppStore","licenca":"Apache-2.0","sobre":"Apps do CasaOS."}' > "$G"/garimpo.json
echo '{"name":"AList","id":"alist","port":5244,"short_desc":"Lista de arquivos","categories":["utilities"],"version":"v3.45","tipi_version":1,"available":true}' > "$G"/apps/alist/config.json
echo "$JPEG" | base64 -d > "$G"/apps/alist/metadata/logo.jpg
# Receitas (0.21.0): AList com o socket do Docker (requer privilégios), Jellyfin comum em JSON e uma
# receita que é atalho para fora (não pode ser lida)
printf 'services:\n  alist:\n    image: xhofe/alist:v3.45\n    volumes:\n      - ${APP_DATA_DIR}/data:/opt/alist/data\n      - /var/run/docker.sock:/var/run/docker.sock\n' > "$G"/apps/alist/docker-compose.yml
echo '{"schemaVersion":2,"services":[{"name":"jellyfin","image":"jellyfin/jellyfin:10.9","isMain":true,"internalPort":8096,"environment":[{"key":"TZ","value":"${TZ}"}],"volumes":[{"hostPath":"${APP_DATA_DIR}/data/config","containerPath":"/config"}]}]}' > "$R"/apps/jellyfin/docker-compose.json
ln -s "$T"/outside/secret.txt "$R"/apps/atalho/docker-compose.yml
echo "fixture pronta em $T"
