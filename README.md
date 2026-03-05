# Distributed Market Data Event Backbone (Go)

A **real-time WebSocket event routing backbone** built in Go for distributing and processing live market data streams.

This system is **not a chat application and not a frontend system**.
It is designed as a **backend event infrastructure prototype**, focusing on **deterministic routing, concurrency correctness, and streaming analytics**.

All interaction is performed via Go services and terminal-based WebSocket clients.

---

# System Overview

The system ingests real-time market trade data, normalizes it into canonical domain events, and distributes those events to subscribed consumers.

In addition to routing raw events, the backbone also supports **stream analytics**, demonstrated via a rolling **Simple Moving Average (SMA)** computation layer.
```
Binance Streams
       │
       ▼
Feed Adapter
       │
       ▼
Hub (Event Backbone)
       │
 ┌─────┴─────┐
 │           │
Raw Events   Analytics Engine
 │           │
 │      Derived Events (SMA)
 │           │
 └─────► Clients ◄─────┘
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

The system separates:

| Layer        | Responsibility                |
| ------------ | ----------------------------- |
| Feed Adapter | Exchange ingestion            |
| Hub          | Event routing                 |
| Domain       | Market data semantics         |
| Analytics    | Streaming computation         |
| Redis        | Cross-instance infrastructure |

This prevents domain logic from leaking into infrastructure components.

---

### Payload Agnostic Routing

The Hub does not interpret event payloads.

It routes based on:
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
* Event broadcasting
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

## WebSocket Clients

Clients can act as:

### Consumers

Consumers subscribe to rooms:
```
join 101
join 102
```

They receive:
```
price_update
sma_update
```

events.

---

### Ingestors

Ingestor clients publish events but **never subscribe to rooms**.

Example publisher:
```
Feed Adapter
```

---

# Trading Domain

The trading module defines canonical market events.

### Event: price_update
```
{
  "instrument": "BTC_USDT",
  "price": 71240.21,
  "timestamp": ...
}
```

Rooms map to **numeric instrument IDs**:

| Instrument | ID  |
| ---------- | --- |
| BTC_USDT   | 101 |
| ETH_USDT   | 102 |
| BNB_USDT   | 103 |
| XRP_USDT   | 104 |
| SOL_USDT   | 105 |

Numeric routing improves performance and simplifies distributed partitioning.

---

# Feed Adapter

A dedicated ingestion service connects to the Binance multi-stream WebSocket endpoint.

Responsibilities:

* Exchange connection
* Payload normalization
* Instrument ID mapping
* Event publishing to Hub

This isolates exchange-specific logic from the backbone.

---

# Analytics Layer

The system includes a **stream analytics engine** that processes live events.

Current implementation:

### Rolling SMA (Simple Moving Average)
```
SMA = average of last N prices
```

Current configuration:
```
SMA window = 20 prices
```

Pipeline:
```
price_update
     │
     ▼
Analytics Engine
     │
     ▼
sma_update
```

Example derived event:
```
{
  "type": "sma_update",
  "instrument_id": 101,
  "value": 71239.88
}
```

Analytics runs **asynchronously and non-blocking** to preserve Hub performance.

---

# Redis Integration

Redis is used strictly as **infrastructure**, not as a source of truth.

### Redis Pub/Sub

Used for **cross-instance event fan-out**.

Allows multiple Hub instances to broadcast events.

---

### Redis KV

Used for **warm-starting consumers**.

When a client joins a room:

1. L1 cache checked
2. Redis fallback queried
3. Latest price sent immediately

---

### Failure Mode

If Redis becomes unavailable:
```
System automatically degrades to single-instance mode
```

Event routing continues uninterrupted.

---

# Caching Strategy

Two-level caching system:

### L1 Cache (In-Memory)

Implemented with **Ristretto**.

Stores:
```
instrument:last_price
```

Owned exclusively by the Hub.

---

### L2 Cache (Redis)

Used for warm-start recovery and cross-instance visibility.

Redis is **not authoritative**.

---

# Metrics & Observability

The system includes built-in runtime metrics.

Tracked values include:
```
Events Ingested
Events Broadcasted
Messages Delivered
Messages Dropped
Active Clients
Active Rooms
```

Metrics are logged periodically to observe throughput and system health.

---

# Backpressure Handling

Each client has a bounded send channel.

If a client becomes slow:
```
messages dropped
drop counter increments
client eventually disconnected
```

This protects the Hub from slow consumers.

---

# Example Client Output
```
💰 PRICE BTC_USDT = 71240.01
💰 PRICE BTC_USDT = 71239.99
📊 SMA(101) = 71239.98
```

Clients receive both **raw market data** and **derived analytics events**.

---

# Current System Capabilities

✔ Real-time exchange ingestion
✔ Deterministic event routing
✔ Multi-room subscriptions
✔ Cross-instance event fan-out
✔ Rolling streaming analytics
✔ Backpressure control
✔ Redis-based warm start
✔ Metrics instrumentation

---

# What This System Is Not

This project intentionally excludes:
```
Frontend UI
REST APIs
Persistent event storage
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
* Distributed fan-out design
* Streaming analytics patterns
* Failure-aware infrastructure design

---

# Future Work

Planned system evolution:

### Phase 6 – Durability

Introduce an append-only event log using:
```
Redis Streams
Kafka
NATS JetStream
```

to support replay and recovery.

---

### Horizontal Scaling

Future architecture may include:
```
Hub sharding
Instrument partitioning
Worker pools
```

to support higher throughput.

---

# Running the System

Start the server:
```
go run cmd/server/main.go
```

Run a WebSocket consumer:
```
go run ws-consumer/main.go 101
```

Subscribe to multiple instruments:
```
go run ws-consumer/main.go 101,102
```

---

# Why This Project Exists

This project is a **learning-focused distributed systems prototype** exploring how real-time market data infrastructure can be designed with:

* strong concurrency guarantees
* minimal shared state
* deterministic event routing

It serves as a foundation for experimenting with **scalable event-driven backend systems**.
