#!/usr/bin/env bash
#
# Bootstrap do servico do hub na Raspberry Pi. Roda NA Pi, com sudo.
#
# Cria o usuario de sistema, os diretorios persistentes, instala a unit systemd
# e habilita o servico no boot. Nao copia config.json nem segredos: esses sao
# provisionados uma unica vez por voce.
#
# Normalmente chamado por deploy/install-pi.sh (make install-pi) a partir da
# maquina de desenvolvimento; tambem pode ser rodado direto na Pi:
#   sudo ./install.sh
#
set -euo pipefail

SERVICE_NAME="${SERVICE_NAME:-smart-home-hub}"
SERVICE_USER="${SERVICE_USER:-smart-home}"
INSTALL_DIR="${INSTALL_DIR:-/opt/smart-home-hub}"
CONFIG_DIR="${CONFIG_DIR:-/etc/smart-home-hub}"
STATE_DIR="${STATE_DIR:-/var/lib/smart-home-hub}"
UNIT_SRC="${UNIT_SRC:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/smart-home-hub.service}"

if [ "$(id -u)" -ne 0 ]; then
  echo "ERRO: rode com sudo (precisa criar usuario e unit systemd)." >&2
  exit 1
fi

if [ ! -f "$UNIT_SRC" ]; then
  echo "ERRO: unit nao encontrada em $UNIT_SRC" >&2
  echo "      rode este script a partir do diretorio deploy/ do repo." >&2
  exit 1
fi

echo "== usuario de sistema: $SERVICE_USER"
if id "$SERVICE_USER" >/dev/null 2>&1; then
  echo "   ja existe; mantido"
else
  useradd --system --no-create-home --home-dir "$INSTALL_DIR" \
    --shell /usr/sbin/nologin "$SERVICE_USER"
fi

echo "== diretorios"
install -d -m 0755 "$INSTALL_DIR"
install -d -m 0755 "$INSTALL_DIR/firmware"
install -d -m 0750 -o root -g "$SERVICE_USER" "$CONFIG_DIR"
install -d -m 0700 -o "$SERVICE_USER" -g "$SERVICE_USER" "$STATE_DIR"

echo "== unit systemd"
install -m 0644 -o root -g root "$UNIT_SRC" "/etc/systemd/system/$SERVICE_NAME.service"
systemctl daemon-reload
systemctl enable "$SERVICE_NAME"

if [ -f "$CONFIG_DIR/config.json" ]; then
  echo "== reiniciando servico"
  systemctl restart "$SERVICE_NAME"
  echo
  echo "OK. Status: systemctl status $SERVICE_NAME"
else
  echo
  echo "AVISO: $CONFIG_DIR/config.json ainda nao existe."
  echo "Copie o config e entao rode:"
  echo "  sudo systemctl start $SERVICE_NAME"
fi
