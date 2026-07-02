# TradeFlow

A distributed real-time market data backend in Go. Ingests live Binance price feeds across 300 instruments, runs 6 streaming analytics engines, and pushes indicator updates to authenticated WebSocket consumers through a two-server split architecture.

---

## Demo

<!-- Drop the video link below (e.g. an uploaded .mp4 in the repo, a GitHub asset URL, or a YouTube link) -->

📹 **[Watch the demo](PASTE_VIDEO_LINK_HERE)**

---

## Architecture

```
Binance WebSocket Feed (300 USDT pairs)
              │
              ▼
     Background Worker
              │
       Indicator Registry
              │
   ┌──────────┼──────────┐
  SMA  EMA  OHLC  BB  RSI  MACD
              │
        Data Server :8081
        · Runs all engines
        · Writes Redis sorted sets + Postgres
        · Publishes to analytics:events channel
              │
         Redis Pub/Sub
              │
       Client Server :8080
       · /ws  — authenticated WebSocket (push + pull)
       · /history — REST history bursts
       · /login /logout /subscribe /subscriptions
```

Two binaries, cleanly separated. `dataserver` owns all computation. `clientserver` owns all client-facing I/O. Neither knows about the other's internals — Redis pub/sub is the only coupling.

---

## Redis — 2 Pairs + Load Balancer

```
pair1:  primary :6381   replica :6380   sentinel :26380   name "mymaster"
pair2:  primary :6383   replica :6382   sentinel :26381   name "mymaster2"
```

Three mechanisms stack together:

| Layer | What it does |
|---|---|
| Sentinel ×2 | Failover — detects primary death, auto-promotes replica |
| Load Balancer | Routing — writes go to least-loaded healthy primary |
| Scatter-Gather | Speed — reads fan out to both replicas concurrently, fastest wins |

If both replicas fail, reads fall back to the write primary. If one pair is saturated or down, all traffic reroutes to the other. Disaster recovery validated live with zero consumer disconnections under load.

**Session storage** is pinned to `pair2Primary (:6383)` directly — bypasses the LB entirely to guarantee read-your-own-writes consistency for auth tokens.

---

## Analytics Engines

| Engine | Params | Resolutions | Storage |
|---|---|---|---|
| SMA | window=20 | 1s + 1m | Redis + Postgres |
| EMA | window=20 | 1s + 1m | Redis + Postgres |
| RSI | period=14, Wilder's smoothing | 1s + 1m | Redis + Postgres |
| MACD | fast=12, slow=26, signal=9 | 1s + 1m | Redis + Postgres |
| Bollinger Bands | window=20, k=2.0 | 1m | Redis + Postgres |
| OHLC | — | 1m | Redis + Postgres |

All engines are registered in the indicator registry and picked up by the background worker automatically. Adding a new engine requires no changes to the worker, registry, or routing layer.

---

## History Layer

| Tier | Redis Key Pattern | Granularity | TTL |
|---|---|---|---|
| Warm cache | `hist:1m:{indicator}:{id}` | 1 min | 48h |
| Hourly rollup | `hist:1h:{indicator}:{id}` | 1 hour | 7d |
| Cold fallback | Postgres | 1 min | unlimited |

On room join, client receives a history burst: last 1800 × 1s points + 60 × 1m points for all 6 indicators. Hourly rollup job averages 60 1m-buckets into one 1h-point (OHLC uses proper open/high/low/close merge). On login, a background warmer pre-loads Redis from Postgres for all subscribed instruments — non-blocking, fire-and-forget.

---

## Session & User Management

Authentication is token-based. Passwords are checked against Postgres. On success, a UUID session token is generated and stored in Redis with a 24h TTL.

```
POST /login        → validates credentials, creates session token, returns token + subscriptions
POST /logout       → deletes token from Redis immediately (invalidates session)
POST /subscribe    → requires Bearer token, resolves client_id from session
POST /unsubscribe  → requires Bearer token
GET  /subscriptions → requires Bearer token
WS   /ws?token=… → token validated before WebSocket upgrade; rejected with 4001 if invalid
```

An `AuthMiddleware` wraps all protected routes — it extracts the `Authorization: Bearer <token>` header, validates against Redis, injects `clientID` into request context. Handlers never trust client-supplied identity. WebSocket connections carry the token as a query param; invalid tokens get HTTP 401 before the upgrade completes.

