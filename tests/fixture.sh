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
echo '{"name":"Syncthing","id":"syncthing","port":8384,"short_desc":"Sincronização de arquivos","categories":["utilities"],"version":"1.27"}' > "$T"/runtipi/apps/migrated/syncthing/config.json
printf 'APP_PORT=8384\nAPP_LOCAL_DOMAIN=syncthing-migrated.server.lan\nAPP_PROTOCOL=https\nSOME_PASSWORD=supersecreto\nLOCAL_DOMAIN=server.lan\nINTERNAL_IP=192.168.1.50\n' > "$T"/runtipi/app-data/migrated/syncthing/app.env
echo '{"name":"Dashdot Lite","id":"dashdot-lite","port":3001}' > "$T"/runtipi/apps/personal-store/dashdot-lite/config.json
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
echo "fixture pronta em $T"
