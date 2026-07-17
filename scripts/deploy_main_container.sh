#!/usr/bin/env bash
# Deploy current git checkout into the local Podman Quadlet main container.
#
# Flow: test → build binary → thin-layer image → tag main → restart unit → healthcheck
# Rollback: on health failure, retag main-prev and restart again.
#
# Production (this machine):
#   unit:  easy_proxies_main.service  (from ~/.config/containers/systemd/easy_proxies_main.container)
#   image: localhost/easy-proxies:main
#   data:  /home/jason/.local/share/easy_proxies-main → /app
#
# Usage:
#   scripts/deploy_main_container.sh              # test + deploy
#   scripts/deploy_main_container.sh --skip-test  # deploy only
#   scripts/deploy_main_container.sh --full       # full Dockerfile rebuild (slow)
#   SKIP_RESTART=1 scripts/deploy_main_container.sh  # build/tag only
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

IMAGE="${IMAGE:-localhost/easy-proxies:main}"
CANDIDATE="${CANDIDATE:-localhost/easy-proxies:main-candidate}"
PREV="${PREV:-localhost/easy-proxies:main-prev}"
UNIT="${UNIT:-easy_proxies_main.service}"
CONTAINER="${CONTAINER:-easy_proxies_main}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:9091/}"
HEALTH_TIMEOUT_SEC="${HEALTH_TIMEOUT_SEC:-240}"
PROMOTE_VALIDATE_STRICT="${PROMOTE_VALIDATE_STRICT:-0}"
BUILD_TAGS="${BUILD_TAGS:-with_utls with_quic with_grpc with_wireguard with_gvisor with_clash_api}"

SKIP_TEST=0
FULL=0
SKIP_RESTART="${SKIP_RESTART:-0}"
for arg in "$@"; do
  case "$arg" in
    --skip-test) SKIP_TEST=1 ;;
    --full) FULL=1 ;;
    --skip-restart) SKIP_RESTART=1 ;;
    -h|--help)
      sed -n '2,20p' "$0"
      exit 0
      ;;
    *)
      echo "unknown arg: $arg" >&2
      exit 2
      ;;
  esac
done

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }
die() { echo "ERROR: $*" >&2; exit 1; }

validate_promote() {
  local base nodes_json cfg_file summary rc
  base="${HEALTH_URL%/}"
  nodes_json="$WORK/nodes.json"
  cfg_file="$WORK/promote-config.yaml"

  if ! curl -fsS --max-time 5 "$base/api/nodes?availability=all&page_size=5000" -o "$nodes_json"; then
    log "WARN: promote validation skipped: nodes API unavailable"
    return 0
  fi
  podman exec "$CONTAINER" sh -c 'cat /app/config.yaml 2>/dev/null || true' >"$cfg_file" || true

  set +e
  summary=$(python3 - "$cfg_file" "$nodes_json" <<'PY'
import json, re, sys

cfg_path, nodes_path = sys.argv[1], sys.argv[2]
text = open(cfg_path, encoding='utf-8', errors='ignore').read()
block = {}
in_block = False
for raw in text.splitlines():
    line = raw.split('#', 1)[0].rstrip()
    if not line.strip():
        continue
    if re.match(r'^free_proxy_promote\s*:', line):
        in_block = True
        continue
    if in_block and re.match(r'^\S', line):
        break
    if in_block:
        m = re.match(r'^\s+([A-Za-z0-9_]+)\s*:\s*(.*?)\s*$', line)
        if m:
            block[m.group(1)] = m.group(2).strip().strip('"\'')

def as_bool(v):
    return str(v).strip().lower() in ('1', 'true', 'yes', 'on')
def as_int(v, default):
    try:
        return int(str(v).strip())
    except Exception:
        return default

enabled = as_bool(block.get('enabled', 'false'))
max_latency = as_int(block.get('max_latency_ms', 800), 800)
min_success = as_int(block.get('min_success_count', 1), 1)
max_failure = as_int(block.get('max_failure_count', 1), 1)
prefix = block.get('name_prefix') or 'free-promoted-'

nodes = json.load(open(nodes_path, encoding='utf-8'))
if isinstance(nodes, dict):
    nodes = nodes.get('nodes') or nodes.get('data') or nodes.get('items') or []

eligible = []
promoted = []
for n in nodes:
    name = str(n.get('name') or '')
    source = str(n.get('source') or '')
    if name.startswith(prefix):
        promoted.append(n)
    if source != 'free_proxy':
        continue
    if n.get('blacklisted') or not n.get('available') or not n.get('initial_check_done'):
        continue
    if int(n.get('success_count') or 0) < min_success:
        continue
    if max_failure >= 0 and int(n.get('failure_count') or 0) > max_failure:
        continue
    latency = int(n.get('last_latency_ms') or 0)
    if max_latency > 0 and latency > 0 and latency > max_latency:
        continue
    eligible.append(n)

promoted_available = [n for n in promoted if n.get('available') and not n.get('blacklisted')]
promoted_blacklisted = [n for n in promoted if n.get('blacklisted')]
promoted_with_port = [n for n in promoted if int(n.get('port') or 0) > 0]
print(
    f"promote validation: enabled={str(enabled).lower()} eligible_free={len(eligible)} "
    f"promoted_total={len(promoted)} promoted_available={len(promoted_available)} "
    f"promoted_blacklisted={len(promoted_blacklisted)} promoted_with_port={len(promoted_with_port)} prefix={prefix}"
)
for n in eligible[:5]:
    print(f"eligible: {n.get('name')} latency={n.get('last_latency_ms')} success={n.get('success_count')} failures={n.get('failure_count')} uri={n.get('uri')}")
if not enabled and eligible:
    sys.exit(10)
if enabled and eligible and not promoted:
    sys.exit(11)
if enabled and promoted and not promoted_available:
    sys.exit(12)
PY
)
  rc=$?
  set -e

  while IFS= read -r line; do
    [[ -n "$line" ]] && log "$line"
  done <<<"$summary"
  if [[ "$rc" == "10" ]]; then
    [[ "$PROMOTE_VALIDATE_STRICT" == "1" ]] && die "promote disabled but eligible free_proxy nodes exist"
    log "WARN: promote disabled but eligible free_proxy nodes exist"
  elif [[ "$rc" == "11" ]]; then
    [[ "$PROMOTE_VALIDATE_STRICT" == "1" ]] && die "promote enabled but no promoted nodes found"
    log "WARN: promote enabled but no promoted nodes found"
  elif [[ "$rc" == "12" ]]; then
    [[ "$PROMOTE_VALIDATE_STRICT" == "1" ]] && die "promote nodes exist but none are available"
    log "WARN: promote nodes exist but none are available"
  elif [[ "$rc" != "0" ]]; then
    log "WARN: promote validation inconclusive (exit=$rc)"
  fi
}

