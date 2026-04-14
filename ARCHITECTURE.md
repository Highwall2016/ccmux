# Ccmux — 完整架構設計

> 在手機上管理、串流並全互動操作所有電腦上的 terminal session。  
> 不依賴 tmux，安裝 ccmux agent 即可使用。絲滑是第一優先。

---

## 已確認的設計決策

| 問題 | 決策 |
|------|------|
| 互動性 | **全互動**（雙向輸入輸出） |
| tmux 依賴 | **不依賴 tmux**，ccmux agent 自行管理 PTY session pool |
| 行動平台 | **iOS + Android 同時支援**（Flutter 單一 codebase） |
| 部署策略 | **自架優先**，架構設計支援未來無縫遷移雲端 |
| 瀏覽器支援 | **不需要** |
| Push Notification | **必須**（APNs + FCM） |
| 第一優先 | **絲滑、低延遲**，效能設計貫穿所有決策 |

---

## 為什麼 Stateless 不影響絲滑？

這是一個重要的架構釐清。「Stateless」在這裡只適用於 **HTTP REST API 層**，不是 WebSocket 層。

```
┌─────────────────────────────────────────────────────────┐
│  HTTP REST API    → Stateless（標準做法）                │
│  WebSocket Hub    → In-memory Stateful（訊息路由核心）   │
│  Redis            → 非同步持久化，完全不在 hot path 上   │
│  PostgreSQL       → 用戶/裝置/session 元資料             │
└─────────────────────────────────────────────────────────┘
```

### 訊息傳遞的 Hot Path（零 Redis、零 DB）

```
Agent PTY 輸出
  → 批次 16ms
  → WebSocket frame
  → Backend in-memory Hub
  → 廣播給訂閱的 Client（純 memory，microsecond 級延遲）
  
同時（非同步，不阻塞 hot path）：
  → Redis Streams XADD（持久化 scrollback）
```

**Redis 只做兩件事，都在 hot path 之外：**
1. **Scrollback 持久化**：async 寫入，新客戶端連入時可還原畫面
2. **多 replica 廣播**：只有雲端水平擴展時才啟用（pub/sub）

**自架單一 instance 時**：Redis pub/sub 完全不啟用，純 in-memory hub，延遲等同於本地記憶體存取。絲滑的關鍵不是 stateless/stateful，而是：
- Hot path 零 I/O
- Agent 16ms batching（~60fps 輸出節奏）
- Flutter Impeller GPU 渲染
- 正確的 VT100 parser 不 stall

---

## 系統架構

```
┌──────────────────────────────────────────────────────────────┐
│                       使用者的電腦                            │
│                                                              │
│  ┌─────────────────────────────────────┐                    │
│  │     ccmux daemon (Go)               │                    │
│  │                                     │                    │
│  │  PTY Pool:                          │                    │
│  │  ├── session-a: bash    [PTY fd]    │                    │
│  │  ├── session-b: vim     [PTY fd]    │                    │
│  │  └── session-c: make    [PTY fd]    │                    │
│  └───────────────┬─────────────────────┘                    │
│                  │  WSS port 443                             │
└──────────────────┼───────────────────────────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────────────────────────┐
│                  Relay Backend (Go)                          │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │              In-Memory Hub（訊息路由核心）              │  │
│  │                                                        │  │
│  │  agentConns map[deviceID → AgentConn]                  │  │
│  │  clientConns map[sessionID → []ClientConn]             │  │
│  │                                                        │  │
│  │  熱路徑：Agent output → broadcast to clients           │  │
│  │  完全在記憶體，零外部 I/O                               │  │
│  └──────────┬──────────────────────────┬──────────────────┘  │
│             │（async，不阻塞）          │（async，不阻塞）    │
│        ┌────▼─────┐             ┌──────▼──────────────┐      │
│        │  Redis   │             │  Redis Streams       │      │
│        │(pub/sub, │             │  (scrollback buffer  │      │
│        │ 僅多節點)│             │   持久化)            │      │
│        └──────────┘             └─────────────────────┘      │
│                                                              │
│        ┌──────────────────┐   ┌─────────────────────────┐   │
│        │    PostgreSQL     │   │    Push Notification    │   │
│        │(用戶/裝置/sessions│   │  APNs (iOS) + FCM (Android)│ │
│        └──────────────────┘   └─────────────────────────┘   │
└──────────────────────────────────┬───────────────────────────┘
                                   │  WSS port 443
                          ┌────────┴──────────┐
                          ▼                   ▼
                     [iOS App]          [Android App]
                  (Flutter/Dart)      (Flutter/Dart)
```

