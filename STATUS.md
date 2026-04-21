# Ccmux — 實作進度

> 最後更新：2026-04-21（Phase 6 Mobile UI 重設計完成）

---

## Phase 1：後端 + 協定基礎（✅ 完成）

### 已完成

| 檔案 / 目錄 | 說明 |
|------------|------|
| `go.work` | Go workspace，連結 backend + agent |
| `docker-compose.yml` | 開發環境：postgres:16 + redis:7 + backend |
| `backend/go.mod` | Go module `github.com/ccmux/backend`（含所有依賴） |
| `backend/pkg/protocol/packet.go` | MessagePack 協定定義（所有 TypeXxx 常數、Packet/Encode/Decode） |
| `backend/migrations/001_users.sql` | users 表 |
| `backend/migrations/002_devices.sql` | devices 表 + index |
| `backend/migrations/003_sessions.sql` | terminal_sessions 表 + index |
| `backend/migrations/004_refresh_tokens.sql` | refresh_tokens 表 |
| `backend/migrations/005_push_tokens.sql` | push_tokens 表 + index |
| `backend/migrations/migrations.go` | `//go:embed *.sql` — 輸出 `migrations.FS` |
| `backend/internal/store/postgres.go` | `DB` struct，`Open()`，`Migrate(fs.FS)` |
| `backend/internal/store/users.go` | CreateUser / GetUserByEmail / GetUserByID |
| `backend/internal/store/devices.go` | CreateDevice / GetDeviceByID / ListDevicesByUser / DeleteDevice / TouchDevice / ValidateDeviceToken |
| `backend/internal/store/sessions.go` | CreateSession / GetSessionByID / ListSessionsByDevice / UpdateStatus / TouchSession / ResizeSession |
| `backend/internal/store/push_tokens.go` | UpsertPushToken / DeletePushToken / GetPushTokensForUser / DeleteExpiredPushToken |
| `backend/internal/store/refresh_tokens.go` | CreateRefreshToken / GetRefreshToken / RevokeRefreshToken / DeleteExpiredRefreshTokens |
| `backend/internal/auth/password.go` | HashPassword / CheckPassword（bcrypt cost 12） |
| `backend/internal/auth/jwt.go` | NewAccessToken（15m）/ NewRefreshToken（30d）/ ParseToken |
| `backend/internal/auth/device_token.go` | GenerateDeviceToken / HashDeviceToken / GenerateRefreshToken / HashRefreshToken |
| `backend/internal/hub/hub.go` | In-memory Hub：RegisterAgent / UnregisterAgent / Subscribe / Unsubscribe / **Broadcast**（hot path）/ ReplayScrollback |
| `backend/internal/hub/agent_conn.go` | AgentConn：ReadPump / writePump / Send |
| `backend/internal/hub/client_conn.go` | ClientConn：ReadPump / writePump / Send / send（non-blocking，backpressure drop）|
| `backend/internal/api/middleware/auth.go` | JWT Bearer middleware，UserIDFromCtx |
| `backend/internal/api/middleware/ratelimit.go` | Per-IP token bucket rate limiter |
| `backend/internal/api/auth_handler.go` | POST register / login / refresh / logout |
| `backend/internal/api/device_handler.go` | 裝置注冊 / 列表 / 刪除 |
| `backend/internal/api/session_handler.go` | session 列表（含 device 歸屬驗證） |
| `backend/internal/api/push_handler.go` | push token 注冊 / 刪除 |
| `backend/internal/api/ws_handler.go` | `/ws/agent` + `/ws/client` WebSocket 端點（in-protocol auth、scrollback replay、ownership check） |
| `backend/internal/api/router.go` | App struct + chi 路由組裝 |
| `backend/cmd/server/main.go` | 進入點（`migrate` 子指令 + HTTP server） |
| `backend/Dockerfile` | 多階段構建（golang:1.24-alpine → alpine:3.20） |
| `agent/go.mod` | Go module `github.com/ccmux/agent`（Phase 2–3 持續擴充依賴） |
| `agent/cmd/agent/main.go` | 主程式（Phase 2 實作完成） |

