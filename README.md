# ccmux

Control and interact with terminal sessions on your computer from your phone.

No tmux required. Install the agent on any Mac, start sessions, then monitor and type into them from the iOS/Android app in real time.

---

## How it works

```
┌─────────────────────────────┐
│  Your computer              │
│                             │
│  ccmux-agent (Go)           │
│  ├── session 0: bash  [PTY] │
│  ├── session 1: vim   [PTY] │
│  └── session 2: make  [PTY] │
└────────────┬────────────────┘
             │ WebSocket
             ▼
┌─────────────────────────────┐
│  Backend (Go)               │
│  ├── auth / device registry │
│  ├── in-memory session hub  │
│  └── Redis scrollback       │
└────────────┬────────────────┘
             │ WebSocket
             ▼
┌─────────────────────────────┐
│  Mobile app (Flutter)       │
│  ├── workspace drawer       │
│  ├── session tabs           │
│  └── full interactive term  │
└─────────────────────────────┘
```

**Hot path is zero-I/O:** PTY output is batched in 16 ms windows, forwarded over WebSocket through the backend's in-memory hub, and rendered by the app. No database or Redis on the critical path.

---

## Components

| Component | Language | Location | Role |
|-----------|----------|----------|------|
| `ccmux-agent` | Go | `agent/cmd/agent` | Runs on your computer; owns all PTY sessions; relays I/O to backend |
| `ccmux` (CLI) | Go | `agent/cmd/ctl` | Local control tool; spawn / kill / list / attach sessions |
| Backend | Go | `backend/` | Auth, device registry, session metadata, WebSocket relay hub |
| Mobile app | Flutter | `mobile/` | iOS + Android client |

---

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| Go 1.21+ | For building agent and backend |
| Docker + Docker Compose | For Postgres and Redis |
| Flutter 3.x | For building the mobile app |
| Xcode / Android Studio | For running the mobile app on a device or simulator |

---

## Quick start (local development)

### 1. Start the backend stack

```bash
cd ccmux

# Start Postgres + Redis, build binaries, register a dev user and device,
# and write credentials to .env.agent
./scripts/setup-local.sh
```

This script:
- Builds `bin/ccmux-agent` and `bin/ccmux`
- Starts Postgres and Redis via Docker Compose
- Starts the backend (`go run ./backend/cmd/server`)
- Creates a dev user and device via the HTTP API
- Writes `CCMUX_DEVICE_ID`, `CCMUX_DEVICE_TOKEN`, `CCMUX_SERVER_URL` to `.env.agent`

### 2. Start the agent

In a new terminal:

```bash
./scripts/run-agent.sh
```

The agent connects to the backend over WebSocket and listens for IPC commands on `/tmp/ccmux.sock`.

### 3. Use the CLI

```bash
# Start a new session (name is optional; auto-assigned 0, 1, 2… if omitted)
./bin/ccmux new --name work

# Start a session running a specific command
./bin/ccmux new --name build make test

# List active sessions
./bin/ccmux list
# → work (a3f2…)
# → build (7c1d…)

# Attach interactively from the terminal
./bin/ccmux attach <UUID>
# Detach with Ctrl-\

# Kill a session by name or UUID
./bin/ccmux kill work

# Rename a session
./bin/ccmux rename <UUID> newname
```

### 4. Open the mobile app

```bash
cd mobile
flutter run
```

- Sign in with the dev account (`dev@ccmux.local` / `devpassword123` by default)
- Swipe right or tap ☰ to open the workspace drawer
- Tap any session to open it as an interactive terminal tab
- The bottom toolbar provides: Tab, Esc, Ctrl+C, Ctrl+D, Ctrl+Z, Ctrl+L, arrow keys, PgUp/PgDn

---

## Full stack startup (production / Docker)

```bash
# Start everything including the backend container
docker compose up -d

# Then start the agent on each computer you want to control
source .env.agent
./bin/ccmux-agent
```

Environment variables the backend reads:

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | — | Postgres connection string |
| `REDIS_URL` | — | Redis connection string |
| `JWT_SECRET` | — | Signs access tokens |
| `HMAC_SECRET` | — | Signs device tokens |
| `SERVER_ADDR` | `:8080` | HTTP/WebSocket listen address |
| `FCM_PROJECT_ID` | — | Firebase project (push notifications, optional) |
| `FCM_SERVICE_ACCOUNT_PATH` | — | Path to Firebase service account JSON |

