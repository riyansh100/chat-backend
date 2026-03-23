# Distributed Market Data Event Backbone (Go)

A **real-time WebSocket event routing backbone** built in Go for distributing and processing live market data streams.

This system is **not a chat application and not a frontend system**. It is designed as a **backend event infrastructure prototype**, focusing on **deterministic routing, concurrency correctness, and streaming analytics**.

All interaction is performed via Go services and terminal-based WebSocket clients.

---

# System Overview

The system ingests real-time market trade data from Binance, normalizes it into canonical domain events, and distributes those events to subscribed consumers.

In addition to routing raw events, the backbone supports **stream analytics** via six indicator engines running in parallel, and exposes both a **push model** (room-based broadcast) and a **pull model** (topic-based subscription) for downstream consumers.

A **two-server architecture** separates data computation from client delivery:

```
Binance Streams
       │
       ▼
Background Worker (direct feed)
       │
       ▼
Indicator Registry
       │
  ┌────┼─────┬──────┬──────┐
  │    │     │      │      │
 SMA  EMA  OHLC    BB    RSI   MACD
  │    │     │      │      │
  └────┼─────┴──────┴──────┘
       │
       ▼
  Data Server (port 8081)
  - Runs all engines
  - Writes to Redis + Postgres
  - Publishes to analytics:events
       │
       ▼ Redis pub/sub (analytics:events)
       │
  Client Server (port 8080)
  - Serves /ws (push + pull)
  - Serves /history (REST)
  - No engine logic
       │
  ┌────┴──────────────────┐
  │                       │
Push Model             Pull Model
(Room Broadcast)       (Topic Subscription)
  │                       │
  ▼                       ▼
Clients               Clients
```

The Hub remains **routing-only** and does not contain domain computation logic.

---

# Two-Server Architecture

## Data Server (`cmd/dataserver`)

Runs all analytics engines. Never serves WebSocket clients directly.

Responsibilities:
- Connect to Binance and feed all 6 engines via the indicator registry
- Write per-minute values to Redis (`sma:1s:*`, `ema:1m:*`, etc.) and Postgres
- Write to history Redis namespace (`hist:1m:*`) for warm-start
- Run hourly rollup job — averages 60×1m points into `hist:1h:*`
- Publish every indicator update to Redis `analytics:events` channel
- Expose `/metrics` endpoint

```powershell
$env:FEED_SOURCE="binance"; go run cmd/dataserver/main.go
```

## Client Server (`cmd/clientserver`)

Serves all client traffic. Runs no engines.

Responsibilities:
- Subscribe to `analytics:events` from data server via Redis pub/sub
- Fan out received updates to connected WebSocket clients (push + pull)
- Serve `/ws` and `/ws/ingest` WebSocket endpoints
- Serve `/history` REST endpoint

```powershell
go run cmd/clientserver/main.go
```

---

# Key Design Principles

### Single Writer State Ownership

Each server instance runs a **single Hub goroutine** that owns all routing state.

This ensures:
- No concurrent map writes
- No routing mutexes
- Deterministic state transitions

All state mutations occur via channels.

### Domain / Infrastructure Separation

| Layer | Responsibility |
|---|---|
| Feed Adapter | Exchange ingestion |
| Background Worker | Direct engine feeding, source agnostic |
| Indicator Registry | Fan-out tick to all registered engines |
| Hub | Event routing |
| Domain | Market data semantics |
| Analytics | Streaming computation (SMA, EMA, OHLC, BB, RSI, MACD) |
| History | hist:1m / hist:1h Redis namespace + hourly rollup |
| Redis | Cross-instance infrastructure + history |
| Postgres | Durable analytics data lake |

### Payload Agnostic Routing

The Hub does not interpret event payloads. It routes based on room / instrument ID. This allows the backbone to support any event type without modification.

---

# Analytics Engines

Six engines run concurrently, each processing all 20 instruments independently.

