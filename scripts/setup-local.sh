#!/usr/bin/env bash
# setup-local.sh — bootstrap the full ccmux stack for local development.
#
# What it does:
#   1. Starts the full stack (Postgres + Redis + backend) via docker compose.
#   2. Registers a dev user + device via regist-user-device.sh.
#   3. Builds and starts the local agent via run-agent.sh.
#
# Usage:
#   cd /path/to/ccmux
#   ./scripts/setup-local.sh
#
# To register against a remote backend instead:
#   CCMUX_API_URL=http://100.116.137.95:8080 ./scripts/regist-user-device.sh
#   ./scripts/run-agent.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

log() { echo "[setup] $*"; }

# ─── step 1: start docker compose ─────────────────────────────────────────────

log "starting stack (docker compose up -d) …"
docker compose up -d
log "stack is up"

# ─── step 2: register user + device ───────────────────────────────────────────

"$REPO_ROOT/scripts/regist-user-device.sh"

# ─── step 3: build and run agent ──────────────────────────────────────────────

"$REPO_ROOT/scripts/run-agent.sh"
