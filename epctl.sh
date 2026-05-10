#!/usr/bin/env bash
set -euo pipefail

CONFIG_FILE="${CONFIG_FILE:-config.yaml}"
BIN="${BIN:-./easy_proxies_local}"
LOG_FILE="${LOG_FILE:-/tmp/easy_proxies.run.log}"
PID_FILE="${PID_FILE:-/tmp/easy_proxies.pid}"
ADB_SERIAL="${ADB_SERIAL:-192.168.1.118:5555}"
TEST_URL="${TEST_URL:-http://cp.cloudflare.com/generate_204}"
TIMEOUT="${TIMEOUT:-10}"
RETRIES="${RETRIES:-3}"

usage() {
  cat <<'EOF'
Easy Proxies local control

Usage:
  ./epctl.sh <command> [args]

Service:
  service:start              Start local binary in background
  service:stop               Stop local binary started by this script
  service:restart            Restart local binary
  service:status             Show WebUI, ports, nodes, and regions

Logs:
  logs:tail [N]              Show last N log lines, default 120
  logs:follow                Follow runtime log

Proxy:
  proxy:test [region]        Test Android no-auth region port, default jp
  proxy:regions              Show region to port mapping

ADB:
  adb:set [region]           Set adb reverse and Android global proxy, default jp
  adb:status                 Show adb reverse and proxy settings
  adb:clear                  Clear Android global proxy settings

Short aliases:
  start, stop, restart, status, logs, logs-follow, test
  adb-set, adb-status, adb-clear

Regions:
  us jp hk sg tw kr in ae ch au other

Environment:
  CONFIG_FILE=config.yaml
  BIN=./easy_proxies_local
  LOG_FILE=/tmp/easy_proxies.run.log
  ADB_SERIAL=192.168.1.118:5555
  RETRIES=3
EOF
}

clean_proxy_env() {
  unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
}

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

region_port() {
  local region="${1:-jp}"
  case "$region" in
    us) echo 13001 ;;
    jp) echo 13002 ;;
    hk) echo 13003 ;;
    sg) echo 13004 ;;
    tw) echo 13005 ;;
    kr) echo 13006 ;;
    in) echo 13007 ;;
    ae) echo 13008 ;;
    ch) echo 13019 ;;
    au) echo 13010 ;;
    other) echo 13011 ;;
    *) echo "[ERROR] Unknown region: $region" >&2; exit 2 ;;
  esac
}

show_regions() {
  cat <<'EOF'
Region ports:
  us     13001
  jp     13002
  hk     13003
  sg     13004
  tw     13005
  kr     13006
  in     13007
  ae     13008
  au     13010
  other  13011
  ch     13019
EOF
}

is_running() {
  if [ -f "$PID_FILE" ]; then
    local pid
    pid="$(cat "$PID_FILE" 2>/dev/null || true)"
    [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null
    return
  fi
  return 1
}

find_service_pid() {
  pgrep -f "$BIN --config $CONFIG_FILE" 2>/dev/null | head -n 1 || true
}

start_service() {
  if is_running; then
    echo "[OK] already running, pid=$(cat "$PID_FILE")"
    return
  fi
  if [ ! -x "$BIN" ]; then
    echo "[ERROR] binary not executable: $BIN"
    echo "Build first: go build -tags \"with_utls with_quic with_grpc with_wireguard with_gvisor with_clash_api\" -o ./easy_proxies_local ./cmd/easy_proxies"
    exit 1
  fi
  clean_proxy_env
  nohup "$BIN" --config "$CONFIG_FILE" >"$LOG_FILE" 2>&1 &
  echo $! >"$PID_FILE"
  sleep 2
  if is_running; then
    echo "[OK] started, pid=$(cat "$PID_FILE")"
  else
    echo "[ERROR] failed to start, see $LOG_FILE"
    exit 1
  fi
}

stop_service() {
  if ! is_running; then
    echo "[OK] not running"
    rm -f "$PID_FILE"
    return
  fi
  local pid
  pid="$(cat "$PID_FILE")"
  kill "$pid" 2>/dev/null || true
  for _ in $(seq 1 15); do
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$PID_FILE"
      echo "[OK] stopped"
      return
    fi
    sleep 1
  done
  echo "[WARN] still running after graceful stop, pid=$pid"
}

status_service() {
  clean_proxy_env
  local web
  web="$(curl --max-time 3 -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9091 || true)"
  echo "WebUI: ${web:-000} http://localhost:9091"
  if is_running; then
    echo "Process: running pid=$(cat "$PID_FILE")"
  else
    local found_pid
    found_pid="$(find_service_pid)"
    if [ -n "$found_pid" ]; then
      echo "Process: running pid=$found_pid"
    elif [ "$web" = "200" ]; then
      echo "Process: running, detected by WebUI"
    else
      echo "Process: not found"
    fi
  fi
  echo
  ss -ltnp 2>/dev/null | grep -E ':9091|:2323|:1221|:1300[1-9]|:13010|:13011|:13019' || true
  echo
  if [ "$web" = "200" ]; then
    curl -s http://127.0.0.1:9091/api/nodes | jq '{total_nodes, visible_nodes:(.nodes|length), available:([.nodes[] | select(.available==true)] | length), region_stats}' || true
  fi
}

show_logs() {
  local lines="${1:-120}"
  tail -n "$lines" "$LOG_FILE"
}

test_region() {
  local region="${1:-jp}"
  local port
  port="$(region_port "$region")"
  clean_proxy_env
  local code
  for attempt in $(seq 1 "$RETRIES"); do
    code="$(curl --max-time "$TIMEOUT" -s -o /dev/null -w '%{http_code}' -x "http://127.0.0.1:${port}" "$TEST_URL" || true)"
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

adb_set() {
  local region="${1:-jp}"
  local port
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

case "${1:-}" in
  service:start|start) start_service ;;
  service:stop|stop) stop_service ;;
  service:restart|restart) stop_service; start_service ;;
  service:status|status) status_service ;;
  logs:tail|logs) show_logs "${2:-120}" ;;
  logs:follow|logs-follow) tail -f "$LOG_FILE" ;;
  proxy:test|test) test_region "${2:-jp}" ;;
  proxy:regions|regions) show_regions ;;
  adb:set|adb-set) adb_set "${2:-jp}" ;;
  adb:clear|adb-clear) adb_clear ;;
  adb:status|adb-status) adb_status ;;
  help|-h|--help|"") usage ;;
  *) echo "[ERROR] Unknown command: $1"; usage; exit 2 ;;
esac
