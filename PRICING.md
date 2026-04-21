# ccmux — Pricing, Infrastructure & Scalability

> Last updated: 2026-04-21  
> Covers: resource cost per session, recommended plan limits, retail pricing, Upstash Redis evaluation, and traffic-spike resilience.

---

## 1. What Counts as a "Session"?

One **session** = one PTY process running on a user's computer, streamed via the ccmux relay backend.

Per session, the backend holds:

| Resource | What it is |
|---|---|
| 1 **Redis Stream key** | `session:{id}:scrollback` — raw PTY output, MAXLEN 50k, TTL 24h |
| 1 **Redis Hash key** | `session:{id}:state` — cols/rows/status, ~200 bytes |
| 1 row in **PostgreSQL** `terminal_sessions` | Session metadata, ~200 bytes |
| 1 write every 30 s | `TouchSession` last_activity heartbeat |
| 1 active **WebSocket** per viewing client | Held in-memory by the Hub; zero DB overhead on hot path |

---

## 2. Resource Cost Per Session (Deep Dive)

### 2.1 PostgreSQL

PostgreSQL usage is **almost entirely fixed cost**, not per-session variable cost, because:

- The backend uses a **pgx connection pool** (default ~25 connections shared across all sessions).
- The table rows are tiny (~200 bytes each).
- Writes happen only on session start/end and every-30s heartbeat.

```
100,000 session rows  ≈  20 MB total table size
1 million rows        ≈  200 MB
```

A basic **1 vCPU / 1 GB Postgres** instance handles millions of rows and hundreds of concurrent pool connections with ease. PostgreSQL is essentially **free to scale** for this workload.

### 2.2 Redis (the real bottleneck)

Redis is the dominant per-session resource because every active session owns a live **Redis Stream** for scrollback:

| Session Activity | Entries Stored | RAM Used |
|---|---|---|
| Idle / barely used | ~200 entries | ~20 KB |
| Moderate (bash, editing) | ~5,000 entries | ~1 MB |
| Busy (build output, logs) | ~50,000 entries (max) | ~10–25 MB |
| **Typical average** | **~3,000 entries** | **~1–2 MB** |

> **Design note:** Your architecture already caps streams at MAXLEN 50k and TTL 24h, which is the right call. These limits prevent runaway memory.

---

## 3. Session Limit Recommendations

### Based on CX21 VPS (~$5.50/mo, 4 GB RAM)

| Service | RAM Allocation |
|---|---|
| Backend (Go binary) | 256 MB |
| PostgreSQL | 512 MB |
| Redis | **1,500 MB** |
| Caddy / OS overhead | 256 MB |
| Safety buffer | 1,500 MB |

```
Redis budget:   1,500 MB
Per session:    ~2 MB average
Max concurrent: ~750 sessions
```

At realistic utilization (~20–30%), one CX21 comfortably serves **~150–250 active Pro users**.

### Recommended Plan Tiers

| Plan | Sessions | Devices | Price |
|---|---|---|---|
| **Free** | **3** | 1 | $0 |
| **Pro** | **20** | 5 | **$4.99 / mo** |
| **Team** | **100** | Unlimited | **$14.99 / mo** |

**Why 20 for Pro?**  
A developer rarely runs >10 active terminals simultaneously. 20 is generous and predictable:
- 20 sessions × 2 MB = **40 MB Redis RAM per fully active Pro user**
- One CX21 can serve **~37 fully-saturated Pro users**, or **~150 real-world Pro users**

### Enforce Limits in Code

Add a `plan` column to `users` and check count before spawning:

```sql
-- Migration: add plan tracking
ALTER TABLE users ADD COLUMN plan TEXT NOT NULL DEFAULT 'free';
-- values: 'free' | 'pro' | 'team'
```

```go
// backend/internal/api/session_handler.go
const (
    MaxSessionsFree = 3
    MaxSessionsPro  = 20
    MaxSessionsTeam = 100
)

count, _ := store.CountActiveSessionsByUser(ctx, userID)
if count >= plan.MaxSessions {
    http.Error(w, "session limit reached for your plan", http.StatusForbidden)
    return
}
```

---

## 4. Retail Pricing

> Rule of thumb: price at **10–25× your marginal infrastructure cost** to cover support, bandwidth, push notification quota, and profit margin.

