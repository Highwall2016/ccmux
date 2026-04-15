#!/usr/bin/env bash
# run-agent.sh — build the agent + ctl binaries and start ccmux-agent.
#
# Prerequisites:
#   Run scripts/regist-user-device.sh (or setup-local.sh) first to create .env.agent.
#
# Usage:
#   ./scripts/run-agent.sh          # foreground (Ctrl-C to stop)
#   ./scripts/run-agent.sh &        # background

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

ENV_FILE="$REPO_ROOT/.env.agent"
AGENT_BIN="$REPO_ROOT/bin/ccmux-agent"
CTL_BIN="$REPO_ROOT/bin/ccmux"

[ -f "$ENV_FILE" ] || {
  echo "error: $ENV_FILE not found — run scripts/regist-user-device.sh first" >&2
  exit 1
}

# ─── build ─────────────────────────────────────────────────────────────────────

echo "[agent] building …"
mkdir -p "$REPO_ROOT/bin"
go build -o "$AGENT_BIN" ./agent/cmd/agent
go build -o "$CTL_BIN"   ./agent/cmd/ctl
echo "[agent] built: $AGENT_BIN  $CTL_BIN"

# ─── run ───────────────────────────────────────────────────────────────────────

# shellcheck source=/dev/null
source "$ENV_FILE"

echo "[agent] starting — device=$CCMUX_DEVICE_ID  server=$CCMUX_SERVER_URL"
exec "$AGENT_BIN"