---

## Mobile App UI 設計

UI 參考 cmux 的佈局精神，但大幅精簡：只有三個區域，無瀏覽器。

### Portrait 模式（主要使用情境）

```
┌──────────────────────────────┐
│ ≡   MacBook › bash      🔔 1 │  ← Header
│                              │    ≡ 開啟 Workspace Drawer
│                              │    🔔 通知 badge
├──────────────────────────────┤
│ bash  │ vim  │ make  │  +   │  ← Tab Panel（橫向可捲動）
├──────────────────────────────┤
│                              │
│                              │
│      Terminal Output         │
│      (flutter_xterm)         │
│                              │
│                              │
│                              │
├──────────────────────────────┤
│ Tab  Esc  Ctrl  ↑  ↓  ←  → │  ← 特殊鍵工具列
└──────────────────────────────┘
```

### Landscape 模式（鍵盤接入時推薦）

```
┌──────┬───────────────────────────┐
│      │ bash  │ vim  │ make  │ + │
│  W   ├───────────────────────────┤
│  o   │                           │
│  r   │    Terminal Output        │
│  k   │    (flutter_xterm)        │
│  s   │                           │
│  p   ├───────────────────────────┤
│  c   │ Tab  Esc  Ctrl  ↑ ↓ ← → │
└──────┴───────────────────────────┘
```

### Workspace Drawer（左滑開啟）

```
┌─────────────────────────────────┐
│  Ccmux          [+ New Session] │
├─────────────────────────────────┤
│  💻 MacBook Pro          online │
│  ├ ● bash                       │  ← 綠點 = active
│  ├ ● vim                        │
│  └ ○ make    ✓ exit 0  3m ago  │  ← 灰點 = exited，有 exit code
│                                 │
│  🖥 Ubuntu Server        online │
│  └ ● tail -f deploy.log         │
│                                 │
│  🖥 Dev VM               offline│  ← 裝置離線
│    (no active sessions)         │
└─────────────────────────────────┘
```

### Tab Panel 設計細節

- 每個 tab 顯示：session 名稱（或指令的 basename）
- **藍色光暈**：session 有新輸出且目前不在前景（參考 cmux 的 notification ring）
- **紅色 ×**：session 已 exit（點擊可關閉或重啟）
- **+ 按鈕**：建立新 session（彈出選擇：New Shell / Custom Command）
- 長按 tab：重新命名、kill session

### Push Notification 觸發情境

| 事件 | 通知內容 | 範例 |
|------|----------|------|
| Session exit code 0 | ✅ 完成 | "make: Build succeeded" |
| Session exit code ≠ 0 | ❌ 失敗 | "make: Exited with code 2" |
| 輸出匹配關鍵字（可設定） | 📢 自訂提醒 | "deploy.log: ERROR detected" |
| 裝置重新上線 | 📡 裝置上線 | "Ubuntu Server connected" |

---

## Push Notification 架構

### 裝置 Token 注冊流程

```
App 啟動
  → 向系統請求通知權限（iOS: requestAuthorization, Android: POST_NOTIFICATIONS）
  → 取得 device push token（APNs token 或 FCM registration token）
  → POST /api/push/register { platform: "ios"|"android", token: "..." }
  → 後端儲存 push_tokens 表
```

### 後端送出通知流程