> `go mod tidy` + `go build ./...` ✅ 已驗證（Go 1.26.2）

---

## Phase 2：Desktop Agent（✅ 完成）

### 新增 / 修改檔案

| 檔案 | 說明 |
|------|------|
| `agent/go.mod` | 依賴：creack/pty、gorilla/websocket、msgpack/v5、backend（workspace） |
| `agent/internal/config/config.go` | 環境變數設定（CCMUX_SERVER_URL、DEVICE_ID、DEVICE_TOKEN、IPC_SOCKET、SHELL） |
| `agent/internal/pty/session.go` | 單一 PTY session（Start / Write / Resize / Wait / Close） |
| `agent/internal/pty/manager.go` | PTY session pool（Spawn / Write / Resize / Kill / List）含讀迴圈 |
| `agent/internal/relay/batcher.go` | 16 ms 輸出批次器（每 session 獨立 buffer，ticker 定時 flush） |
| `agent/internal/relay/conn.go` | WS Conn：TypeAuth 握手、writePump、readPump、指數退避重連 |
| `agent/internal/ipc/server.go` | Unix socket IPC（spawn / kill / list JSON 指令） |
| `agent/cmd/agent/main.go` | 主程式：組裝 wsConn → batcher → ptyMgr → IPC server → Run |
| `backend/internal/store/sessions.go` | 新增 `UpsertSession`（agent 宣告 active 時建立 DB 記錄） |
| `backend/internal/api/ws_handler.go` | TypeSessionStatus "active" 路徑改呼叫 UpsertSession |

### 架構摘要

```
[ccmux-ctl CLI]
      │  JSON over Unix socket (/tmp/ccmux.sock)
      ▼
[agent IPC server]
      │  spawn(sessionID, cmd, cols, rows)
      ▼
[PTY Manager] ──── read loop ──▶ [Batcher 16ms] ──▶ TypeTerminalOutput
      │  write/resize                                         │
      ◀────────────────────────────────────────────────────── │
[relay.Conn] ◀──── TypeTerminalInput / TypeResize ◀── Backend WS
      │  TypeAuth (device token) + reconnect
      ▼
[Backend /ws/agent]
```

---

## Phase 3：Push Notification + ccmux-ctl（✅ 完成）

| 檔案 | 說明 |
|------|------|
| `backend/internal/notify/fcm.go` | FCM v1 HTTP sender（OAuth2 service account，涵蓋 iOS + Android） |
| `backend/internal/notify/dispatcher.go` | fanOut to all user push tokens；NotifySessionExit / NotifyAlert |
| `backend/internal/api/router.go` | App struct 加 `Notify *notify.Dispatcher` |
| `backend/internal/api/ws_handler.go` | TypeSessionStatus exited → push；TypeAlert → broadcast + push |
| `backend/cmd/server/main.go` | 讀取 FCM_SERVICE_ACCOUNT_PATH / FCM_PROJECT_ID，optional |
| `agent/internal/pty/manager.go` | 加 `onAlert` callback + `checkAlerts`（error/failed/panic/fatal，30s cooldown） |
| `agent/cmd/agent/main.go` | 接 onAlert → TypeAlert WS packet |
| `agent/cmd/ctl/main.go` | `ccmux-ctl spawn/kill/list`（Unix socket IPC，auto-detect terminal size） |

## Phase 4：Flutter App（✅ 完成）

