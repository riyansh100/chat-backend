# Distributed Market Data Event Backbone (Go)

A **real-time WebSocket event routing backbone** built in Go for distributing and processing live market data streams.

This system is **not a chat application and not a frontend system**.
It is designed as a **backend event infrastructure prototype**, focusing on **deterministic routing, concurrency correctness, and streaming analytics**.

All interaction is performed via Go services and terminal-based WebSocket clients.

---

# System Overview

The system ingests real-time market trade data from Binance, normalizes it into canonical domain events, and distributes those events to subscribed consumers.

In addition to routing raw events, the backbone supports **stream analytics** via multiple indicator engines running in parallel, and exposes both a **push model** (room-based broadcast) and a **pull model** (topic-based subscription) for downstream consumers.

```
Binance Streams
       │
       ▼
Background Worker (direct feed)
       │
       ▼
Indicator Registry
       │
  ┌────┼────┐
  │    │    │
 SMA  EMA  OHLC
  │    │    │
  └────┼────┘
       │
       ▼
Hub (Event Backbone)
       │
  ┌────┴────────────────┐
  │                     │
Push Model           Pull Model
(Room Broadcast)     (Topic Subscription)
  │                     │
  ▼                     ▼
Clients             Clients
```

The Hub remains **routing-only** and does not contain domain computation logic.

---

# Key Design Principles

### Single Writer State Ownership

Each server instance runs a **single Hub goroutine** that owns all routing state.

This ensures:

* No concurrent map writes
* No routing mutexes
* Deterministic state transitions

All state mutations occur via channels.

---

### Domain / Infrastructure Separation

| Layer                 | Responsibility                          |
| --------------------- | --------------------------------------- |
| Feed Adapter          | Exchange ingestion                      |
| Background Worker     | Direct engine feeding, source agnostic  |
| Indicator Registry    | Fan-out tick to all registered engines  |
| Hub                   | Event routing                           |
| Domain                | Market data semantics                   |
| Analytics             | Streaming computation (SMA, EMA, OHLC)  |
| Redis                 | Cross-instance infrastructure + history |
| Postgres              | Durable analytics data lake             |

---

### Payload Agnostic Routing

The Hub does not interpret event payloads. It routes based on:
```
room / instrument ID
```

This allows the backbone to support **any event type** without modification.

---

# Core Components

## Hub

Central event router responsible for:

* WebSocket client management
* Room subscription management
* Event broadcasting (push model)
* Topic-based fanout (pull model)
* Backpressure enforcement
* Redis fan-out
* L1 cache updates
* Metrics tracking

Key invariants:
```
Single writer
No shared state
Channel-driven architecture
```

---

## Indicator Registry

A lightweight fan-out abstraction over all analytics engines.

```go
type Feeder interface {
    Input() chan<- PriceUpdateEvent
    InputLen() int
}
```

The background worker calls `registry.Feed(tick)` once per price event. The registry fans it out to every registered engine non-blocking.

**Adding a new indicator in the future requires:**
1. Implement `Input()` and `InputLen()` on your engine
2. Call `hub.registry.Register(engine)` in `hub_init.go`
3. Wire its typed output channel for broadcast + persistence

The worker requires **zero changes**.

---

## Analytics Engines

Three engines run concurrently, each processing all 20 instruments independently.

### SMA — Simple Moving Average
```
SMA = average of last N prices
Window = 20
Resolutions: 1s, 1m
```

### EMA — Exponential Moving Average
```
EMA_t = price × k + EMA_{t-1} × (1 - k)
k = 2 / (window + 1), window = 20
Resolutions: 1s, 1m
```

### OHLC — Open / High / Low / Close Candles
```
1-minute candles
Flush at minute boundary
```

All engines share the same architecture:
* Single goroutine, ticker-based flush
* Buffered input channel (cap 1024)
* Non-blocking fan-out to output channels

---

## Persistence Layer

Each analytics engine persists to both **Redis** (hot, warm-start) and **Postgres** (cold, data lake).

| Store      | Redis Key Pattern  | Postgres Table | Retention          |
| ---------- | ------------------ | -------------- | ------------------ |
| SMA Store  | `sma:1s:{id}`      | `sma`          | 60m (1s), 24h (1m) |
| EMA Store  | `ema:1s:{id}`      | `ema`          | 60m (1s), 24h (1m) |
| OHLC Store | `ohlc:1m:{id}`     | `ohlc`         | 24h (1m candles)   |

