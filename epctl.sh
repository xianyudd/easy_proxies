#!/usr/bin/env bash
set -euo pipefail

# Easy Proxies local/isolated service controller.
# Default profile is production/local. Use EP_PROFILE=isolated or isolated:* aliases
# for the disposable instance. Stop/restart only touches the active profile.

CONFIG_FILE_WAS_SET="${CONFIG_FILE+x}"
BIN_WAS_SET="${BIN+x}"
LOG_FILE_WAS_SET="${LOG_FILE+x}"
PID_FILE_WAS_SET="${PID_FILE+x}"
WEBUI_URL_WAS_SET="${WEBUI_URL+x}"

EP_PROFILE="${EP_PROFILE:-prod}"
case "${1:-}" in
  isolated:*|service:isolated:*) EP_PROFILE="isolated" ;;
esac

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ "$EP_PROFILE" = "isolated" ]; then
  [ -n "$CONFIG_FILE_WAS_SET" ] || CONFIG_FILE="/tmp/easy_proxies_isolated.yaml"
  [ -n "$BIN_WAS_SET" ] || BIN="/tmp/easy_proxies_isolated"
  [ -n "$LOG_FILE_WAS_SET" ] || LOG_FILE="/tmp/easy_proxies_isolated.log"
  [ -n "$PID_FILE_WAS_SET" ] || PID_FILE="/tmp/easy_proxies_isolated.pid"
  [ -n "$WEBUI_URL_WAS_SET" ] || WEBUI_URL="http://127.0.0.1:19093"
else
  [ -n "$CONFIG_FILE_WAS_SET" ] || CONFIG_FILE="config.yaml"
  [ -n "$BIN_WAS_SET" ] || BIN="./easy_proxies_local"
  [ -n "$LOG_FILE_WAS_SET" ] || LOG_FILE="/tmp/easy_proxies.run.log"
  [ -n "$PID_FILE_WAS_SET" ] || PID_FILE="/tmp/easy_proxies.pid"
  [ -n "$WEBUI_URL_WAS_SET" ] || WEBUI_URL="http://127.0.0.1:9091"
fi

WEBUI_TOKEN="${WEBUI_TOKEN:-}"
WEBUI_PASSWORD="${WEBUI_PASSWORD:-}"

ADB_SERIAL="${ADB_SERIAL:-192.168.1.118:5555}"
TEST_URL="${TEST_URL:-http://cp.cloudflare.com/generate_204}"
TIMEOUT="${TIMEOUT:-10}"
RETRIES="${RETRIES:-3}"
START_TIMEOUT="${START_TIMEOUT:-20}"
STOP_TIMEOUT="${STOP_TIMEOUT:-30}"
KILL_TIMEOUT="${KILL_TIMEOUT:-8}"
BUILD_TAGS="${BUILD_TAGS:-with_utls with_quic with_grpc with_wireguard with_gvisor with_clash_api}"

usage() {
  cat <<'EOF'
Easy Proxies control

Usage:
  ./epctl.sh <command> [args]
  EP_PROFILE=isolated ./epctl.sh <command> [args]

Service:
  service:start | start                 Start active profile in background
  service:stop | stop                   Stop only active profile process
  service:restart | restart             Restart active profile
  service:status | status               Show process, WebUI, ports, nodes
  service:build                         Build local binary for active profile

Isolated profile aliases:
  isolated:config                       Write /tmp/easy_proxies_isolated.yaml
  isolated:build                        Build /tmp/easy_proxies_isolated
  isolated:start                        Start isolated instance on WebUI :19093
  isolated:stop                         Stop isolated instance only
  isolated:restart                      Restart isolated instance only
  isolated:status                       Show isolated status
  isolated:logs [N]                     Tail isolated log
  isolated:logs-follow                  Follow isolated log

Logs:
  logs:tail | logs [N]                  Show last N log lines, default 120
  logs:follow | logs-follow             Follow runtime log

Proxy:
  proxy:test | test [region]            Test region proxy port, default jp
  proxy:regions | regions               Show configured/default region ports

IP Reputation:
  reputation:check <region> [count]     Check IP reputation via WebUI API
  reputation:cache                      Show reputation cache summary

Cloudflare Score:
  cf:check <region> [count]             Check CF score for available nodes
  cf:check-all [region]                 Check all nodes, including unavailable
  cf:cache                              Show CF score cache summary

WebUI development:
  web:dev                               Start Vite dev server
  web:typecheck                         Run TypeScript typecheck
  web:build                             Build React WebUI assets

ADB:
  adb:set [region]                      Set adb reverse/global proxy
  adb:status                            Show adb reverse/proxy settings
  adb:clear                             Clear Android global proxy settings

Environment:
  EP_PROFILE=prod|isolated              Active profile, default prod
  CONFIG_FILE=config.yaml               Active config path
  BIN=./easy_proxies_local              Active binary path
  LOG_FILE=/tmp/easy_proxies.run.log    Active log path
  PID_FILE=/tmp/easy_proxies.pid        Active PID path
  WEBUI_URL=http://127.0.0.1:9091       Active WebUI base URL
  WEBUI_TOKEN=<session token, optional>
  WEBUI_PASSWORD=<WebUI password, optional; never printed>
  EP_KEEP_PROXY_ENV=1                   Preserve proxy env for service process
EOF
}

