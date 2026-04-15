#!/usr/bin/env bash
# run-agent.sh — start the ccmux desktop agent using credentials from .env.agent.
#
# Prerequisites:
#   Run scripts/setup-local.sh first.
#
# Usage:
#   ./scripts/run-agent.sh          # foreground (Ctrl-C to stop)
#   ./scripts/run-agent.sh &        # background

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$REPO_ROOT/.env.agent"
AGENT_BIN="$REPO_ROOT/bin/ccmux-agent"

[ -f "$ENV_FILE"   ] || { echo "error: $ENV_FILE not found — run scripts/setup-local.sh first" >&2; exit 1; }
[ -f "$AGENT_BIN"  ] || { echo "error: $AGENT_BIN not found — run scripts/setup-local.sh first" >&2; exit 1; }

# shellcheck source=/dev/null
source "$ENV_FILE"

echo "[agent] starting — device=$CCMUX_DEVICE_ID  server=$CCMUX_SERVER_URL"
exec "$AGENT_BIN"
