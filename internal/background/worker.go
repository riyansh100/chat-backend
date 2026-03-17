package background

import (
	"context"
	"log"
	"math/rand"
	"time"

	"github.com/riyansh/chat-backend/feed-adapter/exchange"
	"github.com/riyansh/chat-backend/internal/analytics"
	"github.com/riyansh/chat-backend/internal/domain/trading"
)

const binanceEndpoint = "wss://stream.binance.com:9443/stream?streams=" +
	"btcusdt@trade/ethusdt@trade/bnbusdt@trade/xrpusdt@trade/solusdt@trade/" +
	"adausdt@trade/dogeusdt@trade/maticusdt@trade/ltcusdt@trade/dotusdt@trade/" +
	"avaxusdt@trade/linkusdt@trade/uniusdt@trade/atomusdt@trade/trxusdt@trade/" +
	"etcusdt@trade/filusdt@trade/icpusdt@trade/aptusdt@trade/arbusdt@trade"

// Worker feeds price ticks into all registered analytics engines
// independently of any WebSocket consumer being connected.
type Worker struct {
	registry *analytics.Registry
	source   string // "binance" or "mock"
}

func NewWorker(source string, registry *analytics.Registry) *Worker {
	return &Worker{
		registry: registry,
		source:   source,
	}
}

func (w *Worker) Start(ctx context.Context) {
	log.Printf("[BG Worker] starting with source=%s", w.source)
	switch w.source {
	case "binance":
		w.runBinance(ctx)
	default:
		w.runMock(ctx)
	}
}

func (w *Worker) runBinance(ctx context.Context) {
	out := make(chan exchange.NormalizedPriceEvent, 512)
	adapter := &exchange.BinanceAdapter{
		Endpoint: binanceEndpoint,
		Out:      out,
	}
	go adapter.Start(ctx)
	log.Println("[BG Worker] Binance feed connected, feeding analytics engines...")

	for {
		select {
		case <-ctx.Done():
			log.Println("[BG Worker] shutting down")
			return
		case evt := <-out:
			id, ok := trading.SymbolToID[evt.Instrument]
			if !ok {
				continue
			}
			w.registry.Feed(analytics.PriceUpdateEvent{
				InstrumentID: id,
				Price:        evt.Price,
				Timestamp:    time.Now().UnixNano(),
			})
		}
	}
}

func (w *Worker) runMock(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	log.Println("[BG Worker] mock feed running, feeding analytics engines...")

	basePrices := map[int]float64{
		101: 60000, 102: 3000, 103: 300, 104: 0.5,
		105: 150, 106: 0.4, 107: 0.08, 108: 0.7,
		109: 80, 110: 7, 111: 35, 112: 15,
		113: 8, 114: 9, 115: 0.12, 116: 25,
		117: 5, 118: 10, 119: 12, 120: 1.5,
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("[BG Worker] shutting down")
			return
		case <-ticker.C:
			for id, base := range basePrices {
				w.registry.Feed(analytics.PriceUpdateEvent{
					InstrumentID: id,
					Price:        base + rand.Float64()*base*0.01,
					Timestamp:    time.Now().UnixNano(),
				})
			}
		}
	}
}
