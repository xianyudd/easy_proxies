#!/usr/bin/env bash
set -u

CONFIG_FILE="${CONFIG_FILE:-config.yaml}"
TIMEOUT="${TIMEOUT:-12}"

if [ ! -f "$CONFIG_FILE" ]; then
  echo "[ERROR] 找不到配置文件: $CONFIG_FILE"
  exit 1
fi

cfg_value() {
  local section="$1"
  local key="$2"
  awk -v section="$section" -v key="$key" '
    $0 ~ "^" section ":" {in_section=1; next}
    in_section && $0 ~ "^[^ ]" {in_section=0}
    in_section && $1 == key":" {
      sub(/^[^:]+:[[:space:]]*/, "", $0)
      gsub(/^"|"$/, "", $0)
      print $0
      exit
    }
  ' "$CONFIG_FILE"
}

LISTENER_ADDR="$(cfg_value listener address)"
LISTENER_PORT="$(cfg_value listener port)"
LISTENER_USER="$(cfg_value listener username)"
LISTENER_PASS="$(cfg_value listener password)"
MGMT_LISTEN="$(cfg_value management listen)"
MODE="$(awk '/^mode:/{print $2; exit}' "$CONFIG_FILE")"
GEOIP_ENABLED="$(cfg_value geoip enabled)"
GEOIP_ADDR="$(cfg_value geoip listen)"
GEOIP_PORT="$(cfg_value geoip port)"
MULTI_PORT_BASE="$(cfg_value multi_port base_port)"

if [ -z "$LISTENER_ADDR" ]; then LISTENER_ADDR="127.0.0.1"; fi
if [ "$LISTENER_ADDR" = "0.0.0.0" ]; then LISTENER_ADDR="127.0.0.1"; fi
if [ -z "$GEOIP_ADDR" ]; then GEOIP_ADDR="$LISTENER_ADDR"; fi
if [ "$GEOIP_ADDR" = "0.0.0.0" ]; then GEOIP_ADDR="127.0.0.1"; fi
if [ -z "$GEOIP_PORT" ]; then GEOIP_PORT="$LISTENER_PORT"; fi

unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY

curl_proxy() {
  local proxy_url="$1"
  local target="$2"
  curl --max-time "$TIMEOUT" --silent --show-error -L \
    --proxy "$proxy_url" \
    --proxy-user "$LISTENER_USER:$LISTENER_PASS" \
    "$target"
}

proxy_http_code() {
  local proxy_url="$1"
  local target="$2"
  curl --max-time "$TIMEOUT" --silent -o /dev/null -w '%{http_code}' \
    --proxy "$proxy_url" \
    --proxy-user "$LISTENER_USER:$LISTENER_PASS" \
    "$target" || true
}

parse_country() {
  sed -n 's/.*"country"[[:space:]]*:[[:space:]]*"\([A-Z][A-Z]\)".*/\1/p' | head -n1
}

show_result() {
  local name="$1"
  local proxy_url="$2"
  local code ip info country
  code="$(proxy_http_code "$proxy_url" "http://cp.cloudflare.com/generate_204")"
  if [ "$code" = "204" ] || [ "$code" = "200" ]; then
    ip="$(curl_proxy "$proxy_url" "https://api.ipify.org" 2>/dev/null || true)"
    info="$(curl_proxy "$proxy_url" "https://ipinfo.io/json" 2>/dev/null || true)"
    country="$(printf '%s' "$info" | parse_country)"
    if [ -n "$country" ]; then
      echo "[OK] $name | HTTP=$code | IP=$ip | Country=$country"
    elif printf '%s' "$info" | grep -q '"status"[[:space:]]*:[[:space:]]*429'; then
      echo "[OK] $name | HTTP=$code | IP=$ip | Country=ipinfo 限流"
    else
      echo "[OK] $name | HTTP=$code | IP=$ip | Country=未知"
    fi
  else
    echo "[FAIL] $name | HTTP=${code:-000}"
  fi
}

echo "=== Easy Proxies 本地代理池自检 ==="
echo "配置文件: $CONFIG_FILE"
echo "模式: ${MODE:-unknown}"
echo "管理面板: ${MGMT_LISTEN:-unknown}"
echo

POOL_PROXY="http://${LISTENER_ADDR}:${LISTENER_PORT}"
show_result "默认代理池" "$POOL_PROXY"

echo
if [ "$GEOIP_ENABLED" = "true" ]; then
  for region in us jp hk sg; do
    show_result "地域代理/$region" "http://${GEOIP_ADDR}:${GEOIP_PORT}/${region}/"
  done
else
  echo "[SKIP] GeoIP 地域路由未启用，US/JP/HK/SG 入口当前不可用"
fi

echo
if [ "$MODE" = "multi-port" ] || [ "$MODE" = "hybrid" ]; then
  if [ -n "$MULTI_PORT_BASE" ]; then
    for port in "$MULTI_PORT_BASE" "$((MULTI_PORT_BASE + 1))" "$((MULTI_PORT_BASE + 2))"; do
      code="$(curl --max-time 4 --silent -o /dev/null -w '%{http_code}' \
        --proxy "http://127.0.0.1:${port}" \
        --proxy-user "$LISTENER_USER:$LISTENER_PASS" \
        http://cp.cloudflare.com/generate_204 || true)"
      if [ "$code" = "204" ] || [ "$code" = "200" ]; then
        echo "[OK] 多端口代理 :$port | HTTP=$code"
      else
        echo "[FAIL] 多端口代理 :$port | HTTP=${code:-000}"
      fi
    done
  else
    echo "[SKIP] 配置中没有 multi_port.base_port"
  fi
else
  echo "[SKIP] 当前模式不是 multi-port/hybrid，多端口入口未启用"
fi