clean_proxy_env() {
  unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
}

strip_quotes() {
  sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//'
}

cfg_value() {
  local section="$1" key="$2" file="${3:-$CONFIG_FILE}"
  [ -f "$file" ] || return 0
  awk -v section="$section" -v key="$key" '
    $0 ~ "^" section ":" {in_section=1; next}
    in_section && $0 ~ "^[^[:space:]]" {in_section=0}
    in_section && $1 == key":" {
      sub(/^[^:]+:[[:space:]]*/, "", $0)
      gsub(/^"|"$/, "", $0)
      print $0
      exit
    }
  ' "$file" | strip_quotes
}

cfg_top_value() {
  local key="$1" file="${2:-$CONFIG_FILE}"
  [ -f "$file" ] || return 0
  awk -v key="$key" '$1 == key":" {sub(/^[^:]+:[[:space:]]*/, "", $0); print; exit}' "$file" | strip_quotes
}

webui_host_port() {
  local url="${WEBUI_URL#http://}"
  url="${url#https://}"
  url="${url%%/*}"
  echo "$url"
}

webui_port() {
  webui_host_port | awk -F: '{print $NF}'
}

configured_port() {
  local section="$1" key="$2" fallback="$3"
  local val
  val="$(cfg_value "$section" "$key" || true)"
  echo "${val:-$fallback}"
}

configured_region_port() {
  local region="$1"
  [ -f "$CONFIG_FILE" ] || return 1
  awk -v region="$region" '
    /^android_proxy:/ {in_android=1; next}
    in_android && /^[^[:space:]]/ {in_android=0}
    in_android && /^[[:space:]]+region_ports:/ {in_regions=1; next}
    in_regions && /^[[:space:]]{4}[^[:space:]]+:/ {
      k=$1; sub(/:/,"",k)
      if (k==region) {print $2; exit}
    }
    in_regions && /^[[:space:]]{2}[^[:space:]]+:/ {in_regions=0}
  ' "$CONFIG_FILE"
}

region_port() {
  local region="${1:-jp}" base custom offset
  case "$region" in
    all) echo all; return ;;
    us) offset=0 ;;
    jp) offset=1 ;;
    hk) offset=2 ;;
    sg) offset=3 ;;
    tw) offset=4 ;;
    kr) offset=5 ;;
    in) offset=6 ;;
    ae) offset=7 ;;
    au) offset=9 ;;
    other) offset=10 ;;
    de) offset=11 ;;
    ca) offset=13 ;;
    gb) offset=14 ;;
    ch) offset=18 ;;
    *) echo "[ERROR] Unknown region: $region" >&2; exit 2 ;;
  esac
  custom="$(configured_region_port "$region" || true)"
  if [ -n "$custom" ]; then
    echo "$custom"
    return
  fi
  base="$(configured_port android_proxy base_port 13001)"
  echo $((base + offset))
}

show_regions() {
  local r
  echo "Region ports (${CONFIG_FILE}):"
  for r in us jp hk sg tw kr in ae au other de ca gb ch; do
    printf '  %-6s %s\n' "$r" "$(region_port "$r")"
  done
}