| Metric | Pro Plan |
|---|---|
| Revenue per user | $4.99 / mo |
| Your infra cost per user | ~$0.04–0.10 / mo |
| **Gross margin** | **~98%** |
| Break-even | **2 paying Pro users** cover your CX21 VPS cost |

### Annual Discount (recommended)

| Plan | Monthly | Annual | Effective Monthly |
|---|---|---|---|
| Pro | $4.99 | $39 / yr | $3.25 |
| Team | $14.99 | $119 / yr | $9.99 |

### ⚠️ App Store Cut Warning

If you charge via **iOS App Store or Google Play in-app purchase**, Apple/Google take **30%** (15% for first-year small business).

| Channel | You receive at $4.99 |
|---|---|
| App Store IAP | ~$3.49 |
| Google Play IAP | ~$3.49 |
| **Stripe web checkout** | **~$4.84** (Stripe 2.9% + $0.30) |

**Recommendation:** Use **Stripe web checkout** as primary billing. Direct users to your website to subscribe. This is legal and is the standard approach for SaaS productivity apps (Notion, Linear, etc. all do this).

---

## 5. Upstash Redis Evaluation

### What is Upstash?

Upstash is a **serverless Redis** provider — you pay per command (request) rather than reserving a fixed server. It scales to zero when idle and up automatically.

### Upstash Pricing Plans (as of 2026-04)

| Plan | Price | Data Limit | Commands | Notes |
|---|---|---|---|---|
| **Free** | $0 | 256 MB | 500K / month | Good for dev/testing only |
| **Pay As You Go** | $0.20 / 100K commands | 100 GB | Unlimited | Scales with usage |
| **Fixed 250 MB** | $10 / mo | 250 MB | Unlimited | No per-command charge |
| **Fixed 1 GB** | ~$20 / mo | 1 GB | Unlimited | — |
| **Fixed 5 GB** | ~$50 / mo | 5 GB | Unlimited | — |
| **Fixed 10 GB** | ~$100 / mo | 10 GB | Unlimited | — |

> Prod Pack add-on: SLA uptime, RBAC, encryption at rest, SOC-2, Prometheus/Datadog integration.

---

### 5.1 Is Upstash a Good Fit for ccmux?

**Short answer: Not ideal for ccmux's workload. Here's why:**

#### ✅ Pros of Upstash

| Pro | Detail |
|---|---|
| Zero ops | No Redis server to manage, patch, or monitor |
| Scales to zero | Free tier for development |
| Global replicas | Low-latency reads in multiple regions |
| SOC-2 / encryption | Useful when you need compliance |
| Auto-scales | Handles burst traffic for writes |

#### ❌ Cons / Concerns for ccmux

| Concern | Severity | Detail |
|---|---|---|
| **Per-command cost on scrollback** | 🔴 HIGH | ccmux does XADD to Redis Stream **on every 16ms PTY output batch**. At 60fps per session, that's ~3,750 writes/minute/session. 10 active sessions = 2.25M commands/hour. At $0.20/100K that's **$4.50/hour** just for scrollback! |
| **Latency** | 🟡 MEDIUM | Upstash is HTTP-based (REST) or uses managed TCP. Your backend does `go h.appendScrollback()` in a goroutine — the extra network hop to Upstash adds 5–50ms per write. Since it's async this doesn't break the hot path, but it accumulates. |
| **Stream MAXLEN enforcement** | 🟡 MEDIUM | Each XADD MAXLEN ~50k itself costs 1 command. Trim operations are also billed. |
| **XREAD for scrollback replay** | 🟡 MEDIUM | On client reconnect, `XREAD COUNT 50000` is 1 command BUT returns massive payload — still 1 billed command, so this is fine. |
| **Data size limits on lower plans** | 🟡 MEDIUM | Free tier 256 MB fits only ~128 idle sessions. Meaningful production needs Fixed 1GB ($20/mo) minimum. |
| **Not designed for high-frequency streaming** | 🔴 HIGH | Upstash is optimized for cache-style access patterns (get/set). Redis Streams with high-frequency XADD is exactly the anti-pattern they warn about. |

#### 💸 Upstash Cost Simulation for ccmux

```
Scenario: 50 active Pro users, each with 5 active sessions = 250 active sessions

Per session write rate: 1 XADD every 16ms (batched) = ~3,750 writes/minute
Per session per hour:   3,750 × 60 = 225,000 commands
250 sessions per hour:  56,250,000 commands

Daily (10h active use):  562,500,000 commands
Monthly:                 ~16.8 billion commands

Pay-As-You-Go cost: 16,800,000,000 ÷ 100,000 × $0.20 = $33,600/month 🚨
```