### SMA — Simple Moving Average
```
SMA = average of last N prices
Window = 20, Resolutions: 1s + 1m
```

### EMA — Exponential Moving Average
```
EMA_t = price × k + EMA_{t-1} × (1 - k)
k = 2 / (window + 1), window = 20
Resolutions: 1s + 1m
```

### OHLC — Open / High / Low / Close Candles
```
1-minute candles, flush at minute boundary
Resolution: 1m
```

### Bollinger Bands
```
Upper = SMA + 2×StdDev
Middle = SMA(20)
Lower = SMA - 2×StdDev
Resolution: 1m
```

### RSI — Relative Strength Index
```
RSI = 100 - (100 / (1 + RS))
RS = avgGain / avgLoss, Period = 14, Wilder's smoothing
Resolutions: 1s + 1m
```

### MACD — Moving Average Convergence Divergence
```
MACD Line  = EMA(12) - EMA(26)
Signal     = EMA(9) of MACD Line
Histogram  = MACD Line - Signal
Resolutions: 1s + 1m
```

All engines share the same architecture:
- Single goroutine, ticker-based flush
- Buffered input channel (cap 1024)
- Non-blocking fan-out to output channels
- Registered via Feeder interface — worker requires zero changes to add a new engine

---

# Indicator Registry

A lightweight fan-out abstraction over all analytics engines.

```go
type Feeder interface {
    Input() chan<- PriceUpdateEvent
    InputLen() int
}
```

The background worker calls `registry.Feed(tick)` once per price event. The registry fans it out to every registered engine non-blocking.

**Adding a new indicator requires:**
1. Implement `Input()` and `InputLen()` on your engine
2. Register in `hub_init.go`
3. Wire its typed output channel for broadcast + persistence

The worker requires **zero changes**.

---

# History Layer

Three tiers of data access per indicator per instrument:

| Tier | Source | Granularity | Retention | Use Case |
|---|---|---|---|---|
| Realtime | WebSocket | Per tick | Live | Live streaming consumers |
| Warm cache | Redis `hist:1m` | 1 minute | 48h | On-join history burst |
| Hourly rollup | Redis `hist:1h` | 1 hour | 7 days | Long-range chart data |
| Cold storage | Postgres | 1 minute | Unlimited | Fallback + analytics |

### How it works

Every time an engine flushes a 1m value, the data server writes it to `hist:1m:{indicator}:{instrument}` in Redis. Every hour a rollup job runs and:
- Reads last 60 entries from `hist:1m`
- Averages them (SMA, EMA, RSI, BB, MACD) or merges them into a proper 1h candle (OHLC)
- Writes result to `hist:1h:{indicator}:{instrument}`

### REST endpoint

```
GET /history?instrument=101&indicator=sma&hours=3&resolution=1m
GET /history?instrument=101&indicator=macd&hours=6&resolution=1h
```

Parameters:

| Param | Required | Values | Default |
|---|---|---|---|
| instrument | yes | 101–120 | - |
| indicator | yes | sma, ema, rsi, macd, bb, ohlc | - |
| hours | no | 1–168 | 3 |
| resolution | no | 1m, 1h | 1m |

Reads from Redis first, falls back to Postgres on cache miss.

---

# Persistence Layer

| Store | Redis Key Pattern | Postgres Table | Retention |
|---|---|---|---|
| SMA | `sma:1s:{id}`, `sma:1m:{id}` | `sma` | 60m (1s), 24h (1m) |
| EMA | `ema:1s:{id}`, `ema:1m:{id}` | `ema` | 60m (1s), 24h (1m) |
| OHLC | `ohlc:1m:{id}` | `ohlc` | 24h |
| BB | `bb:1m:{id}` | `bb` | 24h |
| RSI | `rsi:1s:{id}`, `rsi:1m:{id}` | `rsi` | 60m (1s), 24h (1m) |
| MACD | `macd:1s:{id}`, `macd:1m:{id}` | `macd` | 60m (1s), 24h (1m) |
| History 1m | `hist:1m:{indicator}:{id}` | - | 48h |
| History 1h | `hist:1h:{indicator}:{id}` | - | 7 days |

