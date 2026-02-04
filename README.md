# Distributed WebSocket Event Backbone (Go + Redis)

A **real-time, WebSocket-based event routing backbone** built in Go, designed with strict concurrency and architectural invariants.

This project evolved from a chat-style system into a **trading-style pub/sub event router**, focusing on correctness, separation of concerns, and distributed fan-out — **not frontend UI**.

All interaction is done via Go programs and terminals.

---

## 🧠 Core Philosophy

This is **not** a chat application.

This system is an **event backbone**:

- WebSocket connections are **event producers or consumers**
- Clients **subscribe to topics (rooms)**
- The server routes events — it does **not** execute domain logic
- Correctness and invariants matter more than features

---

## 🧱 High-Level Architecture

### Per Server Instance

WebSocket Client
│
▼
ReadPump() → domain validation → BroadcastEvent
WritePump() ← Hub fan-out
│
▼
Hub (single goroutine, routing authority)
│
├─ Local fan-out to subscribed clients
└─ Redis Pub/Sub (cross-instance fan-out)


### Key Architectural Rules

- Exactly **one Hub goroutine**
- No mutexes
- No concurrent map writes
- All state mutation via channels
- Hub is **routing-only**
- Domains define meaning, Hub defines routing

---

## 🧩 Core Components

### Hub

The Hub is the **single authoritative router** per server process.

Responsibilities:
- Owns all room membership state
- Routes events to subscribed clients
- Publishes ingestion events to Redis Pub/Sub
- Subscribes to Redis Pub/Sub for cross-instance fan-out
- Ignores its own Redis echoes using `Origin`

Non-responsibilities:
- ❌ No domain validation
- ❌ No Redis KV writes
- ❌ No business logic
- ❌ No blocking on clients

Rooms are implemented as:

```go
map[string]map[*Client]bool


Client

Each client represents one WebSocket connection.

Roles:

INGESTOR – produces domain events (e.g., price feeds)

CONSUMER – subscribes to topics and receives updates

Each client runs:

ReadPump() → input, validation, ingestion

WritePump() → outbound fan-out

🔄 Domain Separation
internal/domain/
├── trading/
├── chat/
└── common/


Domains validate input

Domains emit domain events

Infrastructure adapts domain events → Hub events

Hub is completely payload-agnostic

Rules

Only INGESTOR clients can publish trading events

INGESTORS never join rooms

Consumers subscribe by joining rooms

Room name == instrument name

🌍 Redis Integration

Redis is used in two strictly limited roles.

1️⃣ Redis Pub/Sub (Event Bus)

Used for cross-instance fan-out

Messages include Origin

Instances ignore their own echoes

No ordering guarantees

No persistence

Redis failure → system degrades to single-instance operation

2️⃣ Redis KV (Ephemeral Cache)

Used only for warm-starting consumers

Stores last known price per instrument

TTL-based (ephemeral)

Written only at ingestion time

Never written inside the Hub

Not authoritative

Redis failure → no warm-start, system still functions

🔑 Critical Design Decision (Invariant)

Redis KV writes happen at the ingestion boundary, not in the Hub.

Specifically:

KV writes occur inside ReadPump() for INGESTOR clients

The Hub remains a pure router

This avoids:

Redis echo confusion

Duplicate writes

Routing logic leaking into infrastructure

🧪 Verified Behavior

Local routing works without Redis

Redis Pub/Sub fan-out works across instances

Redis KV stores last known prices with TTL

New consumers receive one warm-start event

Feed stops → no continuous updates

Redis down → system still works locally

🎯 Current State

Stable event routing

Redis Pub/Sub + KV fully integrated

Hub invariants preserved

No frontend

No persistence or replay

No Kafka / NATS

🔮 Planned (Not Implemented)

Authentication & API keys for INGESTORS

Read-only consumer enforcement

Event ordering guarantees

Persistence & replay

Kafka / NATS integration

Risk / analytics services

🧠 Learning Goals

This project focuses on:

Concurrency correctness

Single-writer state ownership

Distributed fan-out

Failure-aware design

Clean separation of domain and infrastructure

📌 How to Run

Multiple terminals are used:

Redis server

One or more backend server instances

WebSocket INGESTOR clients

WebSocket CONSUMER clients
