# Distributed WebSocket Event Backbone

A real-time WebSocket-based event routing system built in Go.

This project is an event backbone, not a chat application and not a frontend system.  
All interaction is done via Go programs and terminal-based WebSocket clients.

---

## Overview

- WebSocket connections act as event producers or consumers
- Consumers subscribe to topics (rooms)
- The server routes events only
- Domain logic is separated from routing logic
- Focus on correctness, concurrency, and clean architecture

---

## Architecture

- Single Hub goroutine per server instance
- Hub owns all routing state
- No mutexes
- No concurrent map writes
- All state mutation via channels
- Hub is routing-only and payload-agnostic

---

## Core Components

- Hub
- WebSocket Clients
- Domain modules (trading, chat, common)
- Redis (optional)

---

## Trading Domain

- Event type: `price_update`
- Room name equals instrument name
- Only INGESTOR clients can publish events
- INGESTORS never join rooms
- Consumers subscribe by joining rooms

---

## Redis Integration

- Redis Pub/Sub used for cross-instance fan-out
- Redis KV used for warm-starting consumers
- Redis is not authoritative
- Redis failure degrades system to single-instance mode

---

## Current State

- Redis Pub/Sub and KV fully integrated
- Hub invariants preserved
- No frontend
- No persistence or replay
- No Kafka or NATS

---

## Purpose

This project is built to practice backend and distributed systems concepts such as:

- Concurrency correctness
- Single-writer state ownership
- Event routing
- Distributed fan-out
- Failure-aware system design