On room join, the client server immediately delivers history bursts:

```
sma_history   — 1800×1s + 60×1m
ema_history   — 1800×1s + 60×1m
ohlc_history  — 60×1m candles
bb_history    — 60×1m bands
rsi_history   — 1800×1s + 60×1m
macd_history  — 1800×1s + 60×1m
```

---

# Push Model (Room Broadcast)

Client joins a room and receives all indicator updates for that instrument.

```json
{ "type": "join", "room": "101" }
```

Client receives:
```
price_update
sma_update    (1s + 1m)
ema_update    (1s + 1m)
ohlc_update   (1m)
bb_update     (1m)
rsi_update    (1s + 1m)
macd_update   (1s + 1m)
+ history burst on join for all indicators
```

---

# Pull Model (Topic Subscription)

Fine-grained subscription to a specific indicator + instrument combination. Lower latency, no history burst.

```json
{ "type": "subscribe",   "topic": "rsi:101" }
{ "type": "unsubscribe", "topic": "rsi:101" }
```

Valid topic format: `{indicator}:{instrument_id}`

```
sma:101    ema:102    ohlc:103
bb:101     rsi:102    macd:103
```

Both models coexist. A client can join rooms and subscribe to topics simultaneously.

---

# Trading Domain

| Instrument | ID | Instrument | ID |
|---|---|---|---|
| BTC_USDT | 101 | AVAX_USDT | 111 |
| ETH_USDT | 102 | LINK_USDT | 112 |
| BNB_USDT | 103 | UNI_USDT | 113 |
| XRP_USDT | 104 | ATOM_USDT | 114 |
| SOL_USDT | 105 | TRX_USDT | 115 |
| ADA_USDT | 106 | ETC_USDT | 116 |
| DOGE_USDT | 107 | FIL_USDT | 117 |
| MATIC_USDT | 108 | ICP_USDT | 118 |
| LTC_USDT | 109 | APT_USDT | 119 |
| DOT_USDT | 110 | ARB_USDT | 120 |

---

# Feed Source

```
FEED_SOURCE=binance   (default) — live Binance multi-stream, 20 instruments
FEED_SOURCE=mock      — deterministic fake prices for all 20 instruments
```

---

# Redis Integration

| Usage | Purpose |
|---|---|
| `analytics:events` pub/sub | Data server → client server real-time fan-out |
| `chat:events` pub/sub | Cross-instance Hub fan-out |
| Sorted sets `sma:1s:*` etc. | Per-engine warm-start on join |
| Sorted sets `hist:1m:*` | Rolling 48h history per indicator |
| Sorted sets `hist:1h:*` | Rolling 7d hourly rollup per indicator |
| KV `last_price:*` | Last price per instrument for L1 warm-start |

Failure mode: system degrades to single-instance mode, event routing continues uninterrupted.

---

# Caching Strategy

### L1 Cache (In-Memory)
Ristretto. Stores last price per instrument. Owned exclusively by the Hub.

### L2 Cache (Redis)
Warm-start recovery and cross-instance visibility. Not authoritative.

---

# Metrics & Observability

```
GET http://localhost:8081/metrics
```

```json
{
  "events_ingested": 12400,
  "events_broadcasted": 13800,
  "messages_delivered": 4200,
  "messages_dropped": 0,
  "active_clients": 0,
  "active_rooms": 0,
  "sma_input_len": 0,
  "ohlc_input_len": 0,
  "ema_input_len": 0,
  "bb_input_len": 0,
  "rsi_input_len": 0,
  "macd_input_len": 0,
  "goroutines": 26,
  "heap_mb": 9.9,
  "sys_mb": 25.4
}
```

`*_input_len` values indicate engine backlog. Under normal load these stay at 0. Active clients and rooms will always be 0 on the data server — clients connect to the client server only.

