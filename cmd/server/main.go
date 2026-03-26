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
	instanceID := uuid.NewString()
	log.Println("instanceID:", instanceID)

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6380"})
	redisCache := chatredis.NewRedisCache(rdb, 30*time.Second)
	safeReadClient := chatredis.NewSafeReadClient(rdb, rdb)

	pgConnStr := "postgres://postgres:pwd@localhost:5432/marketdata?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), pgConnStr)
	if err != nil {
		log.Fatal("postgres connect failed:", err)
	}
	defer pool.Close()
	log.Println("Postgres connected")

	h := hub.NewHub(instanceID, redisCache, rdb, safeReadClient, pool)

	ctx := context.Background()
	hub.StartRedisSubscriber(ctx, rdb, h)
	go h.Run()

	feedSource := os.Getenv("FEED_SOURCE")
	if feedSource == "" {
		feedSource = "binance"
	}

	bgWorker := background.NewWorker(feedSource, h.Registry())
	go bgWorker.Start(ctx)
	log.Printf("Background analytics worker started (source=%s)", feedSource)

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(h, w, r)
	})
	http.HandleFunc("/ws/ingest", ws.IngestHandler(h))

	http.HandleFunc("/metrics", metrics.Handler(
		h.Metrics,
		func() int { return h.SMAEngine().InputLen() },
		func() int { return h.OHLCEngine().InputLen() },
		func() int { return h.EMAEngine().InputLen() },
		func() int { return h.BBEngine().InputLen() },
		func() int { return h.RSIEngine().InputLen() },
		func() int { return h.MACDEngine().InputLen() },
	))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Println("Server started on :" + port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
