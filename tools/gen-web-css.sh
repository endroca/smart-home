#!/usr/bin/env bash
#
# Gera o CSS compilado do Tailwind a partir de web_ui.h e grava
# firmware/smart_control/src/web_ui_css.h (embutido no firmware).
#
# Precisa de Node/npm apenas para regenerar; o firmware roda offline.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTENT="$ROOT/firmware/smart_control/src/web_ui.h"
OUTPUT="$ROOT/firmware/smart_control/src/web_ui_css.h"

command -v npm >/dev/null 2>&1 || {
  echo "ERRO: npm nao encontrado (necessario apenas para regenerar o CSS)" >&2
  exit 1
}

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/input.css" <<'CSS'
@tailwind base;
@tailwind components;
@tailwind utilities;
CSS

cat > "$TMP/tailwind.config.js" <<JS
module.exports = {
  content: ["$CONTENT"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        brand: {
          bg: "#121316",
          card: "#1E2025",
          cardBorder: "#2E323A",
          emerald: "#10B981",
          emeraldLight: "#34D399",
          amber: "#F59E0B",
          amberLight: "#FBBF24",
          ruby: "#EF4444"
        }
      },
      fontFamily: {
        sans: ["-apple-system", "BlinkMacSystemFont", "SF Pro Display", "Segoe UI", "Roboto", "sans-serif"]
      }
    }
  },
  plugins: [require("@tailwindcss/forms")]
};
JS

npm install --silent --prefix "$TMP" tailwindcss@3.4.17 @tailwindcss/forms >/dev/null

"$TMP/node_modules/.bin/tailwindcss" \
  -c "$TMP/tailwind.config.js" \
  -i "$TMP/input.css" \
  -o "$TMP/out.css" \
  --minify

{
  echo '#pragma once'
  echo ''
  echo '// GERADO POR tools/gen-web-css.sh - nao editar a mao.'
  echo 'const char INDEX_CSS[] PROGMEM = R"css('
  cat "$TMP/out.css"
  echo ')css";'
} > "$OUTPUT"

chmod 644 "$OUTPUT"

echo "OK"
echo "  css:    $OUTPUT"
echo "  bytes:  $(wc -c < "$OUTPUT")"