---

# Backpressure Handling

| Channel | Buffer | Purpose |
|---|---|---|
| `Send` | 2048 | Push path — room broadcasts |
| `IndicatorFeed` | 256 | Pull path — topic subscriptions |

If a client becomes slow: messages dropped → drop counter increments → client disconnected at threshold.

---

# Postgres Schema

```sql
CREATE TABLE sma (
    time TIMESTAMPTZ NOT NULL, instrument INT NOT NULL,
    resolution TEXT NOT NULL, value DOUBLE PRECISION NOT NULL
);
CREATE TABLE ema (
    time TIMESTAMPTZ NOT NULL, instrument INT NOT NULL,
    resolution TEXT NOT NULL, value DOUBLE PRECISION NOT NULL
);
CREATE TABLE ohlc (
    time TIMESTAMPTZ NOT NULL, instrument INT NOT NULL,
    resolution TEXT NOT NULL, open DOUBLE PRECISION NOT NULL,
    high DOUBLE PRECISION NOT NULL, low DOUBLE PRECISION NOT NULL,
    close DOUBLE PRECISION NOT NULL
);
CREATE TABLE bb (
    time TIMESTAMPTZ NOT NULL, instrument INT NOT NULL,
    resolution TEXT NOT NULL, upper DOUBLE PRECISION NOT NULL,
    middle DOUBLE PRECISION NOT NULL, lower DOUBLE PRECISION NOT NULL
);
CREATE TABLE rsi (
    time TIMESTAMPTZ NOT NULL, instrument INT NOT NULL,
    resolution TEXT NOT NULL, value DOUBLE PRECISION NOT NULL
);
CREATE TABLE macd (
    time TIMESTAMPTZ NOT NULL, instrument INT NOT NULL,
    resolution TEXT NOT NULL, macd_line DOUBLE PRECISION NOT NULL,
    signal_line DOUBLE PRECISION NOT NULL, histogram DOUBLE PRECISION NOT NULL
);
```

---

# Running the System

**Start data server (keep running all day):**
```powershell
$env:FEED_SOURCE="binance"; go run cmd/dataserver/main.go
```

**Start client server:**
```powershell
go run cmd/clientserver/main.go
```

**Run push load tester:**
```powershell
cd ws-load-tester
go run main.go -clients 100 -rooms 101,102,103 -duration 60 -host localhost:8080
```

**Run pull load tester:**
```powershell
cd ws-pull-tester
go run main.go -clients 10 -topics sma:101,rsi:101,macd:101,bb:101,ema:101 -duration 60 -host localhost:8080
```

**Query historical data:**
```powershell
Invoke-RestMethod "http://localhost:8080/history?instrument=101&indicator=sma&hours=3&resolution=1m"
Invoke-RestMethod "http://localhost:8080/history?instrument=101&indicator=macd&hours=6&resolution=1h"
```

**Check metrics:**
```powershell
Invoke-RestMethod http://localhost:8081/metrics | ConvertTo-Json
```

---

# Load Test Results

| Level | Model | Clients | Rooms/Topics | Avg msg/s | Dropped | Avg latency |
|---|---|---|---|---|---|---|
| Push L4 | Push | 100 | 10 rooms | 675.3 | 0 | - |
| Push+3Eng | Push | 100 | 10 rooms (SMA+EMA+OHLC) | 8928.4 | 0 | ~135ms |
| Pull L1 | Pull | 100 | 4 topics | 382.9 | 0 | ~18ms |
| Pull L2 | Pull | 100 | 12 topics | 775.3 | 0 | ~28ms |
| Pull BB | Pull | 10 | bb:101 | 2.0/client | 0 | ~47ms |
| Pull RSI+MACD | Pull | 10 | rsi:101, macd:101 | 15.3/client | 0 | ~6ms |
| Pull All 5 | Pull | 10 | sma+ema+rsi+macd+bb:101 | 35.7 | 0 | ~6ms |

