#!/usr/bin/env bash
#
# Bootstrap do hub na Pi, rodado da maquina de desenvolvimento.
#
# Envia install.sh + smart-home-hub.service para a Pi e executa o bootstrap
# remoto com sudo. Opcionalmente, se DEPLOY_CONFIG estiver definido, copia um
# config.json provisionado uma unica vez (nunca sobrescreve o existente).
#
# Uso: ./deploy/install-pi.sh
#
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${DEPLOY_ENV:-$DEPLOY_DIR/deploy.env}"
if [ -f "$ENV_FILE" ]; then
  # shellcheck disable=SC1090
  . "$ENV_FILE"
fi
# shellcheck source=lib.sh
. "$DEPLOY_DIR/lib.sh"

: "${PI_HOST:?defina PI_HOST em deploy/deploy.env (ou no ambiente)}"
PI_USER="${PI_USER:-pi}"
SERVICE_NAME="${SERVICE_NAME:-smart-home-hub}"
INSTALL_DIR="${INSTALL_DIR:-/opt/smart-home-hub}"
CONFIG_DIR="${CONFIG_DIR:-/etc/smart-home-hub}"
STATE_DIR="${STATE_DIR:-/var/lib/smart-home-hub}"
SERVICE_USER="${SERVICE_USER:-smart-home}"

SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=10)
REMOTE="$PI_USER@$PI_HOST"
# /tmp e limpo no reboot; staging por execucao.
STAGE="/tmp/smart-home-deploy.$$"

echo "== enviando bootstrap para $REMOTE:$STAGE"
deploy_require_ssh "$REMOTE"
rsync -az -e "ssh -o BatchMode=yes -o ConnectTimeout=10" --chmod=F0644 \
  "$DEPLOY_DIR/install.sh" \
  "$DEPLOY_DIR/smart-home-hub.service" \
  "$REMOTE:$STAGE/"

echo "== executando bootstrap"
ssh "${SSH_OPTS[@]}" "$REMOTE" \
  "sudo env SERVICE_NAME='$SERVICE_NAME' SERVICE_USER='$SERVICE_USER' \
   INSTALL_DIR='$INSTALL_DIR' CONFIG_DIR='$CONFIG_DIR' STATE_DIR='$STATE_DIR' \
   bash '$STAGE/install.sh'"

ssh "${SSH_OPTS[@]}" "$REMOTE" "rm -rf '$STAGE'"

if [ -n "${DEPLOY_CONFIG:-}" ]; then
  if [ ! -f "$DEPLOY_CONFIG" ]; then
    echo "ERRO: DEPLOY_CONFIG aponta para arquivo inexistente: $DEPLOY_CONFIG" >&2
    exit 1
  fi
  echo "== provisionando config.json (somente se ausente)"
  rsync -az -e "ssh -o BatchMode=yes -o ConnectTimeout=10" \
    --chmod=F0644 "$DEPLOY_CONFIG" "$REMOTE:$STAGE-config.json"
  ssh "${SSH_OPTS[@]}" "$REMOTE" sudo bash -s -- \
    "$STAGE-config.json" "$CONFIG_DIR/config.json" "$SERVICE_USER" <<'REMOTE_SCRIPT'
set -euo pipefail
STAGE_CFG="$1"; DEST_CFG="$2"; SERVICE_USER="$3"
if [ -f "$DEST_CFG" ]; then
  echo "config.json ja existe; mantido"
else
  install -o root -g "$SERVICE_USER" -m 0640 "$STAGE_CFG" "$DEST_CFG"
  echo "config.json instalado em $DEST_CFG"
fi
rm -f "$STAGE_CFG"
REMOTE_SCRIPT
fi

echo
# Se o binario ja foi deployado, reinicia para aplicar config/unit novos.
ssh "${SSH_OPTS[@]}" "$REMOTE" \
  "if [ -x '$INSTALL_DIR/smart-home-hub' ]; then sudo systemctl restart '$SERVICE_NAME' && echo 'servico reiniciado'; fi"

echo
echo "OK. Proximos passos:"
echo "  1) ajuste $CONFIG_DIR/config.json (endpoint, Tuya)"
echo "  2) ./deploy/seed-db.sh   # opcional: semeia o banco desta maquina"
echo "  3) ./deploy/deploy.sh    # build + envio do binario"
