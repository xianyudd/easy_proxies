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
HEALTH_TIMEOUT_SEC="${HEALTH_TIMEOUT_SEC:-60}"
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

need() { command -v "$1" >/dev/null 2>&1 || die "missing command: $1"; }
need podman
need go
need systemctl
need curl

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

log "restart: systemctl --user restart $UNIT"
systemctl --user restart "$UNIT"

# Wait until unit active and HTTP responds.
deadline=$(( $(date +%s) + HEALTH_TIMEOUT_SEC ))
ok=0
while (( $(date +%s) < deadline )); do
  if systemctl --user is-active --quiet "$UNIT" \
    && curl -fsS --max-time 3 -o /dev/null "$HEALTH_URL" 2>/dev/null; then
    ok=1
    break
  fi
  sleep 2
done

if [[ "$ok" != "1" ]]; then
  log "health FAILED after ${HEALTH_TIMEOUT_SEC}s; rolling back to $PREV"
  if podman image exists "$PREV"; then
    podman tag "$PREV" "$IMAGE"
    systemctl --user restart "$UNIT" || true
  fi
  systemctl --user --no-pager --full status "$UNIT" | tail -40 || true
  podman ps -a --filter "name=^${CONTAINER}$" --format '{{.Names}} {{.Status}}' || true
  die "deploy rolled back (or rollback image missing)"
fi

log "health OK: $HEALTH_URL"
systemctl --user --no-pager --full status "$UNIT" | sed -n '1,15p' || true
podman ps --filter "name=^${CONTAINER}$" --format 'table {{.Names}}\t{{.Status}}\t{{.Image}}'
log "deploy done commit=$commit"
