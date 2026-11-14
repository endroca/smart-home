#!/usr/bin/env bash
#
# Deploy do hub Go na Raspberry Pi (armv7), rodado da maquina de desenvolvimento.
#
# Fluxo: build do front (Svelte/Vite) -> cross-compile -> sha256 -> rsync ->
# install atomico -> restart -> health check. Se o health check nao confirmar a
# versao nova, restaura o binario anterior (.prev) e reinicia.
#
# O front e compilado para web/dist (embutido no binario Go), entao um unico
# comando ja publica UI + backend. Com BUILD_WEB=0 o build do front e pulado e
# o web/dist do repositorio e usado como esta (util sem Node na maquina).
#
# NAO toca em config.json, segredos, firmware/ nem no banco SQLite. Para semear
# o banco da sua maquina use ./deploy/seed-db.sh.
#
# Uso: ./deploy/deploy.sh          # front + backend
#      BUILD_WEB=0 ./deploy/deploy.sh
#
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HUB_DIR="$(cd "$DEPLOY_DIR/.." && pwd)"
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
HEALTH_URL="${HEALTH_URL:-http://$PI_HOST/api/health}"

SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=10)
REMOTE="$PI_USER@$PI_HOST"
STAGE="/tmp/smart-home-hub.stage.$$"
DIST_DIR="$HUB_DIR/dist"
BIN="$DIST_DIR/smart-home-hub-linux-armv7"
BIN_NAME="smart-home-hub"
WEB_DIR="$HUB_DIR/web/app"

VERSION="${VERSION:-$(git -C "$HUB_DIR" rev-parse --short HEAD 2>/dev/null || echo dev)}"
VERSION="${VERSION//[^A-Za-z0-9._-]/}"

# build_web compila o front (Svelte/Vite) para web/dist, que o binario Go
# embute. Sem Node cai para o dist do repositorio; BUILD_WEB=0 desliga.
build_web() {
  if [ "${BUILD_WEB:-1}" != "1" ]; then
    echo "== front: pulado (BUILD_WEB=${BUILD_WEB:-1}); usando web/dist do repositorio"
    return 0
  fi
  if ! command -v npm >/dev/null 2>&1; then
    if [ -f "$HUB_DIR/web/dist/index.html" ]; then
      echo "AVISO: npm nao encontrado; usando web/dist do repositorio" >&2
      return 0
    fi
    echo "ERRO: npm nao encontrado e web/dist ausente (instale Node ou rode o build do front)" >&2
    exit 1
  fi
  echo "== front (Svelte/Vite) -> web/dist"
  if [ ! -d "$WEB_DIR/node_modules" ]; then
    ( cd "$WEB_DIR" && npm ci --no-fund --no-audit )
  fi
  ( cd "$WEB_DIR" && npm run build )
}

build_web

echo "== build armv7 ($VERSION)"
mkdir -p "$DIST_DIR"
(
  cd "$HUB_DIR"
  GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X smart_home/hub/internal/hub.Version=$VERSION" \
    -o "$BIN" ./cmd/smart-home-hub
)

SHA="$(sha256sum "$BIN" | awk '{print $1}')"
echo "   binario=$BIN"
echo "   sha256=$SHA"

deploy_require_ssh "$REMOTE"

echo "== enviando para $REMOTE:$STAGE"
rsync -az -e "ssh -o BatchMode=yes -o ConnectTimeout=10" \
  --chmod=F0644 "$BIN" "$REMOTE:$STAGE"

echo "== instalando e reiniciando"
ssh "${SSH_OPTS[@]}" "$REMOTE" sudo bash -s -- \
  "$STAGE" "$INSTALL_DIR/$BIN_NAME" "$SERVICE_NAME" "$SHA" <<'REMOTE_SCRIPT'
set -euo pipefail
STAGE="$1"; BIN="$2"; SERVICE="$3"; WANT_SHA="$4"
if [ ! -s "$STAGE" ]; then
  echo "ERRO: staging vazio/invalido: $STAGE" >&2
  exit 1
fi
GOT_SHA="$(sha256sum "$STAGE" | awk '{print $1}')"
if [ "$GOT_SHA" != "$WANT_SHA" ]; then
  echo "ERRO: sha256 divergente (esperado $WANT_SHA, obtido $GOT_SHA)" >&2
  rm -f "$STAGE"
  exit 1
fi
if [ -f "$BIN" ]; then
  cp -f "$BIN" "$BIN.prev"
fi
install -m 0755 "$STAGE" "$BIN"
rm -f "$STAGE"
systemctl restart "$SERVICE"
REMOTE_SCRIPT

echo "== health check: $HEALTH_URL"
healthy=0
for _ in $(seq 1 20); do
  body="$(curl -fsS --max-time 3 "$HEALTH_URL" 2>/dev/null || true)"
  if printf '%s' "$body" | grep -q "\"version\":\"$VERSION\""; then
    healthy=1
    break
  fi
  sleep 1
done

if [ "$healthy" -eq 1 ]; then
  echo "OK: hub $VERSION no ar ($HEALTH_URL)"
  exit 0
fi

echo "ERRO: health check nao confirmou a versao $VERSION em $HEALTH_URL" >&2
echo "== rollback para o binario anterior"
ssh "${SSH_OPTS[@]}" "$REMOTE" sudo bash -s -- \
  "$INSTALL_DIR/$BIN_NAME" "$SERVICE_NAME" <<'REMOTE_SCRIPT'
set -euo pipefail
BIN="$1"; SERVICE="$2"
if [ -f "$BIN.prev" ]; then
  install -m 0755 "$BIN.prev" "$BIN"
  systemctl restart "$SERVICE"
  echo "rollback aplicado (.prev)"
else
  echo "AVISO: sem .prev; nenhum rollback possivel" >&2
fi
REMOTE_SCRIPT
exit 1