json_pretty() {
  if command -v jq >/dev/null 2>&1; then jq .; else cat; fi
}

webui_api() {
  local method="$1" path="$2" body="${3:-}"
  local url="${WEBUI_URL%/}${path}"
  local curl_args=(--noproxy '*' --max-time "$TIMEOUT" -sS -X "$method" -H 'Accept: application/json')
  local cookie_jar=""
  clean_proxy_env
  if [ -n "$WEBUI_TOKEN" ]; then
    curl_args+=(-H "Authorization: Bearer ${WEBUI_TOKEN}")
  elif [ -n "$WEBUI_PASSWORD" ]; then
    cookie_jar="$(mktemp -t epctl-webui-cookie.XXXXXX)"
    local auth_code
    auth_code="$(printf '{"password":"%s"}' "$WEBUI_PASSWORD" | curl --noproxy '*' --max-time "$TIMEOUT" -sS -o /dev/null -w '%{http_code}' -c "$cookie_jar" -H 'Content-Type: application/json' -X POST --data-binary @- "${WEBUI_URL%/}/api/auth" || true)"
    if [ "$auth_code" != "200" ]; then
      echo "[ERROR] WebUI authentication failed HTTP=${auth_code:-000}" >&2
      rm -f "$cookie_jar"
      exit 1
    fi
    curl_args+=(-b "$cookie_jar")
  fi
  if [ -n "$body" ]; then
    curl_args+=(-H 'Content-Type: application/json' --data-binary "$body")
  fi
  local response_file status
  response_file="$(mktemp -t epctl-webui-response.XXXXXX)"
  status="$(curl "${curl_args[@]}" -o "$response_file" -w '%{http_code}' "$url" || true)"
  [ -z "$cookie_jar" ] || rm -f "$cookie_jar"
  if ! [[ "$status" =~ ^[0-9]+$ ]] || [ "$status" -lt 200 ] || [ "$status" -ge 300 ]; then
    echo "[ERROR] WebUI API ${method} ${path} failed HTTP=${status:-000} url=$url" >&2
    cat "$response_file" >&2 || true
    rm -f "$response_file"
    exit 1
  fi
  json_pretty <"$response_file"
  rm -f "$response_file"
}

normalize_path() {
  local path="$1"
  if command -v realpath >/dev/null 2>&1; then
    realpath -m "$path"
  else
    case "$path" in /*) echo "$path" ;; *) echo "$PWD/$path" ;; esac
  fi
}

pid_matches_profile() {
  local pid="$1"
  [ -n "$pid" ] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  local cmdline bin_abs cfg_abs
  cmdline="$(tr '\0' ' ' 2>/dev/null <"/proc/$pid/cmdline" || true)"
  [ -n "$cmdline" ] || return 1
  bin_abs="$(normalize_path "$BIN")"
  cfg_abs="$(normalize_path "$CONFIG_FILE")"
  case "$cmdline" in
    *"$bin_abs"*" --config $cfg_abs"*|*"$BIN"*" --config $CONFIG_FILE"*) return 0 ;;
    *) return 1 ;;
  esac
}

is_running() {
  if [ -f "$PID_FILE" ]; then
    local pid
    pid="$(cat "$PID_FILE" 2>/dev/null || true)"
    if pid_matches_profile "$pid"; then return 0; fi
  fi
  [ -n "$(find_service_pids | head -n 1)" ]
}

running_pids() {
  local pids=""
  if [ -f "$PID_FILE" ]; then
    local pid
    pid="$(cat "$PID_FILE" 2>/dev/null || true)"
    if pid_matches_profile "$pid"; then
      pids="$pid"
    fi
  fi
  {
    [ -n "$pids" ] && printf '%s\n' "$pids"
    find_service_pids
  } | sort -u | sed '/^$/d'
}

primary_pid() {
  running_pids | head -n 1
}

sync_pid_file() {
  local pid
  pid="$(primary_pid || true)"
  if [ -n "$pid" ]; then
    echo "$pid" >"$PID_FILE"
  else
    rm -f "$PID_FILE"
  fi
}

display_pid() {
  local pid
  pid="$(primary_pid || true)"
  if [ -n "$pid" ]; then
    echo "$pid"
  elif [ -f "$PID_FILE" ]; then
    cat "$PID_FILE"
  fi
}

pid_file_value() {
  if [ -f "$PID_FILE" ]; then
    cat "$PID_FILE" 2>/dev/null || true
  fi
}

webui_http_code() {
  curl --noproxy '*' --max-time 3 -s -o /dev/null -w '%{http_code}' "${WEBUI_URL%/}" || true
}

find_service_pids() {
  local bin_abs cfg_abs
  bin_abs="$(normalize_path "$BIN")"
  cfg_abs="$(normalize_path "$CONFIG_FILE")"
  for f in /proc/[0-9]*/cmdline; do
    [ -r "$f" ] || continue
    local pid cmdline
    pid="${f#/proc/}"; pid="${pid%/cmdline}"
    cmdline="$(tr '\0' ' ' 2>/dev/null <"$f" || true)"
    case "$cmdline" in
      *"$bin_abs"*" --config $cfg_abs"*|*"$BIN"*" --config $CONFIG_FILE"*) echo "$pid" ;;
    esac
  done
}