---

# File Structure

```
cmd/
  dataserver/main.go       ← data server binary (engines + history writer)
  clientserver/main.go     ← client server binary (ws + /history)
internal/
  analytics/
    engine.go              ← SMA engine
    sma.go                 ← SMA state
    store.go               ← SMAStore (Redis + Postgres)
    ema.go                 ← EMA engine
    ema_store.go           ← EMAStore
    ohlc.go                ← OHLC engine
    ohlc_store.go          ← OHLCStore
    bb.go                  ← Bollinger Bands engine
    bb_store.go            ← BBStore
    rsi.go                 ← RSI engine
    rsi_store.go           ← RSIStore
    macd.go                ← MACD engine
    macd_store.go          ← MACDStore
    registry.go            ← Feeder interface + Registry
  history/
    store.go               ← hist:1m / hist:1h Redis read/write
    rollup.go              ← hourly avg rollup job
    handler.go             ← GET /history endpoint
  background/worker.go     ← feeds registry.Feed(tick)
  hub/
    hub.go                 ← Hub struct + getters
    hub_init.go            ← wires engines, NewHub + NewClientHub
    hub_run.go             ← single-writer event loop
    client.go              ← Client struct
    client_ws.go           ← WritePump + ReadPump
    events.go              ← event types
    subscription.go        ← SubscriptionManager
    message.go
    room.go
    redis_subscriber.go
    redis_message.go
  metrics/
    handler.go             ← /metrics endpoint
    metrics.go
  ws/
    handler.go             ← ServeWS
    ingest_handler.go
  redis/cache.go
  cache/l1.go
  domain/trading/...
ws-load-tester/main.go     ← push load tester
ws-pull-tester/main.go     ← pull load tester
ws-consumer/main.go        ← visual output consumer
```

---

# Current System Capabilities

✔ Real-time Binance exchange ingestion (20 instruments)
✔ Deterministic single-writer event routing
✔ Two-server architecture (data server + client server)
✔ Multi-room push subscriptions
✔ Fine-grained pull subscriptions (per indicator per instrument)
✔ Push + pull coexistence on same connection
✔ Indicator registry (zero-change extensibility for new engines)
✔ SMA engine — 1s + 1m resolutions
✔ EMA engine — 1s + 1m resolutions
✔ OHLC engine — 1m candles
✔ Bollinger Bands engine — 1m resolution
✔ RSI engine — 1s + 1m resolutions (Wilder's smoothing)
✔ MACD engine — 1s + 1m resolutions (12/26/9)
✔ Redis warm-start (sorted sets per engine per instrument)
✔ Postgres data lake (all 6 indicator tables)
✔ History delivery on room join (all 6 indicators)
✔ Rolling 1m history namespace (48h TTL)
✔ Hourly rollup job (avg 1m → 1h, 7d TTL)
✔ REST /history endpoint (Redis-first, Postgres fallback)
✔ Cross-instance event fan-out via Redis pub/sub
✔ Backpressure control with client eviction
✔ Live metrics endpoint (all 6 engine input lens)
✔ Push + pull load testers
✔ Visual ws-consumer (all indicator types)

---

# What This System Is Not

This project intentionally excludes:
```
Frontend UI
Kafka / NATS streaming
Replay capabilities
Authentication
```

The focus is **event infrastructure**, not full application stacks.

---

# Future Work

### Durability
Introduce an append-only event log:
```
Redis Streams / Kafka / NATS JetStream
```
for replay and recovery.

### Horizontal Scaling
```
Hub sharding
Instrument partitioning
Worker pools
```

### New Indicators
Adding a new indicator (e.g. ATR, Stochastic) requires:
1. New engine file implementing `Feeder` interface
2. New store file (Redis + Postgres)
3. Register in `hub_init.go` — worker picks it up automatically
4. Add history write in `hub_init.go` broadcast goroutine