need() { command -v "$1" >/dev/null 2>&1 || die "missing command: $1"; }
need podman
need go
need curl
need python3

unit_exists() {
  command -v systemctl >/dev/null 2>&1 && systemctl --user cat "$UNIT" >/dev/null 2>&1
}

restart_runtime() {
  if unit_exists; then
    log "restart: systemctl --user restart $UNIT"
    systemctl --user restart "$UNIT"
    return
  fi
  if podman container exists "$CONTAINER"; then
    log "restart: podman restart $CONTAINER (unit $UNIT not found)"
    podman restart "$CONTAINER" >/dev/null
    return
  fi
  die "neither unit $UNIT nor container $CONTAINER exists"
}

runtime_active() {
  if unit_exists; then
    systemctl --user is-active --quiet "$UNIT"
    return
  fi
  [[ "$(podman inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null || true)" == "true" ]]
}

runtime_status() {
  if unit_exists; then
    systemctl --user --no-pager --full status "$UNIT" | sed -n '1,15p' || true
  fi
  podman ps -a --filter "name=^${CONTAINER}$" --format 'table {{.Names}}\t{{.Status}}\t{{.Image}}' || true
}

branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
log "deploy start branch=$branch commit=$commit image=$IMAGE unit=$UNIT"

if [[ "$SKIP_TEST" != "1" ]]; then
  log "test: freepromote/config/builder/boxmgr"
  go test ./internal/freepromote/ ./internal/config/ ./internal/builder/ ./internal/boxmgr/ -count=1
else
  log "test: skipped"
fi

WORK="$(mktemp -d /tmp/easy-proxies-deploy-XXXXXX)"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

if [[ "$FULL" == "1" ]]; then
  log "build: full Dockerfile → $CANDIDATE"
  podman build \
    --build-arg "GOPROXY=${GOPROXY:-https://goproxy.cn,direct}" \
    -t "$CANDIDATE" \
    -f "$ROOT/Dockerfile" \
    "$ROOT"
else
  log "build: host binary + thin layer onto $IMAGE"
  if ! podman image exists "$IMAGE"; then
    die "base image $IMAGE missing; run once with --full or pull/build a runtime image first"
  fi
  CGO_ENABLED=0 go build -tags "$BUILD_TAGS" -o "$WORK/easy_proxies" ./cmd/easy_proxies
  cat >"$WORK/Containerfile" <<EOF
FROM $IMAGE
COPY easy_proxies /usr/local/bin/easy_proxies
EOF
  podman build -t "$CANDIDATE" -f "$WORK/Containerfile" "$WORK"
fi

# Keep previous main for rollback.
if podman image exists "$IMAGE"; then
  log "tag: $IMAGE → $PREV"
  podman tag "$IMAGE" "$PREV"
fi
log "tag: $CANDIDATE → $IMAGE"
podman tag "$CANDIDATE" "$IMAGE"

if [[ "$SKIP_RESTART" == "1" ]]; then
  log "restart skipped (SKIP_RESTART=1 / --skip-restart)"
  podman images --format 'table {{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.CreatedSince}}' | head -10
  exit 0
fi

restart_runtime

# Wait until runtime active and HTTP responds.
deadline=$(( $(date +%s) + HEALTH_TIMEOUT_SEC ))
ok=0
while (( $(date +%s) < deadline )); do
  if runtime_active && curl -fsS --max-time 3 -o /dev/null "$HEALTH_URL" 2>/dev/null; then
    ok=1
    break
  fi
  sleep 2
done

if [[ "$ok" != "1" ]]; then
  log "health FAILED after ${HEALTH_TIMEOUT_SEC}s; rolling back to $PREV"
  if podman image exists "$PREV"; then
    podman tag "$PREV" "$IMAGE"
    restart_runtime || true
  fi
  runtime_status
  die "deploy rolled back (or rollback image missing)"
fi

log "health OK: $HEALTH_URL"
validate_promote
runtime_status
log "deploy done commit=$commit"
