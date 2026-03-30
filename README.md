# Distributed WebSocket Event Backbone (Go)

A real-time market data backend in Go. Ingests live Binance price feeds, runs 6 streaming analytics engines, and distributes indicator updates to WebSocket consumers via a two-server architecture.

---

## Architecture

```
Binance (300 instruments)
        │
        ▼
Background Worker → Indicator Registry
                           │
              ┌────────────┼────────────┐
             SMA  EMA  OHLC  BB  RSI  MACD
                           │
                     Data Server :8081
                     - Runs all engines
                     - Writes Redis + Postgres
                     - Publishes analytics:events
                           │
                    Redis pub/sub
                           │
                    Client Server :8080
                    - /ws  (push + pull)
                    - /history (REST)
```

---

## Redis Setup — 2 Pairs + Load Balancer

Two independent primary/replica pairs run in parallel. A client-side load balancer routes traffic across them.

```
pair 1:  primary :6381  replica :6380  sentinel :26380  (mymaster)
pair 2:  primary :6383  replica :6382  sentinel :26381  (mymaster2)
```

**Three mechanisms stack together:**

| Mechanism | Role |
|---|---|
| Sentinel (×2) | Failover — detects primary death, promotes replica automatically |
| Load Balancer | Routing — writes go to least-loaded healthy primary |
| Scatter-Gather | Speed — reads fan out to both replicas concurrently, fastest wins |

On read: both replicas are queried in parallel, the first successful response is used. If both replicas fail, falls back to the write client (primary). If one pair is saturated or down, all traffic routes to the other pair.

---

## Analytics Engines

| Engine | Params | Resolution | Storage |
|---|---|---|---|
| SMA | window=20 | 1s + 1m | Redis + Postgres |
| EMA | window=20 | 1s + 1m | Redis + Postgres |
| OHLC | — | 1m | Redis + Postgres |
| Bollinger Bands | window=20, k=2.0 | 1m | Redis + Postgres |
| RSI | period=14, Wilder's | 1s + 1m | Redis + Postgres |
| MACD | fast=12, slow=26, signal=9 | 1s + 1m | Redis + Postgres |

---

## History Layer

| Tier | Key | Granularity | TTL |
|---|---|---|---|
| Warm cache | `hist:1m:{indicator}:{id}` | 1 min | 48h |
| Hourly rollup | `hist:1h:{indicator}:{id}` | 1 hour | 7d |
| Cold storage | Postgres | 1 min | unlimited |

On room join: client receives a history burst of last 1800×1s + 60×1m points for all 6 indicators.

Hourly rollup job averages 60 1m-points into one 1h-point (OHLC uses proper open/high/low/close merge).

---

## Instruments

300 active Binance USDT spot pairs (IDs 101–400). Configured in `internal/domain/trading/instruments.go`.

---

## Run

Start Redis (6 terminals in redis folder):
```powershell
.\redis-server.exe redis-primary.conf        # pair1 primary :6381
.\redis-server.exe redis-replica.conf        # pair1 replica :6380
.\redis-server.exe sentinel.conf --sentinel  # sentinel1 :26380
.\redis-server.exe redis-pair2-primary.conf  # pair2 primary :6383
.\redis-server.exe redis-pair2-replica.conf  # pair2 replica :6382
.\redis-server.exe sentinel2.conf --sentinel # sentinel2 :26381
```

Start nginx:
```powershell
cd "C:\...\nginx-1.29.6"; .\nginx.exe
```

Build and run servers:
```powershell
go build -o dataserver.exe ./cmd/dataserver
go build -o clientserver.exe ./cmd/clientserver

$env:FEED_SOURCE="binance"; $env:DATA_PORT="8081"; .\dataserver.exe
$env:FEED_SOURCE="binance"; $env:DATA_PORT="8083"; .\dataserver.exe  # standby
.\clientserver.exe
$env:CLIENT_PORT="8082"; .\clientserver.exe                           # optional second
```

Load tester:
```powershell
cd ws-load-tester
go build -o load-tester.exe .

.\load-tester.exe -phase 1 -clients 200 -duration 120 -host localhost:80
.\load-tester.exe -phase 2 -clients 500 -duration 120 -host localhost:80
```

---

## Endpoints

```
WS   /ws                     push + pull WebSocket
WS   /ws/ingest              ingestor WebSocket
GET  /history                ?instrument=101&indicator=sma&hours=3&resolution=1m
GET  /metrics                engine backlogs, heap, goroutines, drop rate
GET  /redis-status           load balancer health — all 4 nodes, in-flight, avg latency
```

---

## Push vs Pull

**Push** — join a room, receive all 6 indicator updates for that instrument:
```json
{ "type": "join", "room": "101" }
```

**Pull** — subscribe to a specific indicator:
```json
{ "type": "subscribe",   "topic": "rsi:101" }
{ "type": "unsubscribe", "topic": "rsi:101" }
```

Both models coexist on the same connection.

---

## Load Test Results

| Phase | Clients | Instruments | Total msgs | Drops | Avg latency | Heap |
|---|---|---|---|---|---|---|
| 1 | 200 | 300 | 310k | 0 | ~63ms | ~10MB |
| 2 | 500 | 300 | 776k | 0 | ~100ms | ~10MB |

Zero drops across both phases. Heap stable regardless of client count.

---

## Postgres Schema

```sql
CREATE TABLE sma  (time TIMESTAMPTZ, instrument INT, resolution TEXT, value DOUBLE PRECISION);
CREATE TABLE ema  (time TIMESTAMPTZ, instrument INT, resolution TEXT, value DOUBLE PRECISION);
CREATE TABLE rsi  (time TIMESTAMPTZ, instrument INT, resolution TEXT, value DOUBLE PRECISION);
CREATE TABLE ohlc (time TIMESTAMPTZ, instrument INT, resolution TEXT, open DOUBLE PRECISION, high DOUBLE PRECISION, low DOUBLE PRECISION, close DOUBLE PRECISION);
CREATE TABLE bb   (time TIMESTAMPTZ, instrument INT, resolution TEXT, upper DOUBLE PRECISION, middle DOUBLE PRECISION, lower DOUBLE PRECISION);
CREATE TABLE macd (time TIMESTAMPTZ, instrument INT, resolution TEXT, macd_line DOUBLE PRECISION, signal_line DOUBLE PRECISION, histogram DOUBLE PRECISION);
```

---

## Adding a New Indicator

1. Implement `Input() chan<- PriceUpdateEvent` and `InputLen() int` on your engine
2. Add store file (Redis sorted set + Postgres insert)
3. Register in `hub_init.go` — background worker picks it up automatically

No changes to the worker, registry, or routing layer.

---

## Tech Stack

Go · Redis (Sorted Sets, Pub/Sub, Sentinel) · Postgres (pgx/v5) · gorilla/websocket · Ristretto (L1 cache) · nginx (least_conn WS proxy)
