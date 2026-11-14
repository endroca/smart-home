#!/usr/bin/env bash
#
# Semeia o banco SQLite do hub na Raspberry Pi com o banco da sua maquina.
#
# Passo explicito e opt-in: NAO roda no deploy normal, para nao apagar o estado
# que a Pi ja criou (devices, automacoes, telemetria). Depois do primeiro seed,
# a Pi passa a ser a fonte de verdade.
#
# O snapshot e tirado com "VACUUM INTO", que gera um arquivo unico consistente
# (sem -wal/-shm) mesmo com o hub local rodando. Na Pi o servico e parado, o
# banco atual e salvo com timestamp, os sidecars velhos sao removidos e o
# snapshot e instalado com dono/modo corretos.
#
# Uso: ./deploy/seed-db.sh [--force]
#   --force  sobrescreve um banco ja existente na Pi (faz backup antes)
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

FORCE="${FORCE:-0}"
for arg in "$@"; do
  case "$arg" in
    --force|-f) FORCE=1 ;;
    *) echo "ERRO: argumento desconhecido: $arg" >&2; exit 2 ;;
  esac
done

: "${PI_HOST:?defina PI_HOST em deploy/deploy.env (ou no ambiente)}"
PI_USER="${PI_USER:-pi}"
SERVICE_NAME="${SERVICE_NAME:-smart-home-hub}"
SERVICE_USER="${SERVICE_USER:-smart-home}"
STATE_DIR="${STATE_DIR:-/var/lib/smart-home-hub}"
DB_SRC="${DB_SRC:-$HUB_DIR/hub.db}"
REMOTE_DB="${STATE_DIR}/hub.db"
HEALTH_URL="${HEALTH_URL:-http://$PI_HOST/api/health}"

command -v sqlite3 >/dev/null 2>&1 || {
  echo "ERRO: sqlite3 nao encontrado (necessario para o snapshot consistente)." >&2
  exit 1
}
[ -f "$DB_SRC" ] || {
  echo "ERRO: banco local nao encontrado: $DB_SRC" >&2
  exit 1
}

SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=10)
REMOTE="$PI_USER@$PI_HOST"
STAGE="/tmp/smart-home-seed.db.$$"
TMP_DIR="$(mktemp -d)"
SEED="$TMP_DIR/hub.db"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "== snapshot consistente de $DB_SRC"
sqlite3 "file:$DB_SRC?mode=ro" "VACUUM INTO '$SEED'"
chmod 600 "$SEED"
echo "   snapshot=$SEED ($(wc -c <"$SEED") bytes)"

echo "== enviando para $REMOTE:$STAGE"
deploy_require_ssh "$REMOTE"
rsync -az -e "ssh -o BatchMode=yes -o ConnectTimeout=10" \
  --chmod=F0600 "$SEED" "$REMOTE:$STAGE"

echo "== instalando na Pi"
ssh "${SSH_OPTS[@]}" "$REMOTE" sudo bash -s -- \
  "$STAGE" "$REMOTE_DB" "$SERVICE_NAME" "$SERVICE_USER" "$FORCE" <<'REMOTE_SCRIPT'
set -euo pipefail
STAGE="$1"; DB="$2"; SERVICE="$3"; SERVICE_USER="$4"; FORCE="$5"

if [ ! -s "$STAGE" ]; then
  echo "ERRO: staging vazio/invalido: $STAGE" >&2
  exit 1
fi
if [ -f "$DB" ] && [ "$FORCE" != "1" ]; then
  echo "ERRO: banco ja existe em $DB (use --force para sobrescrever)." >&2
  rm -f "$STAGE"
  exit 2
fi

systemctl stop "$SERVICE" 2>/dev/null || true

if [ -f "$DB" ]; then
  BACKUP="$DB.bak-$(date +%Y%m%d%H%M%S)"
  cp -f "$DB" "$BACKUP"
  echo "backup do banco atual: $BACKUP"
fi
# Remove o banco e os sidecars: um -wal antigo nao pode ser aplicado a um
# arquivo diferente.
rm -f "$DB" "$DB-wal" "$DB-shm"
install -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0600 "$STAGE" "$DB"
rm -f "$STAGE"

systemctl start "$SERVICE"
echo "banco instalado em $DB"
REMOTE_SCRIPT

echo "== health check: $HEALTH_URL"
healthy=0
for _ in $(seq 1 20); do
  body="$(curl -fsS --max-time 3 "$HEALTH_URL" 2>/dev/null || true)"
  if printf '%s' "$body" | grep -q '"ok":true'; then
    healthy=1
    break
  fi
  sleep 1
done

if [ "$healthy" -eq 1 ]; then
  echo "OK: banco semeado e hub no ar"
  exit 0
fi

echo "ERRO: hub nao respondeu 'ok' apos o seed. Verifique na Pi:" >&2
echo "  systemctl status $SERVICE" >&2
echo "  journalctl -u $SERVICE -n 50" >&2
exit 1
