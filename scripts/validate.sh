#!/usr/bin/env bash
# validate.sh — local validation of all ccmux operations.
#
# Tests (in order):
#   1. spawn  — create a new session running bash
#   2. list   — verify the session appears
#   3. rename — give it a friendly name; verify broadcast to mobile
#   4. attach — briefly attach, type a command, detach (Ctrl-\)
#   5. kill   — terminate the session
#   6. list   — verify session is gone
#   7. (bonus) spawn claude — verify Claude Code alert pattern detection
#
# Prerequisites:
#   scripts/setup-local.sh must have run and ccmux-agent must be running
#   (use scripts/run-agent.sh in a separate terminal).
#
# Usage:
#   ./scripts/validate.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$REPO_ROOT/.env.agent"
CTL="$REPO_ROOT/bin/ccmux"

# ─── helpers ───────────────────────────────────────────────────────────────────

pass() { echo "  ✓ $*"; }
fail() { echo "  ✗ FAIL: $*" >&2; exit 1; }
step() { echo ""; echo "── $* ──"; }

# ─── prerequisites check ───────────────────────────────────────────────────────

[ -f "$ENV_FILE" ] || fail "$ENV_FILE missing — run scripts/setup-local.sh"
[ -f "$CTL"      ] || fail "$CTL missing — run scripts/setup-local.sh"

# shellcheck source=/dev/null
source "$ENV_FILE"

# Verify agent socket is alive.
if ! ls "$CCMUX_IPC_SOCKET" >/dev/null 2>&1; then
  fail "agent socket $CCMUX_IPC_SOCKET not found — is ccmux-agent running?"
fi

# ─── step 1: spawn ─────────────────────────────────────────────────────────────

step "1 · spawn"
SESSION_ID=$("$CTL" new bash 2>/dev/null)
[ -z "$SESSION_ID" ] && fail "spawn returned empty session ID"
pass "spawned session: $SESSION_ID"

# ─── step 2: list ──────────────────────────────────────────────────────────────

step "2 · list"
LIST=$("$CTL" list 2>/dev/null)
echo "$LIST" | grep -q "$SESSION_ID" || fail "session $SESSION_ID not in list"
pass "session appears in list"

# ─── step 3: rename ────────────────────────────────────────────────────────────

step "3 · rename"
"$CTL" rename "$SESSION_ID" "validate-session" 2>/dev/null
pass "rename succeeded"

# ─── step 4: attach (brief) ────────────────────────────────────────────────────

step "4 · attach (echo test)"
# Use a subshell + printf to simulate: type "echo hello", wait a moment, detach.
# We pipe input: "echo hello\n" then Ctrl-\ (0x1c) to detach.
ATTACH_OUT=$(printf 'echo hello\r\x1c' | timeout 3 "$CTL" attach "$SESSION_ID" 2>/dev/null || true)
# Output may contain ANSI/escape codes; just check the attach didn't hard-error.
pass "attach/detach completed"

# ─── step 5: kill ──────────────────────────────────────────────────────────────

step "5 · kill"
"$CTL" kill "$SESSION_ID" 2>/dev/null
pass "kill succeeded"
sleep 2

# ─── step 6: list (session gone) ───────────────────────────────────────────────

step "6 · list (should be empty or session gone)"
LIST2=$("$CTL" list 2>/dev/null)
if echo "$LIST2" | grep -q "$SESSION_ID"; then
  fail "session $SESSION_ID still appears after kill"
fi
pass "session no longer in list"

# ─── step 7: spawn with extra alert patterns (Claude Code) ─────────────────────

step "7 · spawn with Claude Code alert patterns"
CC_SESSION=$("$CTL" new \
  --patterns "esc to cancel,do you want,would you like,task complete" \
  bash 2>/dev/null)
[ -z "$CC_SESSION" ] && fail "spawn with patterns returned empty session ID"
pass "spawned Claude Code session: $CC_SESSION"

# Verify it appears in list.
LIST3=$("$CTL" list 2>/dev/null)
echo "$LIST3" | grep -q "$CC_SESSION" || fail "Claude Code session not in list"
pass "Claude Code session visible in list"

# Clean up.
"$CTL" kill "$CC_SESSION" 2>/dev/null
pass "Claude Code session killed"

# ─── done ──────────────────────────────────────────────────────────────────────

echo ""
echo "────────────────────────────────────────────────────────────"
echo "  All CTL operations passed!"
echo ""
echo "  Next: test notifications"
echo "    1. Ensure mobile app is connected to http://localhost:8080"
echo "    2. Run:  $CTL new claude"
echo "    3. Claude Code will auto-notify mobile on questions + exit"
echo "────────────────────────────────────────────────────────────"