```
Agent 回報 session exit（TypeSessionStatus）
  → Backend 更新 session.status + exit_code
  → 查詢 session → device → user → push_tokens
  → 判斷用戶是否有訂閱此 session（app 在前景時不送）
  → 呼叫 APNs HTTP/2 API 或 FCM v1 HTTP API
  → 送出通知
```

### 資料庫新增：push_tokens 表

```sql
CREATE TABLE push_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID REFERENCES users(id) ON DELETE CASCADE,
    platform   TEXT NOT NULL,   -- "ios" | "android"
    token      TEXT NOT NULL,
    device_name TEXT,           -- "iPhone 16 Pro"
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(user_id, token)
);
```

### 通知關鍵字匹配（可選功能）

Agent 可在輸出中做輕量 pattern match，偵測到特定字串後主動傳送 `TypeAlert` 訊號：

```go
// agent/internal/pty/session.go
var alertPatterns = []string{"error", "failed", "panic", "success", "done"}

func (s *Session) checkAlert(output []byte) {
    lower := strings.ToLower(string(output))
    for _, p := range alertPatterns {
        if strings.Contains(lower, p) {
            s.onAlert(p, output)
            return
        }
    }
}
```

---

## 雲端遷移路徑

### 自架單一 Instance（Phase 1）

```
                      單台 VPS（如 Hetzner CX21, ~5 EUR/月）
┌────────────────────────────────────────┐
│  Docker Compose                        │
│  ├── backend (Go)    :8080             │
│  │     └── in-memory hub only          │
│  │         (Redis pub/sub disabled)    │
│  ├── postgres        :5432             │
│  └── redis           :6379             │
│       (scrollback only, no pub/sub)    │
│                                        │
│  Caddy / Nginx  (TLS termination) :443 │
└────────────────────────────────────────┘

延遲：Agent → Backend → Client
純記憶體路徑，約 < 1ms（不含網路）
```

### 雲端水平擴展（用戶增長後，無需重寫代碼）

只需設定環境變數 `MULTI_INSTANCE_MODE=true`，啟用 Redis pub/sub routing：

```
                       雲端（用戶增長後）
┌───────────────────────────────────────────────────────┐
│                                                       │
│   Load Balancer（支援 WebSocket sticky session）      │
│   ├── backend replica 1  (in-memory hub A)            │
│   ├── backend replica 2  (in-memory hub B)            │
│   └── backend replica 3  (in-memory hub C)            │
│                  │                                    │
│      Redis pub/sub 橋接 hub A/B/C                    │
│      （Agent 在 hub A，Client 在 hub B               │
│       → hub A pub → Redis → hub B sub → Client）     │
│                  │                                    │
│        ┌─────────┴────────┐                          │
│        │   PostgreSQL      │   Redis Cluster          │
│        │  (RDS/Supabase)   │  (ElastiCache/Upstash)  │
│        └──────────────────┘                          │
└───────────────────────────────────────────────────────┘

延遲：Agent → hub A → Redis pub/sub → hub B → Client
增加約 1-2ms（Redis round trip），對 terminal 使用不可感知
```

**讓遷移零代碼改動的環境變數設計：**

```bash
# 自架模式（default）
MULTI_INSTANCE_MODE=false  # Redis pub/sub disabled，純 in-memory

# 雲端模式
MULTI_INSTANCE_MODE=true   # 啟用 Redis pub/sub routing
REDIS_URL=redis://...
DATABASE_URL=postgres://...
APNS_KEY_PATH=/secrets/apns.p8
FCM_SERVICE_ACCOUNT_PATH=/secrets/fcm.json
```

---

## 資料庫 Schema

