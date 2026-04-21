# ccmux — Security Analysis

> This document reviews every layer of the ccmux architecture from a security perspective.  
> It covers what is already protected, what is missing or risky, and concrete fixes ranked by severity.

---

## Architecture Threat Model

ccmux is a **terminal relay service**. The worst-case attack is:

> An attacker **types commands into someone else's terminal** on a real computer.

That makes ccmux a high-value target. Every layer must be treated with the same seriousness as SSH.

```
[Mobile App] ─── TLS ──→ [Backend Relay] ─── TLS ──→ [Desktop Agent]
                                │
                          [PostgreSQL]  [Redis]
```

Threats exist at:
1. **Transport** — is traffic encrypted in transit?
2. **Authentication** — who can connect to what?
3. **Authorization** — can user A touch user B's terminal?
4. **Agent security** — is the daemon safe to run on a developer's machine?
5. **Server security** — is the backend hardened?
6. **Data at rest** — are credentials and scrollback data safe?
7. **Secrets management** — are keys and secrets stored securely?

---

## Layer 1: Transport Security

### ✅ What Is Already Done

The architecture requires TLS everywhere (Phase 7 item: Let's Encrypt via Caddy/Nginx). The protocol uses WebSocket (`wss://`) and all HTTP REST calls go to the same HTTPS origin.

### 🔴 CRITICAL: TLS Is Not Yet Enforced in Code

In `docker-compose.yml`, the backend binds to plain HTTP `:8080` with no TLS termination configured yet. Phase 7 (TLS setup) is listed as **未開始 (not started)**.

**Current state:**
```yaml
# docker-compose.yml — backend is plain HTTP/WS, no TLS
ports: ["8080:8080"]
```

**Risk:** Until Caddy or Nginx is in front with TLS, all traffic — including JWT tokens, device tokens, and raw PTY bytes — travels in plaintext over the network.

**Fix:** Add Caddy to docker-compose immediately:
```yaml
# docker-compose.yml — add this service
caddy:
  image: caddy:2-alpine
  ports:
    - "443:443"
    - "80:80"
  volumes:
    - ./Caddyfile:/etc/caddy/Caddyfile:ro
    - caddy_data:/data
  depends_on:
    - backend
```

```
# Caddyfile
yourdomain.com {
  reverse_proxy backend:8080
}
```

### ⚠️ WebSocket Origin Check Is Disabled

```go
// ws_handler.go line 20
CheckOrigin: func(r *http.Request) bool { return true },
```

This disables the browser Same-Origin check on WebSocket upgrades. Since the agent and mobile app are not browsers, this is **not a CSRF risk**, but it does mean any website could initiate a WS connection from a user's browser if they're logged in.

**Fix for production:** Restrict origin to your own domain:
```go
CheckOrigin: func(r *http.Request) bool {
    origin := r.Header.Get("Origin")
    return origin == "" || origin == "https://yourdomain.com"
},
```

---

## Layer 2: Authentication

### ✅ What Is Already Done

| Mechanism | Implementation | Assessment |
|---|---|---|
| Password hashing | bcrypt cost 12 | ✅ Strong |
| Access token | JWT HS256, 15-minute TTL | ✅ Short-lived |
| Refresh token | SHA-256 hashed in DB, 30-day TTL, revocable | ✅ Good |
| Device token | HMAC-SHA256(random 32 bytes, server secret) | ✅ Strong |
| WS auth timeout | 15-second deadline for TypeAuth after upgrade | ✅ Good |
| Signing method check | `if _, ok := t.Method.(*jwt.SigningMethodHMAC)` | ✅ Prevents alg:none attacks |

### 🔴 HIGH: JWT Uses HS256 (Shared Secret), Not RS256

`jwt.go` uses `jwt.SigningMethodHS256` with a single shared secret (`JWT_SECRET` env var). This is fine while you have **one backend instance**, but:

- If `JWT_SECRET` leaks (e.g., from a misconfigured environment), an attacker can forge **any user's JWT with any user ID**.
- HS256 means the same key signs and verifies — whoever can verify tokens can also mint them.

**Risk level:** Medium–High. The secret being in an env var is a common deployment pattern, but it's one leak away from full account takeover for all users.

**Fix (longer term):** Switch to RS256 (asymmetric). Private key signs, public key verifies. The public key can be published without risk.

```go
// Future: jwt.go — use RS256
t := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
signed, _ := t.SignedString(privateKey)  // RSA private key
```

**Short-term fix:** Ensure `JWT_SECRET` is a minimum 256-bit (32-byte) random value and is loaded from a proper secrets manager, not from version-controlled config.

### ⚠️ MEDIUM: No Account Lockout After Failed Logins

The rate limiter caps auth endpoints at **~12 requests/minute** (0.2 req/s):

```go
// router.go line 35
r.Use(mw.RateLimiter(rate.Limit(0.2), 5)) // ~12/min, burst 5
```

12 attempts per minute = 720/hour = 17,280/day. A 6-character lowercase password has ~309 million combinations but an attacker picking common passwords (rockyou.txt top 1,000) could try all 1,000 in ~1.4 hours **per IP**.

**Fix:** Add a per-email failed-attempt counter in Redis or PostgreSQL. After 5 failures in 10 minutes, lock for 15 minutes:

```go
// After failed login
failKey := "login:fail:" + req.Email
count, _ := redis.Incr(ctx, failKey)
redis.Expire(ctx, failKey, 10*time.Minute)
if count > 5 {
    http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
    return
}
```

### ⚠️ MEDIUM: X-Forwarded-For Spoofing in Rate Limiter

```go
// ratelimit.go line 51-52
if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
    ip = strings.SplitN(fwd, ",", 2)[0]
}
```

If the backend is **directly exposed** (no reverse proxy), an attacker can bypass the rate limiter by sending:
```
X-Forwarded-For: 1.2.3.4
```
and rotating that header freely, so each request appears to come from a different IP.

**Fix:** Only trust `X-Forwarded-For` when behind a known proxy. Set a `TRUSTED_PROXIES` env var:

```go
// Only read X-Forwarded-For when behind a trusted proxy (Caddy/Nginx)
if isTrustedProxy(r.RemoteAddr) {
    ip = strings.SplitN(r.Header.Get("X-Forwarded-For"), ",", 2)[0]
} else {
    ip = r.RemoteAddr
}
```

---

## Layer 3: Authorization (The Most Critical Layer)

### ✅ What Is Already Done

This is the strongest part of the security model. The code enforces **session → device → user** ownership at every sensitive operation:

```go
// ws_handler.go — TypeTerminalInput/TypeResize (line 259-265)
session, _ := a.DB.GetSessionByID(pkt.Session)
dev, _ := a.DB.GetDeviceByID(session.DeviceID)
if dev.UserID != claims.UserID { return }  // ✅ enforced

// ws_handler.go — TypeSubscribe (line 238-241)
dev, _ := a.DB.GetDeviceByID(session.DeviceID)
if dev.UserID != claims.UserID { return }  // ✅ enforced

// session_handler.go — every REST handler
if device.UserID != userID { http.Error(w, "not found", 404) }  // ✅ enforced
```

This correctly prevents User A from subscribing to, typing into, or killing User B's sessions.

### ⚠️ MEDIUM: No Input Sanitization on `Command` Field

When spawning a session via `POST /api/devices/{deviceID}/sessions`:

```go
// session_handler.go line 47-49
if req.Command == "" {
    req.Command = "bash"
}
```

The `Command` string is sent directly to the agent via MessagePack and executed via `forkpty()`. There is no validation of what command can be run. This is **by design** (ccmux is a full terminal access tool), but it means:

**Risk:** If an attacker somehow bypasses auth and reaches this endpoint, they can execute **any arbitrary command** on the target machine.

**Mitigation (not a fix, since the feature is intentional):** The auth chain must be airtight. The ownership checks above are the primary defense. Additionally, add a server-side log of every spawn with user ID + device ID for audit purposes.

### ⚠️ LOW: Session ID Enumeration via `/api/devices/{deviceID}/sessions/{sessionID}`

Session IDs are UUIDs generated by `crypto/rand`. They are not guessable (128 bits of entropy). The ownership check is enforced. This is **not a real risk**, but worth documenting.

---

## Layer 4: Agent Security (Desktop Daemon)

The agent runs as the **logged-in user on their own machine** and executes PTY sessions. This is intentional — it's equivalent to having SSH access to yourself.

### 🔴 HIGH: Device Token Stored in Plaintext in `.env.agent`

```bash
# .env.agent (generated by setup-local.sh)
CCMUX_DEVICE_ID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
CCMUX_DEVICE_TOKEN=xxxxxxxxxxxxxxxxxx...  # raw 32-byte hex token in plaintext
CCMUX_SERVER_URL=ws://localhost:8080
```

This file on disk is the **equivalent of an SSH private key**. If an attacker reads it (e.g., via another process with the same user permissions, a backup, or accidental git commit), they can impersonate the device to the backend and receive the user's terminal output.

**Risks:**
- File accidentally committed to git (`.gitignore` should exclude it)
- Malware on the same machine reads it
- Backup synced to unencrypted cloud storage

**Fixes:**
1. Verify `.gitignore` excludes `.env.agent`:
   ```gitignore
   .env.agent  # should already be here
   ```
2. Consider storing the token in the OS keychain (macOS Keychain, Linux Secret Service) instead of a plaintext file.
3. Set restrictive file permissions on installation:
   ```bash
   chmod 600 ~/.config/ccmux/config.toml
   ```

### ⚠️ MEDIUM: Unix Socket IPC Has No Authentication

```go
// ipc/server.go — anyone with access to /tmp/ccmux.sock can send commands
```

The IPC socket at `/tmp/ccmux.sock` uses filesystem permissions for access control — only processes running as the same Unix user can connect. This is the standard approach (same as Docker socket, tmux socket). However:

- If another process running as the same user is compromised (e.g., a malicious npm package, a browser exploit), it can spawn arbitrary PTY sessions.

**Fix:** This is an accepted trade-off for local IPC. Document it clearly. Consider moving the socket to `~/.run/ccmux/` with `chmod 700` on the directory.

### ✅ Agent WebSocket Auth Is HMAC-Based

The agent authenticates to the backend using `HMAC-SHA256(raw_token, hmac_secret)`. The raw token never leaves the device — the backend stores only the hash. This is equivalent to how GitHub stores personal access tokens. ✅ Correct design.

---

## Layer 5: Server Hardening

### 🔴 HIGH: `JWT_SECRET` and `HMAC_SECRET` Are Weak in Default Config

```yaml
# docker-compose.yml — CHANGE THESE IMMEDIATELY
JWT_SECRET: dev_secret_change_in_prod
HMAC_SECRET: dev_hmac_secret_change_in_prod
```

These are **shipped as defaults in the repository**. If deployed without changing them, an attacker who reads the repo can forge any JWT or device token.

**Fix:** Generate strong secrets at deploy time:
```bash
# Generate 256-bit (32-byte) secrets
JWT_SECRET=$(openssl rand -hex 32)
HMAC_SECRET=$(openssl rand -hex 32)
```

Store them in environment variables, **not** in `docker-compose.yml`. Use Docker secrets, environment injection at deploy time, or a secrets manager (Vault, AWS Secrets Manager, Doppler).

### ⚠️ MEDIUM: No HTTPS-Only Headers on HTTP Responses

Once TLS is set up via Caddy, the backend should emit security headers. These should be set at the Caddy level:

```
# Caddyfile — add security headers
header {
    Strict-Transport-Security "max-age=31536000; includeSubDomains"
    X-Content-Type-Options "nosniff"
    X-Frame-Options "DENY"
    Referrer-Policy "no-referrer"
}
```

### ✅ chi Recoverer Middleware Is Enabled

```go
r.Use(chimw.Recoverer)
```

This prevents panics from crashing the whole server and leaking stack traces to clients. ✅ Correct.

---

## Layer 6: Data At Rest

### ⚠️ PostgreSQL Stores Passwords as bcrypt Hashes

bcrypt cost 12 is strong. Even if the database is stolen, cracking passwords is computationally expensive. ✅

### ⚠️ Redis Scrollback Is Not Encrypted

The Redis scrollback streams (`session:{id}:scrollback`) contain **raw PTY bytes** — everything the user typed and saw in their terminal, including potentially passwords typed at prompts (like `sudo` or `ssh`).

If `ls -la ~/.ssh` or `cat ~/.env` is run, those secrets appear in the scrollback.

**Risks:**
- If Redis is exposed without a password (default Redis requires no auth)
- If the VPS is compromised and Redis memory is dumped

**Fixes:**
1. **Require Redis authentication.** Add `requirepass` to Redis config:
   ```yaml
   # docker-compose.yml
   redis:
     command: redis-server --appendonly yes --requirepass "${REDIS_PASSWORD}"
   ```
2. **Bind Redis to localhost only** (already done via Docker networking):
   ```yaml
   # Do NOT expose Redis port 6379 to host
   # Remove "ports: [6379:6379]" from docker-compose in production
   ```
3. **Enable Redis AOF encryption at rest** (requires Redis Enterprise or a filesystem-level solution like LUKS).

### 🔴 HIGH: Redis Port Exposed in docker-compose.yml

```yaml
# docker-compose.yml line 19
redis:
  ports: [ "6379:6379" ]  # ← EXPOSED TO HOST NETWORK
```

This maps Redis to `0.0.0.0:6379` on the host. If the VPS firewall is misconfigured or not set up, **Redis is accessible from the internet with no authentication**.

> In 2019–2023, exposed Redis instances were among the most common causes of cloud server compromise. Attackers dump the DB, then use `config set dir` + `config set dbfilename` to write SSH keys.

**Fix:** Remove the host port mapping in production:
```yaml
redis:
  # Remove ports entirely — only accessible within Docker network
  image: redis:7-alpine
  command: redis-server --appendonly yes --requirepass "${REDIS_PASSWORD}"
```

---

## Layer 7: Secrets Management

### Summary of All Secrets in the System

| Secret | Where Stored | Risk |
|---|---|---|
| `JWT_SECRET` | Env var in docker-compose | 🔴 Weak default, must be changed |
| `HMAC_SECRET` | Env var in docker-compose | 🔴 Weak default, must be changed |
| `CCMUX_DEVICE_TOKEN` | `.env.agent` on user's machine | 🟡 Plaintext file |
| User passwords | PostgreSQL as bcrypt hash | ✅ Safe |
| Refresh tokens | PostgreSQL as SHA-256 hash | ✅ Safe |
| Device tokens | PostgreSQL as HMAC-SHA256 hash | ✅ Safe |
| Firebase service account | `firebase-key.json` mounted into container | ⚠️ Sensitive file |
| Redis scrollback | Redis RAM + AOF file | ⚠️ Contains PTY output |

### 🔴 `firebase-key.json` In the Repo Root

```yaml
# docker-compose.yml
volumes:
  - ./firebase-key.json:/etc/firebase-key.json:ro
```

`firebase-key.json` is a **Google service account private key**. It must not be committed to git.

Check immediately:
```bash
git log --all --full-history -- firebase-key.json
```

If it was ever committed, rotate the key immediately in Google Cloud Console.

**Fix:**
```gitignore
# .gitignore — should already have this
firebase-key.json
```

---

## Prioritized Fix List

| Priority | Issue | Effort | Impact |
|---|---|---|---|
| 🔴 P1 | Redis exposed on host port 6379 | 5 min | Prevents server compromise |
| 🔴 P1 | Add Redis `requirepass` auth | 10 min | Prevents Redis hijack |
| 🔴 P1 | Change default JWT_SECRET / HMAC_SECRET | 5 min | Prevents token forgery |
| 🔴 P1 | Set up TLS (Caddy) | 30 min | Encrypts all traffic |
| 🔴 P1 | Verify `firebase-key.json` not in git history | 5 min | Prevents FCM abuse |
| 🟡 P2 | Fix X-Forwarded-For spoofing in rate limiter | 1 hour | Prevents rate limit bypass |
| 🟡 P2 | Add per-email login lockout | 2 hours | Prevents credential stuffing |
| 🟡 P2 | Restrict WebSocket CheckOrigin | 15 min | Prevents browser-based WS abuse |
| 🟡 P2 | Set `.env.agent` file permissions to 600 | Script fix | Limits local exposure |
| 🟠 P3 | Switch JWT from HS256 to RS256 | 2 hours | Asymmetric signing is more robust |
| 🟠 P3 | Add security headers in Caddy | 30 min | Defense in depth |
| 🟠 P3 | Audit logging for session spawn/kill/access | 3 hours | Forensics capability |

---

## What Is Already Secure (Summary)

| ✅ | Why It's Good |
|---|---|
| bcrypt cost 12 for passwords | Industry-standard, computationally expensive to crack |
| JWT signing method check | Prevents the `alg:none` attack |
| 15-minute JWT expiry | Short window for token replay |
| Refresh token stored as hash, revocable | Can't be stolen from DB and replayed |
| Device token HMAC-SHA256, hash-only in DB | Raw token never stored |
| 15-second WS auth deadline | Prevents idle connection attacks |
| session→device→user ownership check on every WS operation | The critical IDOR check is in place |
| Ownership check on REST endpoints | Consistent authorization model |
| Separate rate limits for auth vs. general API | Auth brute-force slowed significantly |
| chi `Recoverer` middleware | No panic stack traces to clients |
| Session IDs from `crypto/rand` | Not guessable |
