# Distributed WebSocket Event Backbone (Go)

A real-time WebSocket-based event routing backbone built in Go, with Redis used for cross-instance fan-out and warm-start caching.

This is not a frontend project and not a chat application. All interaction is done via Go programs and terminal-based clients.

---

## Overview

This system functions as an event backbone rather than an application.

- WebSocket connections act as event producers or consumers
- Consumers subscribe to topics (rooms)
- The server routes events only and does not execute domain logic
- Architectural correctness and concurrency invariants are the primary focus

---

## Architecture

WebSocket Client
│
▼
ReadPump() → domain validation → BroadcastEvent
WritePump() ← Hub fan-out
│
▼
Hub (single goroutine, routing authority)
│
├─ Local room fan-out
└─ Redis Pub/Sub (cross-instance fan-out)


---

## Hub Design & Invariants

- Exactly one Hub goroutine per server instance
- No mutexes
- No concurrent map writes
- All state mutation occurs via channels
- Hub is strictly routing-only
- No domain logic inside Hub
- No Redis KV access inside Hub

Rooms are implemented as pure routing constructs:

```go
map[string]map[*Client]bool
Client Model

Each WebSocket connection is represented as a Client.

Roles:

INGESTOR – produces domain events

CONSUMER – subscribes to rooms and receives events

Each client runs two goroutines:

ReadPump() for inbound messages

WritePump() for outbound messages

internal/domain/
├── trading/
├── chat/
└── common/
Domains validate input and emit domain events

Infrastructure adapts domain events into Hub broadcasts

Hub is completely payload-agnostic

Trading Domain

Active event type: price_update
{
  "type": "price_update",
  "instrument": "BTC_USDT",
  "price": 60214.3,
  "ts": 1710000000
}
Rules:

Only INGESTOR clients can publish trading events

INGESTORS never join rooms

Consumers subscribe by joining rooms

Room name equals instrument name

Redis Integration

Redis is optional and non-authoritative.

Redis Pub/Sub

Used for cross-instance fan-out

Messages include an Origin field

Instances ignore their own Redis echoes

No ordering guarantees

No persistence

Redis KV (Ephemeral Cache)

Stores last known price per instrument

TTL-based

Used only to warm-start new consumers

Written only at ingestion time

Never written inside the Hub

Redis failure degrades the system to single-instance operation.

Verified Behavior

Local routing works without Redis

Redis Pub/Sub enables cross-instance fan-out

Redis KV warm-starts new consumers

TTL expiration works correctly

Redis can go down without breaking local routing

Current State

Stable event routing backbone

Redis Pub/Sub and KV fully integrated

Hub invariants preserved

No frontend

No persistence or replay

No Kafka or NATS

Purpose

This project exists to explore and practice:

Concurrency correctness

Single-writer state ownership

Event routing design

Distributed fan-out

Failure-aware backend system architecture