| 路徑 | 說明 |
|------|------|
| `mobile/pubspec.yaml` | xterm、web_socket_channel、messagepack、riverpod、go_router、dio、firebase |
| `mobile/lib/core/protocol/packet.dart` | Packet encode/decode（msgpack string keys "t"/"s"/"p"） |
| `mobile/lib/core/storage/secure_storage.dart` | JWT tokens 安全儲存；isAccessTokenValid |
| `mobile/lib/core/api/api_client.dart` | Dio + JwtInterceptor（auto-refresh on 401）|
| `mobile/lib/core/api/api_models.dart` | AuthResponse / DeviceModel / SessionModel |
| `mobile/lib/core/websocket/ws_client.dart` | WS state machine：connect → TypeAuth → TypeAuthOK → stream |
| `mobile/lib/core/websocket/ws_reconnect.dart` | 指數退避 + connectivity_plus 觸發立即重連 |
| `mobile/lib/features/auth/` | login_page / register_page / auth_provider（Riverpod AsyncNotifier）|
| `mobile/lib/features/workspace/` | workspace_provider / workspace_drawer / device_section |
| `mobile/lib/features/terminal/terminal_provider.dart` | 每 session xterm.Terminal；output / scrollback / status dispatch |
| `mobile/lib/features/terminal/terminal_page.dart` | Scaffold：AppBar + TabPanel + TerminalView + SpecialKeyToolbar |
| `mobile/lib/features/terminal/tab_panel.dart` | 橫向 session 標籤；new-output 藍點；exit 紅圈 |
| `mobile/lib/features/terminal/terminal_view.dart` | xterm TerminalView；resize send；pinch-to-zoom |
| `mobile/lib/features/terminal/special_key_toolbar.dart` | Tab/Esc/Ctrl+C/D/Z/L/↑↓←→/PgUp/PgDn |
| `mobile/lib/features/notifications/push_service.dart` | FCM token 取得 + POST /api/push/register |
| `mobile/lib/features/notifications/notification_handler.dart` | foreground local notif；background/terminated tap routing |
| `mobile/lib/router.dart` | go_router：/login → /register → /terminal；auth redirect |
| `mobile/lib/main.dart` | Firebase init → initNotifications → ProviderScope |
| `mobile/android/` | AndroidManifest（POST_NOTIFICATIONS）、MainActivity.kt |
| `mobile/ios/` | AppDelegate.swift（Firebase + FCM delegate）、Podfile（iOS 13）|

---

## Phase 5：tmux 整合（✅ 完成）

### 新增 / 修改檔案

