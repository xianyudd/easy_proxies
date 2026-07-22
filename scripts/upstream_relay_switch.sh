#!/usr/bin/env bash
# Switch EP upstream_proxy to the local relay (default) or re-pin relay outbound then switch.
# Usage:
#   scripts/upstream_relay_switch.sh              # EP -> socks5://127.0.0.1:17890
#   scripts/upstream_relay_switch.sh gemini       # re-render relay from tag, restart relay, switch EP
#   scripts/upstream_relay_switch.sh --no-restart # only write EP config
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RELAY_URI="${RELAY_URI:-socks5://127.0.0.1:17890}"
UNIT_MAIN="${UNIT_MAIN:-easy_proxies_main.service}"
UNIT_UP="${UNIT_UP:-easy_proxies_upstream.service}"
CONTAINER="${CONTAINER:-easy_proxies_main}"
NO_RESTART=0

ARGS=()
for a in "$@"; do
  case "$a" in
    --no-restart) NO_RESTART=1 ;;
    *) ARGS+=("$a") ;;
  esac
done

if [[ ${#ARGS[@]} -gt 0 ]]; then
  "$ROOT/scripts/upstream_relay_render.sh" "${ARGS[0]}"
  systemctl --user restart "$UNIT_UP"
  sleep 1
  systemctl --user is-active "$UNIT_UP"
fi

podman exec -u 0 "$CONTAINER" sh -c "
set -e
cp -a /app/config.yaml /app/config.yaml.bak-upstream-switch-\$(date +%Y%m%d%H%M%S) 2>/dev/null || true
sed -i 's|^upstream_proxy:.*|upstream_proxy: ${RELAY_URI}|' /app/config.yaml
grep -n '^upstream_proxy' /app/config.yaml
"

if [[ "$NO_RESTART" -eq 0 ]]; then
  systemctl --user restart "$UNIT_MAIN"
  echo "restarted $UNIT_MAIN"
fi

echo "EP upstream_proxy -> $RELAY_URI"
echo "Note: system Clash/7890 is untouched."