```sql
-- 用戶帳號
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

-- 裝置（一個 user 可以有多台電腦）
CREATE TABLE devices (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    device_token TEXT NOT NULL,     -- HMAC-SHA256，儲存 hash
    platform     TEXT NOT NULL,     -- "macos" | "linux" | "windows"
    last_seen    TIMESTAMPTZ,
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

-- Terminal Session 元資料
CREATE TABLE terminal_sessions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id     UUID REFERENCES devices(id) ON DELETE CASCADE,
    name          TEXT,             -- 用戶自訂名稱
    command       TEXT NOT NULL,    -- 如 "/bin/bash", "vim ."
    status        TEXT DEFAULT 'active', -- active | exited | killed
    exit_code     INTEGER,
    cols          INTEGER DEFAULT 220,
    rows          INTEGER DEFAULT 50,
    started_at    TIMESTAMPTZ DEFAULT NOW(),
    ended_at      TIMESTAMPTZ,
    last_activity TIMESTAMPTZ DEFAULT NOW()
);

-- Push notification tokens（一個 user 可以有多支手機）
CREATE TABLE push_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID REFERENCES users(id) ON DELETE CASCADE,
    platform    TEXT NOT NULL,      -- "ios" | "android"
    token       TEXT NOT NULL,
    device_name TEXT,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(user_id, token)
);

-- Refresh tokens
CREATE TABLE refresh_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked     BOOLEAN DEFAULT FALSE,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_devices_user_id ON devices(user_id);
CREATE INDEX idx_sessions_device_id ON terminal_sessions(device_id);
CREATE INDEX idx_push_tokens_user_id ON push_tokens(user_id);
```

**Redis Key 設計（Scrollback Only）：**
```
session:{id}:scrollback   → Stream（raw PTY bytes，TTL 24h，max 50k entries）
session:{id}:state        → Hash { cols, rows, status, last_activity }
agent:{device_id}:ping    → String "1"（TTL 30s，15s 心跳更新）
```

---

## 通訊協定

### MessagePack over WebSocket（Binary Frame）

```go
// pkg/protocol/packet.go

type Packet struct {
    Type    uint8  `msgpack:"t"`
    Session string `msgpack:"s,omitempty"`
    Payload []byte `msgpack:"p,omitempty"`
}

type ResizePayload struct {
    Cols uint16 `msgpack:"c"`
    Rows uint16 `msgpack:"r"`
}

type AlertPayload struct {
    Pattern string `msgpack:"p"`   // 觸發的關鍵字
    Excerpt []byte `msgpack:"e"`   // 觸發的輸出片段（for notification body）
}

const (
    TypeTerminalOutput = 0x01  // PTY 輸出（raw bytes）
    TypeTerminalInput  = 0x02  // 鍵盤輸入（raw bytes）
    TypeResize         = 0x03  // { cols, rows }
    TypeSessionList    = 0x10  // 完整 session 列表
    TypeSessionStatus  = 0x11  // 單一 session 狀態變化
    TypeScrollback     = 0x12  // 歷史輸出回放 chunks
    TypeScrollbackDone = 0x13  // 回放完畢，切換即時串流
    TypeAlert          = 0x14  // 觸發 push notification
    TypeAuth           = 0x20
    TypeAuthOK         = 0x21
    TypeAuthFail       = 0x22
    TypeSubscribe      = 0x30  // 訂閱 session（帶 fromOffset）
    TypeUnsubscribe    = 0x31
    TypePing           = 0xFF
    TypePong           = 0xFE
)
```

### In-Memory Hub 核心實作

```go
// backend/internal/hub/hub.go

type Hub struct {
    // Agent 連線：一個裝置一個連線
    agents map[string]*AgentConn     // deviceID → conn
    // Client 訂閱：一個 session 可能有多個 client 同時看
    subs   map[string][]*ClientConn  // sessionID → []conn
    mu     sync.RWMutex

    // 僅在 MULTI_INSTANCE_MODE=true 時啟用
    redisPub *redis.Client
}

// Broadcast 是 hot path，必須盡量快
func (h *Hub) Broadcast(sessionID string, pkt []byte) {
    // 1. in-memory 廣播（永遠執行）
    h.mu.RLock()
    clients := h.subs[sessionID]
    h.mu.RUnlock()
    for _, c := range clients {
        c.send(pkt)  // non-blocking channel send
    }

    // 2. Redis 持久化（async，不阻塞廣播）
    go h.appendScrollback(sessionID, pkt)

    // 3. 跨 replica 廣播（僅 multi-instance mode）
    if h.redisPub != nil {
        go h.redisPub.Publish(ctx, "session:"+sessionID, pkt)
    }
}
```

