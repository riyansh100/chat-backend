package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/riyansh/chat-backend/internal/background"
	"github.com/riyansh/chat-backend/internal/hub"
	"github.com/riyansh/chat-backend/internal/metrics"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
	"github.com/riyansh/chat-backend/internal/ws"
)

func main() {
	// 1. Instance identity
	instanceID := uuid.NewString()
	log.Println("instanceID:", instanceID)

	// 2. Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6380",
	})

	// 3. Redis cache
	redisCache := chatredis.NewRedisCache(rdb, 30*time.Second)

	// 4. Postgres
	pgConnStr := "postgres://postgres:pwd@localhost:5432/marketdata?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), pgConnStr)
	if err != nil {
		log.Fatal("postgres connect failed:", err)
	}
	defer pool.Close()
	log.Println("Postgres connected")

	// 5. Hub
	h := hub.NewHub(instanceID, redisCache, rdb, pool)

	ctx := context.Background()
	hub.StartRedisSubscriber(ctx, rdb, h)
	go h.Run()

	// 6. Background analytics worker
	// Feeds SMA + OHLC engines directly — no consumer needs to be connected.
	// FEED_SOURCE=binance (default) or FEED_SOURCE=mock
	feedSource := os.Getenv("FEED_SOURCE")
	if feedSource == "" {
		feedSource = "binance"
	}
	bgWorker := background.NewWorker(feedSource, h.SMAEngine(), h.OHLCEngine())
	go bgWorker.Start(ctx)
	log.Printf("Background analytics worker started (source=%s)", feedSource)

	// 7. WebSocket handlers
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(h, w, r)
	})
	http.HandleFunc("/ws/ingest", ws.IngestHandler(h))

	// 8. Metrics endpoint — hit localhost:8080/metrics to see live stats
	http.HandleFunc("/metrics", metrics.Handler(
		h.Metrics,
		func() int { return h.SMAEngine().InputLen() },
		func() int { return h.OHLCEngine().InputLen() },
	))

	// 9. Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Println("Server started on :" + port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