| 檔案 | 說明 |
|------|------|
| `agent/internal/tmux/tmux.go` | tmux 偵測與操作：`IsRunning` / `ListPanes` / `NewWindow` / `SplitWindow`；以 pane ID SHA-256 產生穩定 ccmux session UUID（`deriveCcmuxID`） |
| `agent/internal/tmux/discover.go` | 自動發現並注冊 tmux pane 為 ccmux session（`DiscoverAndRegister`）；定期 reconcile；廣播 tmux pane 樹（`TypeTmuxTree`） |
| `backend/migrations/006_sessions_tmux.sql` | 為 `terminal_sessions` 加 `tmux_target TEXT` 和 `tmux_backed BOOLEAN` 欄位 |
| `backend/pkg/protocol/packet.go` | 新增 `TypeTmuxTree = 0x32`；新增 `TmuxTreePayload` / `TmuxSessionTree` / `TmuxWindowTree` / `TmuxPaneTree` 結構體；`SpawnRequest` 加 `UseTmux` / `TmuxSplit` 欄位；`SessionStatusPayload` 加 `TmuxBacked` / `TmuxTarget` 欄位 |
| `agent/internal/pty/manager.go` | 加 `SpawnTmux` 方法（以 tmux pane 做 I/O）；`HasSession` 查詢方法 |
| `agent/cmd/agent/main.go` | 啟動時呼叫 `tmux.DiscoverAndRegister`（interval 10s） |
| `agent/cmd/ctl/main.go` | `ccmux new` 加 `--tmux` / `--tmux-split` 旗標 |
| `agent/internal/config/config.go` | 加 `CCMUX_TMUX_DISCOVER` 環境變數（預設啟用） |
| `agent/internal/ipc/server.go` | `spawn` 指令加 `use_tmux` / `tmux_split` 欄位 |
| `agent/internal/relay/conn.go` | 轉發 `TypeTmuxTree` 封包至 backend |
| `backend/internal/api/session_handler.go` | 列出 session 時包含 `tmux_backed` / `tmux_target` 欄位 |
| `backend/internal/api/ws_handler.go` | 轉發 `TypeTmuxTree` 給所有訂閱同裝置的 client |
| `backend/internal/hub/hub.go` | Hub 新增 `BroadcastToDevice` 廣播至裝置所有訂閱者 |
| `backend/internal/store/sessions.go` | `UpsertSession` 和 `CreateSession` 支援 `tmux_backed` / `tmux_target` 欄位 |
| `mobile/lib/core/api/api_models.dart` | `SessionModel` 加 `tmuxBacked` / `tmuxTarget`；新增 `TmuxTree` / `TmuxSessionNode` / `TmuxWindowNode` / `TmuxPaneNode` 模型 |
| `mobile/lib/core/protocol/packet.dart` | 解碼 `TypeTmuxTree` 封包 |
| `mobile/lib/features/terminal/modifier_provider.dart` | `ArmedModifier` 狀態（none / ctrl / alt）；同一時間只能 arm 一個 |
| `mobile/lib/features/terminal/swipe_pref_provider.dart` | 持久化偏好：是否啟用左右滑動切換 tmux window（SharedPreferences） |
| `mobile/lib/features/terminal/tmux_prefix_button.dart` | Ctrl+B 按鈕（短按送 prefix，長按彈出快捷操作：new/next/prev/rename window，split，detach） |
| `mobile/lib/features/terminal/tmux_window_tabs.dart` | tmux-backed session 下方顯示橫向 window tab bar；點擊切換 window；右側滑動開關圖示 |
| `mobile/lib/features/workspace/tmux_hierarchy_section.dart` | workspace drawer 中以 session → window → pane 三層樹狀結構顯示 tmux 拓樸；bare PTY session 顯示於「Direct PTY」區塊 |
| `mobile/lib/features/terminal/terminal_page.dart` | 整合 `TmuxWindowTabs` 和 `TmuxPrefixButton`；tmux-backed session 自動顯示額外控制項 |
| `mobile/lib/features/terminal/terminal_provider.dart` | 追蹤 `tmuxBacked` 狀態；處理 `TypeTmuxTree` 更新 workspace |
| `mobile/lib/features/terminal/terminal_view.dart` | 加入水平滑動偵測（當 `swipePrefProvider` 開啟時），左滑 / 右滑送 Ctrl+B n / p 切換 window |
| `mobile/lib/features/terminal/special_key_toolbar.dart` | 工具列整合 modifier provider；Ctrl / Alt 可 arm，下一次按鍵自動附加 modifier |
| `mobile/lib/features/workspace/workspace_drawer.dart` | 有 tmuxTree 時使用 `TmuxHierarchySection`；無 tmux 時 fallback 至原有平面列表 |
| `mobile/lib/features/workspace/workspace_provider.dart` | 接收並解析 `TypeTmuxTree` 封包，更新 `WorkspaceState.tmuxTreeByDevice` |
| `mobile/pubspec.yaml` / `pubspec.lock` | 新增 `shared_preferences` 依賴（用於滑動偏好持久化） |

### 架構補充

```
[tmux server]
  └─ panes (list-panes -a)
        │  每 10s 或 agent 啟動時 reconcile
        ▼
[agent/internal/tmux.DiscoverAndRegister]
  ├─ SpawnTmux(ccmuxID, name, target) → PTY Manager（attach to tmux pane I/O）
  └─ sendTmuxTree → TypeTmuxTree → Backend → BroadcastToDevice → Mobile
        │
        ▼
[Mobile workspace drawer]
  tmuxTree: session → window → pane (三層)
[Mobile terminal page]
  TmuxWindowTabs（window tab bar）
  TmuxPrefixButton（Ctrl+B + 快捷操作）
  swipe left/right → next/prev window
```

---

## Phase 6：Mobile UI 重設計（✅ 完成）