Environment variables the agent reads:

| Variable | Default | Description |
|----------|---------|-------------|
| `CCMUX_SERVER_URL` | `ws://localhost:8080` | Backend WebSocket URL |
| `CCMUX_DEVICE_ID` | — | UUID assigned at device registration |
| `CCMUX_DEVICE_TOKEN` | — | HMAC-signed token for agent auth |
| `CCMUX_IPC_SOCKET` | `/tmp/ccmux.sock` | Unix socket path for the CLI |

---

## CLI reference

```
ccmux new [--name NAME] [--cols N] [--rows N] [--patterns P1,P2] [COMMAND...]
ccmux kill NAME|UUID
ccmux list
ccmux attach UUID
ccmux rename UUID NEWNAME
```

| Flag | Description |
|------|-------------|
| `--name` | Display name shown in the app and used with `kill`. Auto-assigned (0, 1, 2…) when omitted. |
| `--cols` | Terminal width. Auto-detected from the current terminal when omitted. |
| `--rows` | Terminal height. Auto-detected from the current terminal when omitted. |
| `--patterns` | Comma-separated alert patterns that trigger a push notification when matched in output. Defaults include `error`, `failed`, `panic`, `fatal`, `esc to cancel`, `do you want`, `would you like`, `are you sure`. |

`COMMAND` defaults to `bash` when omitted.

---

## Mobile app overview

### Workspace drawer (swipe right or tap ☰)
- Lists all registered devices and their active sessions
- Tap a session to open it as a tab
- Long-press or tap ⋮ to rename or kill a session from the app

### Session tabs
- Each open session gets a tab at the top
- Blue dot on inactive tabs indicates new output
- Red × button closes the local tab (session keeps running on the computer)

### Terminal view
- Full interactive PTY: everything you can type in a local terminal works here
- Scrollback replayed on open so you see existing output immediately

### Bottom toolbar
`Tab` · `Esc` · `Ctrl+C` · `Ctrl+D` · `Ctrl+Z` · `Ctrl+L` · `↑` · `↓` · `←` · `→` · `PgUp` · `PgDn`

---

## Repository structure

```
ccmux/
├── agent/
│   ├── cmd/
│   │   ├── agent/        # ccmux-agent binary
│   │   └── ctl/          # ccmux CLI binary
│   └── internal/
│       ├── config/       # env var loading
│       ├── ipc/          # Unix socket server/client (JSON protocol)
│       ├── pty/          # PTY session pool and manager
│       └── relay/        # WebSocket connection to backend
├── backend/
│   ├── cmd/server/       # Backend entry point
│   ├── internal/
│   │   ├── api/          # HTTP + WebSocket handlers
│   │   ├── auth/         # JWT and HMAC helpers
│   │   ├── hub/          # In-memory session broadcast hub
│   │   └── store/        # Postgres queries
│   ├── migrations/       # SQL schema migrations
│   └── pkg/protocol/     # Shared MessagePack wire protocol
├── mobile/
│   └── lib/
│       ├── core/
│       │   ├── api/      # REST client and models
│       │   ├── protocol/ # Packet decoding
│       │   └── websocket/# WS client + reconnect manager
│       └── features/
│           ├── auth/     # Login / register screens
│           ├── terminal/ # Session tabs, terminal view, toolbar
│           └── workspace/# Drawer, device sections
├── scripts/
│   ├── setup-local.sh    # Bootstrap dev stack end-to-end
│   ├── run-agent.sh      # Start agent with saved credentials
│   └── validate.sh       # Smoke-test all CLI commands
├── docker-compose.yml    # Postgres + Redis + backend
└── go.work               # Go workspace (agent + backend modules)
```

---

## Wire protocol

All real-time messages between agent and backend (and backend and mobile) are framed as MessagePack with a common envelope:

```
[ type (uint8) | session_id (string) | payload (bytes) ]
```

Key packet types:

| Type | Direction | Description |
|------|-----------|-------------|
| `terminal_output` | agent → mobile | PTY output chunk |
| `terminal_input` | mobile → agent | Keystrokes |
| `session_status` | agent ↔ mobile | Session lifecycle (active / exited / killed) |
| `resize` | mobile → agent | Terminal window resize |
| `scrollback` | backend → mobile | Buffered output replay on connect |
| `ping` / `pong` | both | Keepalive (45 s interval) |