This is catastrophically expensive for a PTY stream workload. **Do not use Upstash Pay-As-You-Go for ccmux scrollback.**

#### What If You Still Want Upstash?

You can use Upstash only for **non-streaming use cases**:

| Use Case | Upstash? | Why |
|---|---|---|
| Scrollback (XADD/XREAD) | ❌ No | Too many commands per session |
| `session:{id}:state` hash | ⚠️ Caution | Moderate write rate (~1/30s per session), manageable |
| `agent:{device}:ping` TTL key | ✅ OK | 1 write per 15s per device, very low volume |
| Feature flags / config cache | ✅ OK | Classic cache pattern, Upstash excels here |
| Rate limiting (INCR) | ✅ OK | Low volume |

**Bottom line:** Use a **self-managed Redis** (on your VPS, or Hetzner Managed Redis) for scrollback streams. Upstash is not cost-effective for high-frequency streaming workloads.

---

### 5.2 Better Redis Alternatives

| Option | Monthly Cost | Best For |
|---|---|---|
| **Self-managed Redis on VPS** (your current setup) | $0 extra (bundled in VPS) | Phase 1 — best value |
| **Hetzner Managed Redis** | ~€9/mo (1 GB) | When you want ops-free but stay affordable |
| **Render Redis** | $10/mo (256 MB) | Simple managed option |
| **Railway Redis** | $5/mo flat | Dev/small prod |
| **AWS ElastiCache t4g.micro** | ~$13/mo | When already on AWS |
| **Upstash Fixed 1GB** | $20/mo | Only if you reduce XADD frequency significantly |

---

## 6. Handling Traffic Spikes

### 6.1 ccmux's Spike Profile

ccmux can experience two types of spikes:

| Type | Example | What Stresses |
|---|---|---|
| **User growth spike** | Featured on HN / ProductHunt → 500 sign-ups in 1 hour | PostgreSQL auth queries, HTTP API |
| **Session activity spike** | Many users start builds simultaneously | Redis writes, WebSocket Hub memory, network |

### 6.2 What the Current Architecture Handles Well

**Hot path is zero-I/O.** The Hub routes PTY output purely in memory:

```
PTY output → [in-memory Hub] → mobile clients
                ↓ (async goroutine, non-blocking)
            Redis XADD (scrollback)
```

This means:
- **1,000 concurrent sessions** generating output all hit only Go's in-memory broadcast — no DB, no Redis on the critical path
- Redis/PG spikes never directly stall mobile clients
- The Go binary easily handles **50,000+ concurrent WebSocket connections** on a single instance (goroutine per WS, ~8 KB stack each = ~400 MB for 50k)

### 6.3 Bottlenecks Under a Spike

| Component | Spike Behavior | Risk Level |
|---|---|---|
| **Go backend** (in-memory Hub) | Goroutines are cheap; 10k WS conns ≈ 80 MB RAM | 🟢 Low |
| **PostgreSQL** (register/login) | Sign-up spike → many INSERT + bcrypt hashes | 🟡 Medium |
| **Redis** (XADD scrollback) | Many sessions writing simultaneously | 🟡 Medium |
| **VPS network bandwidth** | PTY output × active sessions × clients | 🟠 Medium-High |
| **Single VPS availability** | No redundancy — if it goes down, all users affected | 🔴 High |

### 6.4 bcrypt is Your Hidden Bottleneck

Your `auth/password.go` uses **bcrypt cost 12**. Each login/register costs:
```
bcrypt cost 12 ≈ 250ms of CPU per hash
10 registrations/second = 2.5 CPU cores fully saturated
```

On a 2-vCPU CX21, a registration spike of >8 req/s will cause queuing. **This is the first thing that breaks.**

**Fix:** Add a concurrency limiter for auth endpoints:

```go
// backend/internal/api/middleware/auth_limiter.go
var bcryptSem = make(chan struct{}, 4) // max 4 concurrent bcrypt ops

func BcryptLimiter(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        bcryptSem <- struct{}{}
        defer func() { <-bcryptSem }()
        next.ServeHTTP(w, r)
    })
}
```

This gracefully queues auth requests instead of thrashing the CPU.

### 6.5 Scale-Up Triggers & Actions