---

## Agent 設計

### CLI 介面

```bash
# 安裝
curl -fsSL https://get.ccmux.app | bash
brew install ccmux           # macOS Homebrew

# 初始化（在 App 上登入後，掃描 QR code 或輸入 pairing code）
ccmux auth pair

# Daemon 管理
ccmux daemon start           # 啟動（自動加入 launchd/systemd）
ccmux daemon stop
ccmux daemon status

# Session 管理（向 daemon 發 IPC 指令）
ccmux new                    # 新 session（使用 $SHELL）
ccmux new "make all"         # 執行指定指令
ccmux new --name build "make all"
ccmux list                   # 列出所有 session
ccmux attach <id>            # 在本機 terminal attach（類 tmux attach）
ccmux kill <id>
```

### PTY Session 生命週期

```
ccmux new bash
  → CLI 透過 Unix socket 向 daemon 發 CreateSession 請求
  → Daemon 呼叫 forkpty()（creack/pty）
  → fork 子行程執行 /bin/bash
  → Daemon 啟動 goroutine 讀取 PTY master fd
  → 讀到輸出 → 批次 16ms → WebSocket → Backend Hub → 廣播
  → 子行程 exit → Daemon 傳送 TypeSessionStatus（exit_code）
             → Backend 更新 DB + 觸發 push notification
```

---

## 效能設計細節

### 絲滑的五個支柱

| 支柱 | 實作 | 效果 |
|------|------|------|
| In-memory routing | Hub 純記憶體廣播 | 訊息路由 < 1ms |
| 16ms batching | Agent 攢批輸出 | ~60fps 節奏，減少 95% WS 訊息數 |
| Scrollback replay | Redis Streams offset | 重連畫面瞬間還原，無空白等待 |
| Impeller GPU 渲染 | Flutter Impeller | terminal 文字 120fps |
| Optimistic resize | 本地先 reflow | 旋轉螢幕零延遲 |

### Backpressure（防止手機過熱）

```go
// backend/internal/hub/client_conn.go
const sendChSize = 256           // client send channel buffer

func (c *ClientConn) send(data []byte) {
    select {
    case c.sendCh <- data:       // 正常：放入 buffer
    default:
        // buffer 滿 → 客戶端消費太慢 → 丟棄此幀（terminal 輸出允許丟幀）
        // 下次 scrollback replay 可補回
        c.dropCount++
    }
}
```

### Agent 的 32KB 立即發送

當單次 PTY 輸出超過 32KB（如 `cat large_file`），不等 16ms ticker，立即送出：

```go
case data := <-outputCh:
    buf.Write(data)
    if buf.Len() >= 32*1024 {   // 大輸出立即送，不積壓
        flush()
    }
```

---

## 目錄結構

