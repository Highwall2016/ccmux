# Ccmux — 實作進度

> 最後更新：2026-04-14

---

## Phase 1：後端 + 協定基礎（進行中）

### 已完成

| 檔案 / 目錄 | 說明 |
|------------|------|
| `go.work` | Go workspace，連結 backend + agent |
| `docker-compose.yml` | 開發環境：postgres:16 + redis:7 + backend |
| `backend/go.mod` | Go module `github.com/ccmux/backend` |
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
| `backend/internal/store/sessions.go` | CreateSession / GetSessionByID / ListSessionsByDevice / UpdateStatus / TouchSession |
| `backend/internal/store/push_tokens.go` | UpsertPushToken / DeletePushToken / GetPushTokensForUser / DeleteExpiredPushToken |
| `backend/internal/auth/password.go` | HashPassword / CheckPassword（bcrypt cost 12） |
| `backend/internal/auth/jwt.go` | NewAccessToken（15m）/ NewRefreshToken（30d）/ ParseToken |
| `backend/internal/auth/device_token.go` | GenerateDeviceToken（HMAC-SHA256）/ GenerateRefreshToken（SHA-256） |
| `backend/internal/hub/hub.go` | In-memory Hub：RegisterAgent / UnregisterAgent / Subscribe / Unsubscribe / **Broadcast**（hot path）/ ReplayScrollback |
| `backend/internal/hub/agent_conn.go` | AgentConn：ReadPump / writePump / Send |
| `backend/internal/hub/client_conn.go` | ClientConn：ReadPump / writePump / send（non-blocking，backpressure drop）|

### 待完成（Phase 1 剩餘）

- [ ] `backend/internal/api/middleware/auth.go` — JWT middleware
- [ ] `backend/internal/api/middleware/ratelimit.go` — Token bucket rate limiter
- [ ] `backend/internal/api/auth_handler.go` — register / login / refresh / logout
- [ ] `backend/internal/api/device_handler.go` — 裝置注冊 / 列表 / 刪除
- [ ] `backend/internal/api/session_handler.go` — session 列表
- [ ] `backend/internal/api/push_handler.go` — push token 注冊 / 刪除
- [ ] `backend/internal/api/ws_handler.go` — `/ws/agent` + `/ws/client` WebSocket 端點
- [ ] `backend/internal/api/router.go` — 路由組裝
- [ ] `backend/cmd/server/main.go` — 進入點（migrate 子指令 + HTTP server）
- [ ] `backend/Dockerfile` — 多階段構建
- [ ] `go mod tidy` + 確認 `go build ./...` 通過
- [ ] `agent/go.mod` + `agent/cmd/agent/main.go` — stub

---

## Phase 2–5（未開始）

| Phase | 內容 | 狀態 |
|-------|------|------|
| Phase 2 | Desktop Agent（PTY pool、IPC、WS relay、batching） | 未開始 |
| Phase 3 | 後端串流完整流程（scrollback replay、resize、push notification） | 未開始 |
| Phase 4 | Flutter App（Login、Workspace Drawer、Tab Panel、Terminal View、特殊鍵） | 未開始 |
| Phase 5 | 安全性與打磨（TLS、rate limit、E2E 測試） | 未開始 |

---

## 目前已確認的架構決策

| 問題 | 決策 |
|------|------|
| 通訊協定 | MessagePack over WebSocket binary frame |
| 路由核心 | In-memory Hub，hot path 零 Redis |
| 持久化 | Redis Streams scrollback（async），PostgreSQL metadata |
| 水平擴展 | `MULTI_INSTANCE_MODE=true` 啟用 Redis pub/sub，無需改代碼 |
| 行動平台 | Flutter（iOS + Android 單一 codebase） |
| 推播 | APNs（iOS）+ FCM（Android） |
| 認證 | JWT（access 15m + refresh 30d）+ device HMAC token |

---

## 下一步

繼續 Phase 1 剩餘的 API handlers + router + main.go，然後：
1. `go mod tidy` 加入所有依賴
2. `go build ./...` 驗證編譯
3. 繼續 Phase 2 Desktop Agent