| Trigger | Action | Cost Impact |
|---|---|---|
| >150 Pro users sustained | Upgrade VPS: CX21 → CX32 (4 vCPU, 8 GB, ~$14/mo) | +$8.50/mo |
| >500 Pro users sustained | Upgrade to CX42 (8 vCPU, 16 GB, ~$28/mo) + separate managed Redis | +$28/mo |
| >2,000 users or HN spike | Enable `MULTI_INSTANCE_MODE=true`, add 2nd backend replica, put Redis on dedicated instance | Scales horizontally |
| Redis hits 70% of RAM | Add Redis MAXMEMORY policy `allkeys-lru` or increase VPS RAM | — |

### 6.6 Handling a "ProductHunt Spike" (0→500 users in 1 hour)

The most likely real-world stress event. Here's what actually happens:

```
500 new sign-ups in 1 hour = ~8.3 registrations/minute = 0.14 req/s

bcrypt 250ms each → 0.14 × 0.25 = 3.5% CPU  (totally fine on CX21)
Postgres INSERTs  → negligible
Redis             → 0 (new users don't have sessions yet)
```

Even a 10× spike (5,000 sign-ups/hour = 1.4 req/s) is **completely fine** on CX21:
```
1.4 req/s × 250ms = 35% of 1 CPU core
```

The real spike risk is **all 500 users connecting sessions simultaneously**:
```
500 users × 5 sessions = 2,500 active sessions
2,500 sessions × 2 MB Redis = 5,000 MB Redis → exceeds CX21 Redis budget!
```

**Mitigation:** The rate limiter (already in your `ratelimit.go`) prevents abuse, and free users are capped at 3 sessions. A ProductHunt spike of mostly Free users:
```
500 Free users × 3 sessions × 20 KB idle = 30 MB Redis → completely fine
```

### 6.7 Multi-Instance Mode (Already Built In)

Your architecture already supports horizontal scaling with **zero code changes**:

```bash
# On a second VPS, just run the backend with:
MULTI_INSTANCE_MODE=true
REDIS_URL=redis://your-shared-redis:6379
```

```
Load Balancer (sticky sessions via cookie)
├── backend-1 (in-memory Hub A)
├── backend-2 (in-memory Hub B)
└── backend-3 (in-memory Hub C)
         │
    Redis pub/sub bridges Hub A ↔ B ↔ C
         │
    Shared PostgreSQL (or RDS/Supabase)
```

This gives you **elastic horizontal scaling** — just add VPS nodes when load grows.

---

## 7. Recommended Infrastructure Roadmap

| Phase | Users | Infrastructure | Monthly Cost |
|---|---|---|---|
| **Phase 1** (now) | 0–150 | Hetzner CX21: all-in-one (backend + PG + Redis) | **~$5.50** |
| **Phase 2** | 150–500 | Hetzner CX32 (4 vCPU, 8 GB) | **~$14** |
| **Phase 3** | 500–2,000 | CX42 + Hetzner Managed Redis (~€9) | **~$37** |
| **Phase 4** | 2,000+ | 2× CX32 backend + managed PG (Supabase $25) + managed Redis | **~$80–120** |
| **Enterprise** | 10,000+ | Kubernetes / Fly.io autoscale + ElastiCache + RDS | **$300–600** |

> At 2,000 Pro users × $4.99 = **$9,980 MRR** — infrastructure is still only 1.2% of revenue.

---

## 8. Summary Cheat Sheet

| Decision | Recommendation |
|---|---|
| Free plan sessions | **3 sessions, 1 device** |
| Pro plan sessions | **20 sessions, 5 devices — $4.99/mo** |
| Team plan sessions | **100 sessions, unlimited devices — $14.99/mo** |
| Use Upstash for scrollback? | **❌ No** — too expensive for high-frequency XADD |
| Use Upstash for TTL/rate limit? | **✅ Yes** — low command volume use cases OK |
| Best Redis for Phase 1 | **Self-managed on same VPS** (free) |
| Best Redis for Phase 3+ | **Hetzner Managed Redis** (~€9/mo, ops-free) |
| Spike resilience | **Strong on hot path** (in-memory Hub); add bcrypt concurrency limiter for auth |
| Scale trigger | >150 Pro users → upgrade VPS; >500 users → `MULTI_INSTANCE_MODE=true` |
| Billing channel | **Stripe web checkout** (avoid 30% App Store cut) |
| Break-even | **2 Pro subscribers** cover CX21 server cost |