```
ccmux/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── hub/
│   │   │   ├── hub.go              # In-memory 路由核心（hot path）
│   │   │   ├── agent_conn.go
│   │   │   ├── client_conn.go
│   │   │   └── multinode.go        # Redis pub/sub（MULTI_INSTANCE_MODE only）
│   │   ├── auth/
│   │   │   ├── jwt.go
│   │   │   ├── password.go
│   │   │   └── device_token.go
│   │   ├── session/
│   │   │   ├── manager.go
│   │   │   └── scrollback.go       # Redis Streams async 讀寫
│   │   ├── notify/
│   │   │   ├── apns.go             # Apple Push Notification Service
│   │   │   ├── fcm.go              # Firebase Cloud Messaging
│   │   │   └── dispatcher.go       # 判斷是否要送、送給哪些 token
│   │   ├── store/
│   │   │   ├── postgres.go
│   │   │   ├── users.go
│   │   │   ├── devices.go
│   │   │   ├── sessions.go
│   │   │   └── push_tokens.go
│   │   └── api/
│   │       ├── router.go
│   │       ├── middleware/
│   │       │   ├── auth.go
│   │       │   └── ratelimit.go
│   │       ├── auth_handler.go
│   │       ├── device_handler.go
│   │       ├── session_handler.go
│   │       └── push_handler.go     # POST /api/push/register
│   ├── pkg/protocol/packet.go
│   ├── migrations/
│   │   ├── 001_users.sql
│   │   ├── 002_devices.sql
│   │   ├── 003_sessions.sql
│   │   ├── 004_refresh_tokens.sql
│   │   └── 005_push_tokens.sql
│   ├── go.mod
│   └── Dockerfile
│
├── agent/
│   ├── cmd/agent/main.go           # cobra CLI 入口
│   ├── internal/
│   │   ├── daemon/
│   │   │   ├── daemon.go           # 主 daemon loop
│   │   │   └── ipc.go              # Unix socket IPC（CLI ↔ daemon）
│   │   ├── pty/
│   │   │   ├── session.go          # 單一 PTY session + alert pattern matching
│   │   │   └── pool.go             # Session pool 管理
│   │   └── relay/
│   │       ├── client.go           # WebSocket 連線至後端
│   │       ├── reconnect.go        # Exponential backoff
│   │       └── batch.go            # 16ms batching + 32KB immediate flush
│   ├── pkg/protocol/packet.go
│   ├── config/config.go            # ~/.config/ccmux/config.toml
│   ├── install/
│   │   ├── install.sh
│   │   ├── launchd.plist           # macOS 開機自啟
│   │   └── systemd.service         # Linux 開機自啟
│   └── go.mod
│
├── mobile/
│   ├── lib/
│   │   ├── features/
│   │   │   ├── auth/
│   │   │   │   ├── login_page.dart
│   │   │   │   ├── pair_page.dart      # QR code / pairing code 配對頁面
│   │   │   │   └── auth_provider.dart
│   │   │   ├── workspace/
│   │   │   │   ├── workspace_drawer.dart   # 左滑 Workspace + session 列表
│   │   │   │   ├── device_section.dart     # 依裝置分組
│   │   │   │   └── workspace_provider.dart
│   │   │   ├── terminal/
│   │   │   │   ├── terminal_page.dart      # 主畫面（Header + Tabs + Terminal）
│   │   │   │   ├── tab_panel.dart          # Tab bar（藍色光暈、紅色 exit badge）
│   │   │   │   ├── terminal_view.dart      # flutter_xterm 整合
│   │   │   │   ├── special_key_toolbar.dart # Tab/Esc/Ctrl/方向鍵工具列
│   │   │   │   └── terminal_provider.dart
│   │   │   └── notifications/
│   │   │       ├── notification_handler.dart  # APNs/FCM 接收處理
│   │   │       └── push_service.dart          # Token 注冊
│   │   ├── core/
│   │   │   ├── websocket/
│   │   │   │   ├── ws_client.dart          # 連線、token 刷新、重連
│   │   │   │   └── ws_reconnect.dart       # Exponential backoff + offset replay
│   │   │   └── protocol/
│   │   │       └── packet.dart             # MessagePack decode/encode
│   │   ├── router.dart
│   │   └── main.dart
│   ├── ios/
│   ├── android/
│   └── pubspec.yaml
│
├── go.work                         # Go workspace（共用 protocol package）
├── docker-compose.yml
├── docker-compose.prod.yml
└── ARCHITECTURE.md
```

---

## 實作階段

### Phase 1：後端 + 協定基礎（2-3 天）
- [ ] Go workspace + `pkg/protocol/packet.go`
- [ ] PostgreSQL schema（5 張表）+ migrations
- [ ] JWT 認證（register / login / refresh / logout）
- [ ] 裝置注冊 + device_token
- [ ] In-memory Hub 骨架（AgentConn、ClientConn、Broadcast）
- [ ] Scrollback async 寫入 Redis Streams
- [ ] Push token 注冊 API