wait_for_webui() {
  local deadline=$((SECONDS + START_TIMEOUT)) code
  while [ "$SECONDS" -lt "$deadline" ]; do
    code="$(curl --noproxy '*' --max-time 2 -s -o /dev/null -w '%{http_code}' "${WEBUI_URL%/}" || true)"
    if [ "$code" = "200" ]; then
      echo "[OK] WebUI ready: $WEBUI_URL"
      return 0
    fi
    sleep 1
  done
  echo "[WARN] WebUI not ready after ${START_TIMEOUT}s: $WEBUI_URL"
  return 1
}

build_service() {
  local out="$BIN"
  if [ "$EP_PROFILE" = "prod" ] && [ "$out" = "./easy_proxies_local" ]; then
    out="$ROOT_DIR/easy_proxies_local"
  fi
  echo "[INFO] building $out"
  (cd "$ROOT_DIR" && GOCACHE="${GOCACHE:-/tmp/easy-proxies-go-build}" go build -tags "$BUILD_TAGS" -o "$out" ./cmd/easy_proxies)
  echo "[OK] built $out"
}

write_isolated_config() {
  local source_config="${SOURCE_CONFIG:-$ROOT_DIR/config.yaml}"
  local cache_dir="/tmp/.cache/easy-proxies-isolated"
  mkdir -p "$cache_dir"
  cat >"$CONFIG_FILE" <<EOF
mode: hybrid
listener:
  address: 127.0.0.1
  port: 12340
  username: user
  password: pass
multi_port:
  address: 127.0.0.1
  base_port: 30000
  username: user
  password: pass
android_proxy:
  enabled: true
  listen: 127.0.0.1
  base_port: 30150
pool:
  mode: balance
  failure_threshold: 3
  blacklist_duration: 24h0m0s
management:
  enabled: true
  listen: 127.0.0.1:19093
  clash_api_listen: 127.0.0.1:19094
  probe_target: http://cp.cloudflare.com/generate_204
  password: ""
subscription_refresh:
  enabled: true
  interval: 1h0m0s
  timeout: 30s
  health_check_timeout: 1m0s
  drain_timeout: 30s
  min_available_nodes: 1
quality_check:
  enabled: false
  interval: 1h0m0s
  region: all
  count: 500
  include_unavailable: false
  retry_failed: true
  cloudflare_timeout: 3s
  cloudflare_concurrency: 32
geoip:
  enabled: false
  database_path: ./GeoLite2-Country.mmdb
  listen: 127.0.0.1
  port: 12341
  auto_update_enabled: false
  auto_update_interval: 24h0m0s
log:
  output: stdout
  file: /tmp/easy_proxies_isolated-inner.log
  max_size: 200
  max_backups: 2
  max_age: 3
  compress: false
nodes: []
nodes_file: /tmp/easy_proxies_isolated_nodes.txt
subscriptions:
$(awk '
  /^subscriptions:/ {in_sub=1; next}
  in_sub && /^[^[:space:]]/ {in_sub=0}
  in_sub && /^[[:space:]]*-/ {print $0}
' "$source_config" 2>/dev/null || true)
free_proxy_cache:
  enabled: true
  path: $cache_dir/free-proxies.txt
  refresh_on_start: false
  auto_reload: true
  workers: 8
  max_age: 6h
free_proxy_max_nodes: 0
free_proxy_filter:
  enabled: true
  min_tier: http_basic
  workers: 200
  timeout: 2s
  max_candidates: 0
  probes:
    http: http://cp.cloudflare.com/generate_204
    https: https://example.com/
free_proxy_sources:
$(awk '
  /^free_proxy_sources:/ {in_src=1; next}
  in_src && /^[^[:space:]]/ {in_src=0}
  in_src {print $0}
' "$source_config" 2>/dev/null || true)
external_ip: ""
log_level: info
skip_cert_verify: false
upstream_proxy: ""
EOF
  if ! grep -q '^[[:space:]]*-' "$CONFIG_FILE"; then
    echo "[WARN] no subscriptions/free sources copied from $source_config"
  fi
  echo "[OK] wrote isolated config: $CONFIG_FILE"
}

start_service() {
  if is_running; then
    sync_pid_file
    echo "[OK] already running, profile=$EP_PROFILE pid=$(display_pid)"
    return
  fi
  if [ ! -f "$CONFIG_FILE" ] && [ "$EP_PROFILE" = "isolated" ]; then
    write_isolated_config
  fi
  if [ ! -x "$BIN" ]; then
    echo "[ERROR] binary not executable: $BIN"
    echo "Build first: ./epctl.sh service:build"
    exit 1
  fi
  if [ "${EP_KEEP_PROXY_ENV:-0}" != "1" ]; then
    clean_proxy_env
  fi
  mkdir -p "$(dirname "$LOG_FILE")" "$(dirname "$PID_FILE")"
  echo "[INFO] starting profile=$EP_PROFILE bin=$BIN config=$CONFIG_FILE log=$LOG_FILE"
  setsid "$BIN" --config "$CONFIG_FILE" >"$LOG_FILE" 2>&1 < /dev/null &
  echo $! >"$PID_FILE"
  sleep 1
  if is_running; then
    sync_pid_file
    echo "[OK] started, pid=$(display_pid)"
    wait_for_webui || true
  else
    echo "[ERROR] failed to start, see $LOG_FILE"
    tail -n 80 "$LOG_FILE" 2>/dev/null || true
    exit 1
  fi
}

alive_pids() {
  local pids="$*" alive="" pid
  for pid in $pids; do
    if kill -0 "$pid" 2>/dev/null; then
      alive="$alive $pid"
    fi
  done
  echo "$alive"
}

wait_until_stopped() {
  local wait_seconds="$1" pids="$2" deadline alive
  deadline=$((SECONDS + wait_seconds))
  while [ "$SECONDS" -lt "$deadline" ]; do
    alive="$(alive_pids $pids)"
    if [ -z "$alive" ]; then
      return 0
    fi
    sleep 1
  done
  alive="$(alive_pids $pids)"
  [ -z "$alive" ]
}

stop_service() {
  local pids alive
  pids="$(find_service_pids | sort -u || true)"
  if [ -z "$pids" ]; then
    local web pid_file
    web="$(webui_http_code)"
    pid_file="$(pid_file_value)"
    if [ "$web" = "200" ]; then
      echo "[ERROR] WebUI is still reachable but no matching process is visible from this environment, profile=$EP_PROFILE" >&2
      [ -z "$pid_file" ] || echo "[ERROR] PID file still points to pid=$pid_file; retry outside the sandbox or with elevated permissions" >&2
      return 1
    fi
    echo "[OK] not running, profile=$EP_PROFILE"
    rm -f "$PID_FILE"
    return
  fi
  echo "[INFO] stopping profile=$EP_PROFILE pids=$(echo "$pids" | tr '\n' ' ')"
  for pid in $pids; do kill "$pid" 2>/dev/null || true; done
  if wait_until_stopped "$STOP_TIMEOUT" "$pids"; then
    rm -f "$PID_FILE"
    echo "[OK] stopped"
    return
  fi

  alive="$(alive_pids $pids)"
  echo "[WARN] graceful stop timed out after ${STOP_TIMEOUT}s, killing:${alive}"
  for pid in $alive; do kill -KILL "$pid" 2>/dev/null || true; done
  if wait_until_stopped "$KILL_TIMEOUT" "$alive"; then
    rm -f "$PID_FILE"
    echo "[OK] stopped after kill"
    return
  fi

  echo "[ERROR] still running after kill:$(alive_pids $alive)" >&2
  exit 1
}

status_service() {
  clean_proxy_env
  local web node_json settings_json pids listen_re
  web="$(webui_http_code)"
  echo "Profile: $EP_PROFILE"
  echo "Config:  $CONFIG_FILE"
  echo "Binary:  $BIN"
  echo "PIDFile: $PID_FILE"
  echo "Log:     $LOG_FILE"
  echo "WebUI:   ${web:-000} ${WEBUI_URL}"
  pids="$(find_service_pids | sort -u || true)"
  if [ -n "$pids" ]; then
    echo "Process: running pid=$(echo "$pids" | tr '\n' ' ')"
  elif [ "$web" = "200" ]; then
    local pid_file
    pid_file="$(pid_file_value)"
    if [ -n "$pid_file" ]; then
      echo "Process: running via WebUI, pid file=$pid_file (process not visible from this environment)"
    else
      echo "Process: running, detected by WebUI only"
    fi
  else
    echo "Process: not found"
  fi
  echo
  local pool_port android_base multi_base clash_port geo_port web_port
  pool_port="$(configured_port listener port 2323)"
  android_base="$(configured_port android_proxy base_port 13001)"
  multi_base="$(configured_port multi_port base_port 24000)"
  web_port="$(webui_port)"
  clash_port="$(cfg_value management clash_api_listen || true)"; clash_port="${clash_port##*:}"
  geo_port="$(configured_port geoip port 1221)"
  listen_re=":(${web_port}|${pool_port}|${geo_port}|${multi_base}|${android_base}|${clash_port:-0})"
  echo "Listening ports (key ports):"
  ss -ltnp 2>/dev/null | grep -E "$listen_re" || true
  echo
  if [ "$web" = "200" ]; then
    node_json="$(curl --noproxy '*' --max-time "$TIMEOUT" -sS "${WEBUI_URL%/}/api/nodes" || true)"
    if command -v jq >/dev/null 2>&1 && [ -n "$node_json" ]; then
      echo "$node_json" | jq '{total_nodes, visible_nodes:(.nodes|length), available:([.nodes[] | select(.available==true)] | length), source_stats, region_stats, port_range: (([.nodes[].port? // empty] | sort) as $p | if ($p|length)>0 then {first:$p[0], last:$p[-1]} else null end)}' || true
    else
      echo "$node_json"
    fi
    settings_json="$(curl --noproxy '*' --max-time "$TIMEOUT" -sS "${WEBUI_URL%/}/api/settings" || true)"
    if command -v jq >/dev/null 2>&1 && [ -n "$settings_json" ]; then
      echo
      echo "Settings summary:"
      echo "$settings_json" | jq '{subscriptions:(.subscriptions|length? // 0), free_proxy_sources:(.free_proxy_sources|length? // 0), free_proxy_cache, free_proxy_max_nodes}' 2>/dev/null || true
    fi
  fi
}

show_logs() {
  local lines="${1:-120}"
  if [ ! -f "$LOG_FILE" ]; then
    echo "[WARN] log file not found: $LOG_FILE"
    return
  fi
  tail -n "$lines" "$LOG_FILE"
}

test_region() {
  local region="${1:-jp}" port code
  port="$(region_port "$region")"
  clean_proxy_env
  for attempt in $(seq 1 "$RETRIES"); do
    code="$(curl --noproxy '*' --max-time "$TIMEOUT" -s -o /dev/null -w '%{http_code}' -x "http://127.0.0.1:${port}" "$TEST_URL" || true)"
    if [ "$code" = "204" ] || [ "$code" = "200" ]; then
      echo "[OK] $region :$port HTTP=$code attempt=$attempt"
      return
    fi
    echo "[WARN] $region :$port HTTP=${code:-000} attempt=$attempt/$RETRIES"
    sleep 1
  done
  echo "[FAIL] $region :$port HTTP=${code:-000}"
  exit 1
}

reputation_check() {
  local region="${1:-}" count="${2:-10}"
  [ -n "$region" ] || { echo "[ERROR] Usage: ./epctl.sh reputation:check <region>" >&2; exit 2; }
  region_port "$region" >/dev/null
  webui_api GET "/api/reputation/check?region=${region}&mode=multi-port&count=${count}"
}

reputation_cache() { webui_api GET "/api/reputation/cache"; }

cf_check() {
  local region="${1:-}" count="${2:-10}"
  [ -n "$region" ] || { echo "[ERROR] Usage: ./epctl.sh cf:check <region> [count]" >&2; exit 2; }
  region_port "$region" >/dev/null
  webui_api GET "/api/cloudflare/check?region=${region}&mode=multi-port&count=${count}"
}

cf_check_all() {
  local region="${1:-all}"
  region_port "$region" >/dev/null
  webui_api GET "/api/cloudflare/check?region=${region}&mode=multi-port&count=500&include_unavailable=true"
}

cf_cache() { webui_api GET "/api/cloudflare/cache"; }

adb_set() {
  local region="${1:-jp}" port
  port="$(region_port "$region")"
  adb -s "$ADB_SERIAL" reverse "tcp:${port}" "tcp:${port}"
  adb -s "$ADB_SERIAL" shell settings put global http_proxy "127.0.0.1:${port}"
  adb -s "$ADB_SERIAL" shell settings put global global_http_proxy_host 127.0.0.1
  adb -s "$ADB_SERIAL" shell settings put global global_http_proxy_port "$port"
  echo "[OK] adb proxy set: $region 127.0.0.1:$port"
  adb_status
}

adb_clear() {
  adb -s "$ADB_SERIAL" shell settings put global http_proxy :0 || true
  adb -s "$ADB_SERIAL" shell settings delete global http_proxy || true
  adb -s "$ADB_SERIAL" shell settings delete global global_http_proxy_host || true
  adb -s "$ADB_SERIAL" shell settings delete global global_http_proxy_port || true
  echo "[OK] adb proxy cleared"
}

adb_status() {
  adb -s "$ADB_SERIAL" reverse --list || true
  adb -s "$ADB_SERIAL" shell settings list global | grep -E 'proxy|http_proxy' || true
}

web_dev() { (cd web && npm run dev); }
web_typecheck() { (cd web && npm_config_cache="${NPM_CONFIG_CACHE:-/tmp/easy_proxies-npm-cache}" npm run typecheck); }
web_build() { (cd web && npm_config_cache="${NPM_CONFIG_CACHE:-/tmp/easy_proxies-npm-cache}" npm run build); }

cmd="${1:-}"
case "$cmd" in
  service:start|start) start_service ;;
  service:stop|stop) stop_service ;;
  service:restart|restart) stop_service; start_service ;;
  service:status|status) status_service ;;
  service:build|build) build_service ;;
  isolated:config|service:isolated:config) EP_PROFILE=isolated; write_isolated_config ;;
  isolated:build|service:isolated:build) EP_PROFILE=isolated; build_service ;;
  isolated:start|service:isolated:start) start_service ;;
  isolated:stop|service:isolated:stop) stop_service ;;
  isolated:restart|service:isolated:restart) stop_service; start_service ;;
  isolated:status|service:isolated:status) status_service ;;
  isolated:logs) show_logs "${2:-120}" ;;
  isolated:logs-follow) tail -f "$LOG_FILE" ;;
  logs:tail|logs) show_logs "${2:-120}" ;;
  logs:follow|logs-follow) tail -f "$LOG_FILE" ;;
  proxy:test|test) test_region "${2:-jp}" ;;
  proxy:regions|regions) show_regions ;;
  reputation:check) reputation_check "${2:-}" "${3:-10}" ;;
  reputation:cache) reputation_cache ;;
  cf:check) cf_check "${2:-}" "${3:-10}" ;;
  cf:check-all) cf_check_all "${2:-all}" ;;
  cf:cache) cf_cache ;;
  web:dev) web_dev ;;
  web:typecheck) web_typecheck ;;
  web:build) web_build ;;
  adb:set|adb-set) adb_set "${2:-jp}" ;;
  adb:clear|adb-clear) adb_clear ;;
  adb:status|adb-status) adb_status ;;
  help|-h|--help|"") usage ;;
  *) echo "[ERROR] Unknown command: $cmd"; usage; exit 2 ;;
esac