### 設計來源

`ui/ccmux-app.html` — Claude Design 匯出的 HTML 原型；以深色調（`#0d0d0f`）、綠色 accent（`#3DDC84`）、JetBrains Mono 字型為主視覺語言。

### 新增 / 修改檔案

| 檔案 | 說明 |
|------|------|
| `mobile/lib/core/theme.dart` | 新增 `CcmuxColors` 色彩常數（bg / accent / red / blue / cyan / yellow / purple）及 `buildCcmuxTheme()` |
| `mobile/lib/features/workspace/session_list_page.dart` | 全新首頁：Active / Ended 分區列表、彩色 avatar、左滑 Kill、長按 context sheet、裝置切換 sheet、設定 sheet、新 session sheet、重命名 sheet |
| `mobile/lib/features/workspace/workspace_provider.dart` | 新增 `selectedDeviceIdProvider`（追蹤目前選取裝置） |
| `mobile/lib/features/terminal/terminal_page.dart` | 全面重設計：自訂 nav bar（返回 + session 名稱 + CPU/MEM 小工具）、移除 TabPanel 和底部輸入列；改為 `ConsumerWidget` |
| `mobile/lib/features/terminal/special_key_toolbar.dart` | 重新設計：深色背景（`#0A0A0C`）、圓角按鍵、Ctrl+C 紅色、按下動畫、Ctrl/Alt arm 狀態以 accent 色呈現 |
| `mobile/lib/router.dart` | 登入後跳轉至 `/sessions`；新增 `/sessions` 路由（`SessionListPage`） |
| `mobile/lib/main.dart` | 使用 `buildCcmuxTheme()` 取代舊的 teal seed 主題 |

### 主要 UX 變更

- **導覽架構翻轉**：原本以終端機為首頁（側抽屜選 session）→ 改為 session 列表為首頁，點擊 session 進入終端機，返回鍵回列表
- **Session 列表**：avatar 以 session 名稱 hash 決定顏色；unread 狀態從 `terminalProvider.hasNewOutput` 取得；依 lastActivity 排序
- **底部輸入列移除**：直接點按終端機畫面喚出鍵盤輸入
- **CPU / MEM 動畫**：終端機 nav bar 顯示模擬資源使用量（active session 時每 1.2s 隨機浮動），符合設計原型行為

---

## Phase 7（未開始）

| 內容 | 狀態 |
|------|------|
| TLS 設定（cert-manager / Let's Encrypt） | 未開始 |
| Rate limit 壓力測試 | 未開始 |
| E2E 測試（agent ↔ backend ↔ Flutter） | 未開始 |
| Flutter iOS 推播沙箱驗收 | 未開始 |
| Docker image 推至 registry | 未開始 |

---

## 目前已確認的架構決策

| 問題 | 決策 |
|------|------|
| 通訊協定 | MessagePack over WebSocket binary frame |
| 路由核心 | In-memory Hub，hot path 零 Redis |
| 持久化 | Redis Streams scrollback（async），PostgreSQL metadata |
| 水平擴展 | `MULTI_INSTANCE_MODE=true` 啟用 Redis pub/sub，無需改代碼 |
| 行動平台 | Flutter（iOS + Android 單一 codebase） |
| 推播 | FCM v1 HTTP API（iOS + Android 統一，Firebase 處理 APNs 橋接） |
| 認證 | JWT（access 15m + refresh 30d）+ device HMAC token |
| HTTP Router | chi v5 |
| Rate Limit | Per-IP token bucket（golang.org/x/time/rate） |

---

## 下一步

1. **啟動服務**：`docker compose up -d` → `go run ./backend/cmd/server` → `go run ./agent/cmd/agent`
2. **Flutter app**：安裝 Flutter → `cd mobile && flutter pub get` → `flutter run`
3. **Firebase 設定**：`google-services.json`（Android）+ `GoogleService-Info.plist`（iOS）放入對應目錄
4. **Phase 7**：TLS、E2E 測試、壓力測試
