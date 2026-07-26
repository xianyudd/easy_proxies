#!/usr/bin/env bash
# Rollback EP upstream_proxy to system Clash SOCKS (default 127.0.0.1:7890).
# Usage:
#   scripts/upstream_relay_rollback.sh
#   UPSTREAM=socks5://127.0.0.1:7890 scripts/upstream_relay_rollback.sh
set -euo pipefail

UPSTREAM="${UPSTREAM:-socks5://127.0.0.1:7890}"
UNIT_MAIN="${UNIT_MAIN:-easy_proxies_main.service}"
CONTAINER="${CONTAINER:-easy_proxies_main}"
STOP_RELAY="${STOP_RELAY:-0}"

podman exec -u 0 "$CONTAINER" sh -c "
set -e
cp -a /app/config.yaml /app/config.yaml.bak-upstream-rollback-\$(date +%Y%m%d%H%M%S) 2>/dev/null || true
sed -i 's|^upstream_proxy:.*|upstream_proxy: ${UPSTREAM}|' /app/config.yaml
grep -n '^upstream_proxy' /app/config.yaml
"

systemctl --user restart "$UNIT_MAIN"
echo "EP upstream_proxy rolled back -> $UPSTREAM"

if [[ "$STOP_RELAY" == "1" ]]; then
  systemctl --user stop easy_proxies_upstream.service || true
  echo "stopped easy_proxies_upstream"
fi
