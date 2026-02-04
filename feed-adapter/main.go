package main

import (
	"github.com/riyanshsachdev/feed-adapter/backend"
	"github.com/riyanshsachdev/feed-adapter/exchange"
	"github.com/riyanshsachdev/feed-adapter/normalizer"
)

func main() {
	// Channel for raw exchange data
	rawFeed := make(chan exchange.RawPrice, 100)

	// Channel for normalized domain events
	events := make(chan interface{}, 100)

	// 1️⃣ Start mock exchange feed
	exchange.StartMockFeed(rawFeed)

	// 2️⃣ Create WS ingestor (does NOT connect yet)
	ws := backend.NewWSIngestor(
		"ws://localhost:8080/ws/ingest",
		"INGESTOR_API_KEY",
	)

	// 3️⃣ Start reconnecting ingest loop
	go ws.Start(events)

	// 4️⃣ Pipeline: raw → domain → events channel
	for raw := range rawFeed {
		event := normalizer.MapToDomain(raw)
		events <- event
	}
}
