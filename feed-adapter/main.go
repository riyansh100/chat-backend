package main

import (
	"context"
	"os"

	"github.com/riyansh/chat-backend/feed-adapter/backend"
	"github.com/riyansh/chat-backend/feed-adapter/exchange"
	"github.com/riyansh/chat-backend/feed-adapter/normalizer"
)

func main() {
	source := os.Getenv("FEED_SOURCE")
	if source == "" {
		source = "mock"
	}

	// Channel for raw exchange data
	rawFeed := make(chan exchange.RawPrice, 100)

	// Channel for normalized domain events
	events := make(chan interface{}, 100)

	// 🔹 WS ingestor MUST start regardless of source
	ws := backend.NewWSIngestor(
		"ws://localhost:8080/ws/ingest",
		"INGESTOR_API_KEY",
	)
	go ws.Start(events)

	// 🔹 Producer selection
	if source == "mock" {

		exchange.StartMockFeed(rawFeed)

		for raw := range rawFeed {
			event := normalizer.MapToDomain(raw)
			events <- event
		}

	} else if source == "binance" {

		ctx := context.Background()
		out := make(chan exchange.NormalizedPriceEvent, 100)

		adapter := &exchange.BinanceAdapter{
			Endpoint: "wss://stream.binance.com:9443/stream?streams=btcusdt@trade/ethusdt@trade/bnbusdt@trade/xrpusdt@trade/solusdt@trade/adausdt@trade/dogeusdt@trade/maticusdt@trade/ltcusdt@trade/dotusdt@trade",
			Out:      out,
		}

		go adapter.Start(ctx)

		// forward normalized events into generic events channel
		go func() {
			for evt := range out {
				domainEvent := normalizer.PriceUpdateFromNormalized(evt)
				events <- domainEvent
			}
		}()

		// keep process alive
		select {}
	}
}