---

## WebSocket Protocol

**Push model** — join a room, receive all 6 indicator updates for that instrument:
```json
{ "type": "join", "room": "101" }
```

**Pull model** — subscribe to a specific indicator topic:
```json
{ "type": "subscribe",   "topic": "rsi:101" }
{ "type": "unsubscribe", "topic": "rsi:101" }
```

Both models coexist on the same connection. History bursts fire automatically on join.

---

## Frontend — TradeFlow Dashboard

Single-file vanilla JS dashboard (`frontend/index.html`). Dark navy theme, IBM Plex Mono.

- **Market Watch sidebar** — all 25 instruments with live prices, joins all 25 rooms on connect
- **Instrument detail** — gated behind subscription; shows candlestick chart + BB overlay + RSI/MACD combo chart + 4 indicator cards
- **Charts** — LightweightCharts v4.1.3; per-instrument cache survives instrument switches; crosshair sync between price and combo chart
- **Login/logout** — token stored in memory, attached to every fetch and WebSocket URL
- **WS reconnect** — auto-reconnects on drop; skips reconnect on server-sent close code 4001 (auth rejection), forces logout instead

---

## Postgres Schema

```sql
CREATE TABLE clients (
    id SERIAL PRIMARY KEY,
    username TEXT UNIQUE,
    password TEXT
);

CREATE TABLE client_subscriptions (
    id SERIAL PRIMARY KEY,
    client_id INT REFERENCES clients(id),
    instrument_id INT,
    UNIQUE(client_id, instrument_id)
);

-- Analytics tables (same shape for sma, ema, rsi)
CREATE TABLE sma  (time TIMESTAMPTZ, instrument INT, resolution TEXT, value DOUBLE PRECISION);
CREATE TABLE ema  (time TIMESTAMPTZ, instrument INT, resolution TEXT, value DOUBLE PRECISION);
CREATE TABLE rsi  (time TIMESTAMPTZ, instrument INT, resolution TEXT, value DOUBLE PRECISION);

-- OHLC
CREATE TABLE ohlc (time TIMESTAMPTZ, instrument INT, resolution TEXT,
    open DOUBLE PRECISION, high DOUBLE PRECISION,
    low  DOUBLE PRECISION, close DOUBLE PRECISION);

-- Multi-value indicators
CREATE TABLE bb   (time TIMESTAMPTZ, instrument INT, resolution TEXT,
    upper DOUBLE PRECISION, middle DOUBLE PRECISION, lower DOUBLE PRECISION);
CREATE TABLE macd (time TIMESTAMPTZ, instrument INT, resolution TEXT,
    macd_line DOUBLE PRECISION, signal_line DOUBLE PRECISION, histogram DOUBLE PRECISION);
```

---

## Nginx

Routes traffic across two clientserver instances using `least_conn` (critical for long-lived WS connections). Dataserver metrics use round-robin.

```
/ws            → clientservers (least_conn, WS upgrade headers)
/history       → clientservers
/login         → clientservers
/logout        → clientservers
/subscribe     → clientservers
/unsubscribe   → clientservers
/subscriptions → clientservers
/metrics       → dataservers (round-robin)
/health        → nginx responds 200 directly
```

CORS is handled entirely in Go — Nginx CORS headers are commented out to prevent double-header conflicts.

---

## Load Test Results

| Phase | Clients | Instruments | Messages | Drops | Avg Latency | Heap |
|---|---|---|---|---|---|---|
| 1 | 200 | 300 | 310,000 | 0 | ~63ms | ~10MB |
| 2 | 500 | 300 | 776,000 | 0 | ~100ms | ~10MB |

Zero drops across both phases. Heap stable regardless of client count.

---

## CI/CD Pipeline

### Overview

The pipeline is split into two GitHub Actions workflows:

- **CI** (`.github/workflows/ci.yml`) — runs on every push to any branch
- **Deploy** (`.github/workflows/deploy.yml`) — runs on push to `main` only

### CI Workflow

Runs on `ubuntu-latest`. Steps:

1. Checkout code
2. Set up Go 1.25.5 (matches `go.mod` — required by `golang.org/x/sync`, `golang.org/x/text`, `golang.org/x/mod` which all declare `go 1.25.0` as minimum)
3. Build `dataserver` — `go build ./cmd/dataserver/...`
4. Build `clientserver` — `go build ./cmd/clientserver/...`
5. Run `go vet ./...`
6. Run `golangci-lint` v1.64.8
7. Build both Docker images to verify Dockerfiles are valid

### Deploy Workflow

Runs on push to `main` only. Steps:

1. Checkout code
2. Disable macOS keychain credential store (sets `credsStore` to empty in `~/.docker/config.json`)
3. Log in to GHCR using `GHCR_TOKEN` and `GHCR_USER` secrets
4. Build and push `trradeflow-dataserver` image to GHCR (tagged with commit SHA + `latest`)
5. Build and push `trradeflow-clientserver` image to GHCR (tagged with commit SHA + `latest`)
6. SSH into deployment server and run `docker compose pull && docker compose up -d`

### Required GitHub Secrets

| Secret | Description |
|--------|-------------|
| `GHCR_TOKEN` | GitHub Personal Access Token with `write:packages`, `read:packages` scopes |
| `GHCR_USER` | GitHub username (e.g. `riyansh100`) |
| `SSH_HOST` | Public IP of the deployment server |
| `SSH_USER` | SSH username on the deployment server |
| `SSH_PORT` | SSH port on the deployment server |
| `SSH_PRIVATE_KEY` | Private key corresponding to the server's authorized public key |
| `DEPLOY_PATH` | Path on the server where `docker-compose.yml` lives |

### Go Version Note

`go.mod` declares `go 1.25.5`. This is required by transitive dependencies (`golang.org/x/sync@v0.20.0`, `golang.org/x/text@v0.36.0`). Both the CI workflow and the Dockerfiles must use `golang:1.25-alpine` — using 1.24 causes `golangci-lint` typecheck failures across every package.

### Deployment Target

The deploy workflow SSHes into a server and runs `docker compose pull && docker compose up -d`. The server must have Docker and Docker Compose installed. Recommended providers:

- **Oracle Cloud Free Tier** — permanently free, 4 CPU / 24GB RAM (Ampere A1)
- **Hetzner CAX11** — €3.29/month, 2 CPU / 4GB RAM

A local machine behind a home or office network is not suitable — corporate networks block inbound connections and prevent the required port forwarding.

---

## Running — Mac (backend)

```bash
# Redis (6 terminals)
redis-server redis-primary.conf              # pair1 primary :6381
redis-server redis-replica.conf              # pair1 replica :6380
redis-server sentinel.conf --sentinel        # sentinel1 :26380
redis-server redis-pair2-primary.conf        # pair2 primary :6383
redis-server redis-pair2-replica.conf        # pair2 replica :6382
redis-server redis-sentinel2.conf --sentinel # sentinel2 :26381

# Nginx
sudo nginx -c /path/to/chat-backend/nginx.conf

# Servers
go run ./cmd/dataserver/main.go
go run ./cmd/clientserver/main.go
```

## Running — Windows (frontend)

Open `frontend/index.html` directly in browser. Set `API_BASE` and `WS_BASE` in the script block to point at the Mac's local IP.

---

## Endpoints

```
WS   /ws?token=<token>     Authenticated push + pull WebSocket
WS   /ws/ingest            Ingestor WebSocket (internal)
GET  /history              ?instrument=101&indicator=sma&hours=3&resolution=1m
POST /login                { username, password } → { token, id, username, subscriptions }
POST /logout               Bearer token required
POST /subscribe            Bearer token + { instrument_id }
POST /unsubscribe          Bearer token + { instrument_id }
GET  /subscriptions        Bearer token → { client_id, subscriptions }
GET  /metrics              Engine backlogs, heap, goroutines, drop rate
GET  /health               200 ok
```

---

## Tech Stack

Go · Redis (Sorted Sets, Pub/Sub, Sentinel) · Postgres (pgx/v5) · gorilla/websocket · Ristretto (L1 cache) · nginx (least_conn WS proxy) · LightweightCharts v4.1.3 · Docker · GitHub Actions (CI/CD)

---

## Roadmap

- TimescaleDB migration (hypertables for analytics storage)
- Horizontal hub sharding
- ATR + Stochastic indicators
- Event replay via Redis Streams or NATS JetStream