On room join, the server immediately delivers historical data to the client:
```
sma_history  — last 1800 × 1s + 60 × 1m points
ema_history  — last 1800 × 1s + 60 × 1m points
ohlc_history — last 60 × 1m candles
```

---

## Push Model (Room Broadcast)

Classic room-based subscription. Client joins a room and receives all indicator updates for that instrument.

```json
{ "type": "join", "room": "101" }
```

Client receives:
```
price_update
sma_update  (1s + 1m)
ema_update  (1s + 1m)
ohlc_update (1m)
+ history burst on join
```

---

## Pull Model (Topic Subscription)

Clients subscribe to **specific indicator + instrument combinations** without joining a room. Fine-grained, lower latency, no history burst.

```json
{ "type": "subscribe",   "topic": "sma:101" }
{ "type": "unsubscribe", "topic": "sma:101" }
```

Valid topic format: `{indicator}:{instrument_id}`

Examples:
```
sma:101    ema:102    ohlc:103
```

Each client has a dedicated `IndicatorFeed` channel. The `SubscriptionManager` fans out matching updates into it, completely independent of the room broadcast path.

**Both models coexist.** A client can join rooms (push) and subscribe to topics (pull) simultaneously.

---

## WebSocket Clients

Clients can act as:

### Consumers
Subscribe to rooms or topics and receive market data + analytics events.

### Ingestors
Publish price events but never subscribe to rooms.

---

# Trading Domain

Rooms map to numeric instrument IDs:

| Instrument  | ID  |
| ----------- | --- |
| BTC_USDT    | 101 |
| ETH_USDT    | 102 |
| BNB_USDT    | 103 |
| XRP_USDT    | 104 |
| SOL_USDT    | 105 |
| ADA_USDT    | 106 |
| DOGE_USDT   | 107 |
| MATIC_USDT  | 108 |
| LTC_USDT    | 109 |
| DOT_USDT    | 110 |
| AVAX_USDT   | 111 |
| LINK_USDT   | 112 |
| UNI_USDT    | 113 |
| ATOM_USDT   | 114 |
| TRX_USDT    | 115 |
| ETC_USDT    | 116 |
| FIL_USDT    | 117 |
| ICP_USDT    | 118 |
| APT_USDT    | 119 |
| ARB_USDT    | 120 |

---

# Feed Adapter / Background Worker

A background worker connects directly to Binance on startup and feeds all analytics engines independently of any consumer being connected.

Supports two modes via `FEED_SOURCE` env var:
```
FEED_SOURCE=binance   (default) — live Binance multi-stream
FEED_SOURCE=mock      — deterministic fake prices for all 20 instruments
```

---

# Redis Integration

Redis is used strictly as **infrastructure**, not as a source of truth.

### Redis Pub/Sub
Cross-instance event fan-out. Allows multiple Hub instances to broadcast events.

### Redis Sorted Sets
Per-instrument analytics history for warm-start on client join.

### Redis KV
Last price per instrument for L1 cache warm-start.

### Failure Mode
```
System automatically degrades to single-instance mode.
Event routing continues uninterrupted.
```

---

# Caching Strategy

### L1 Cache (In-Memory)
Implemented with **Ristretto**. Stores last price per instrument. Owned exclusively by the Hub.

### L2 Cache (Redis)
Used for warm-start recovery and cross-instance visibility. Not authoritative.

---

# Metrics & Observability

Live metrics available at:
```
GET http://localhost:8080/metrics
```

```json
{
  "events_ingested": 12400,
  "events_broadcasted": 13800,
  "messages_delivered": 4200,
  "messages_dropped": 0,
  "active_clients": 100,
  "active_rooms": 10,
  "sma_input_len": 0,
  "ohlc_input_len": 0,
  "ema_input_len": 0,
  "goroutines": 26,
  "heap_mb": 9.9,
  "sys_mb": 25.4
}
```

`*_input_len` values indicate engine backlog. Under normal load these stay at 0.

---

# Backpressure Handling

Each client has two bounded channels:

| Channel         | Buffer | Purpose                        |
| --------------- | ------ | ------------------------------ |
| `Send`          | 2048   | Push path — room broadcasts    |
| `IndicatorFeed` | 256    | Pull path — topic subscriptions |

If a client becomes slow:
```
messages dropped → drop counter increments → client disconnected at threshold
```

Pull path clients are naturally protected — narrow topic streams rarely overflow.

