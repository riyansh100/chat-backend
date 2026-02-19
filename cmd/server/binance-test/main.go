package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/riyansh/chat-backend/feed-adapter/exchange"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// CTRL+C handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	out := make(chan exchange.NormalizedPriceEvent, 100)

	adapter := &exchange.BinanceAdapter{
		Endpoint: "wss://stream.binance.com:9443/ws/btcusdt@trade",
		Out:      out,
	}

	go adapter.Start(ctx)

	fmt.Println("🚀 Binance adapter running...")

	for {
		select {
		case <-sigCh:
			fmt.Println("\n🛑 stopping...")
			return

		case evt := <-out:
			fmt.Printf("📈 %s | %.2f | %d\n", evt.Instrument, evt.Price, evt.Timestamp)
		}
	}
}