### Phase 2：Desktop Agent（2-3 天）
- [ ] Daemon + Unix socket IPC
- [ ] PTY pool（fork、讀寫、exit 偵測）
- [ ] Alert pattern matching → TypeAlert
- [ ] WebSocket relay（16ms batch + 32KB immediate flush）
- [ ] Exponential backoff 重連
- [ ] 心跳（TypePing + Redis TTL 更新）
- [ ] macOS launchd / Linux systemd 安裝腳本

### Phase 3：後端串流完整流程（1-2 天）
- [ ] Scrollback replay（offset-based，斷線續傳）
- [ ] 鍵盤輸入 + Resize 轉發
- [ ] Session status 同步（exit code、title 更新）
- [ ] Push notification 送出（APNs + FCM）
- [ ] MULTI_INSTANCE_MODE Redis pub/sub（可後期再開）

### Phase 4：Flutter App（4-5 天）
- [ ] 登入 / QR code 配對 UI
- [ ] Workspace Drawer（裝置分組、session 狀態 badge）
- [ ] Tab Panel（藍色新輸出光暈、紅色 exit badge、+ 新建）
- [ ] Terminal View（flutter_xterm 整合）
- [ ] 特殊鍵工具列（Tab、Esc、Ctrl+C/D/Z、方向鍵）
- [ ] Push notification 接收 + 前景/背景處理
- [ ] WebSocket 斷線重連 + scrollback offset replay
- [ ] Pinch to zoom 字體大小
- [ ] 橫向/直向 auto resize → TypeResize

### Phase 5：安全性與打磨（1-2 天）
- [ ] TLS（Let's Encrypt）
- [ ] Rate limiting
- [ ] Session 存取控制強制驗證
- [ ] 裝置離線 UI（灰色 badge，session 標記 detached）
- [ ] E2E 測試（Agent → Backend → App 完整串流 + 通知驗證）

---

## 風險評估

| 風險 | 嚴重度 | 緩解方案 |
|------|--------|---------|
| ANSI 序列在 WS frame 邊界切斷 | HIGH | `flutter_xterm` VT100 parser 緩衝不完整序列，等待下一幀補完 |
| 手機網路切換導致斷線 | HIGH | `connectivity_plus` 偵測 → 重連 + Redis Streams offset replay 補回漏失 |
| APNs/FCM token 過期 | MEDIUM | 收到 410 Gone 時自動從 DB 刪除，App 下次啟動重新注冊 |
| 高頻輸出（cat large file）手機過熱 | MEDIUM | Client send channel backpressure，滿時丟幀（scrollback 可補） |
| CJK 字元寬度錯誤 | MEDIUM | `flutter_xterm` 使用 wcwidth，需 CJK 專項測試 |
| 未授權存取他人 terminal | CRITICAL | 後端訂閱時強制驗證 session → device → user 歸屬 |
| Redis 重啟 scrollback 遺失 | LOW | AOF 持久化，或接受重啟後 scrollback 清空（視場景接受） |

---

## 本地開發快速啟動

```yaml
# docker-compose.yml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: ccmux
      POSTGRES_USER: ccmux
      POSTGRES_PASSWORD: devpassword
    ports: ["5432:5432"]
    volumes: [postgres_data:/var/lib/postgresql/data]

  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes
    ports: ["6379:6379"]
    volumes: [redis_data:/data]

  backend:
    build: ./backend
    depends_on: [postgres, redis]
    environment:
      DATABASE_URL: postgres://ccmux:devpassword@postgres:5432/ccmux
      REDIS_URL: redis://redis:6379
      JWT_SECRET: dev_secret
      MULTI_INSTANCE_MODE: "false"
      SERVER_ADDR: :8080
    ports: ["8080:8080"]

volumes:
  postgres_data:
  redis_data:
```

```bash
docker compose up -d postgres redis
cd backend  && go run ./cmd/server migrate && go run ./cmd/server
cd agent    && go run ./cmd/agent daemon start --server ws://localhost:8080
cd mobile   && flutter run
```