---

# Load Test Results

| Level      | Model | Clients | Rooms/Topics          | Avg msg/s | Total msgs | Dropped | Avg latency |
| ---------- | ----- | ------- | --------------------- | --------- | ---------- | ------- | ----------- |
| Push L1    | Push  | 10      | 1 room                | 9.5       | 570        | 0       | -           |
| Push L2    | Push  | 10      | 3 rooms               | 25.5      | 1,530      | 0       | -           |
| Push L3    | Push  | 50      | 5 rooms               | 224.7     | 13,480     | 0       | -           |
| Push L4    | Push  | 100     | 10 rooms              | 675.3     | 40,518     | 0       | -           |
| Push L5    | Push  | 100     | 20 rooms              | 700.9     | 42,055     | 0       | -           |
| Push L6    | Push  | 200     | 20 rooms              | 1666.7    | 100,002    | 0       | -           |
| Push+3Eng  | Push  | 100     | 10 rooms (SMA+EMA+OHLC)| 8928.4   | 535,705    | 0       | ~135ms      |
| Pull L1    | Pull  | 100     | 4 topics              | 382.9     | 22,971     | 0       | ~18ms       |
| Pull L2    | Pull  | 100     | 12 topics             | 775.3     | 46,516     | 0       | ~28ms       |

Push vs Pull latency difference is due to channel contention, not buffer size. Pull clients receive a narrow stream with no competition.

---

# Postgres Schema

```sql
CREATE TABLE sma (
    time        TIMESTAMPTZ NOT NULL,
    instrument  INT NOT NULL,
    resolution  TEXT NOT NULL,
    value       DOUBLE PRECISION NOT NULL
);

CREATE TABLE ema (
    time        TIMESTAMPTZ NOT NULL,
    instrument  INT NOT NULL,
    resolution  TEXT NOT NULL,
    value       DOUBLE PRECISION NOT NULL
);

CREATE TABLE ohlc (
    time        TIMESTAMPTZ NOT NULL,
    instrument  INT NOT NULL,
    resolution  TEXT NOT NULL,
    open        DOUBLE PRECISION NOT NULL,
    high        DOUBLE PRECISION NOT NULL,
    low         DOUBLE PRECISION NOT NULL,
    close       DOUBLE PRECISION NOT NULL
);
```

---

# Running the System

**Start server:**
```powershell
go run cmd/server/main.go
```

**Run push load tester:**
```powershell
cd ws-load-tester
go run main.go -clients 100 -rooms 101,102,103 -duration 60
```

**Run pull load tester:**
```powershell
cd ws-pull-tester
go run main.go -clients 100 -topics sma:101,ema:101,ohlc:101 -duration 60
```

**Check metrics:**
```powershell
Invoke-RestMethod http://localhost:8080/metrics | ConvertTo-Json
```

---

# Current System Capabilities

✔ Real-time Binance exchange ingestion (20 instruments)
✔ Deterministic single-writer event routing
✔ Multi-room push subscriptions
✔ Fine-grained pull subscriptions (per indicator per instrument)
✔ Push + pull coexistence on same connection
✔ Indicator registry (zero-change extensibility for new engines)
✔ SMA engine — 1s + 1m resolutions
✔ EMA engine — 1s + 1m resolutions
✔ OHLC engine — 1m candles
✔ Redis warm-start (sorted sets per engine per instrument)
✔ Postgres data lake (SMA, EMA, OHLC tables)
✔ History delivery on room join
✔ Cross-instance event fan-out via Redis pub/sub
✔ Backpressure control with client eviction
✔ Live metrics endpoint
✔ Push + pull load testers

---

# What This System Is Not

This project intentionally excludes:
```
Frontend UI
REST APIs
Kafka / NATS streaming
Replay capabilities
```

The focus is **event infrastructure**, not full application stacks.

---

# Project Goals

This project is built to practice and demonstrate:

* Concurrency correctness in Go
* Single-writer event loop architecture
* Real-time event routing
* Streaming analytics with multiple indicators
* Indicator registry pattern for extensibility
* Push vs pull delivery models
* Distributed fan-out design
* Failure-aware infrastructure design

---

# Future Work

### New Indicators
Adding a new indicator (e.g. Bollinger Bands, RSI) requires:
1. New engine file implementing `Feeder` interface
2. New store file (Redis + Postgres)
3. Register in `hub_init.go` — worker picks it up automatically

### Phase — Durability
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
