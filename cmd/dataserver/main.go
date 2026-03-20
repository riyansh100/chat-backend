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
	"github.com/riyansh/chat-backend/internal/history"
	"github.com/riyansh/chat-backend/internal/hub"
	"github.com/riyansh/chat-backend/internal/metrics"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

func main() {
	instanceID := uuid.NewString()
	log.Println("[DataServer] instanceID:", instanceID)

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6380"})
	redisCache := chatredis.NewRedisCache(rdb, 30*time.Second)

	pgConnStr := "postgres://postgres:pwd@localhost:5432/marketdata?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), pgConnStr)
	if err != nil {
		log.Fatal("postgres connect failed:", err)
	}
	defer pool.Close()
	log.Println("[DataServer] Postgres connected")

	// Hub runs engines + publishes to analytics:events
	h := hub.NewHub(instanceID, redisCache, rdb, pool)
	ctx := context.Background()

	go h.Run()

	feedSource := os.Getenv("FEED_SOURCE")
	if feedSource == "" {
		feedSource = "binance"
	}

	bgWorker := background.NewWorker(feedSource, h.Registry())
	go bgWorker.Start(ctx)
	log.Printf("[DataServer] analytics worker started (source=%s)", feedSource)

	// History store + rolling hourly rollup
	histStore := history.NewStore(rdb, pool)
	go history.StartRollupJob(ctx, histStore)
	log.Println("[DataServer] hourly rollup job started")

	// Metrics endpoint only — no /ws on data server
	http.HandleFunc("/metrics", metrics.Handler(
		h.Metrics,
		func() int { return h.SMAEngine().InputLen() },
		func() int { return h.OHLCEngine().InputLen() },
		func() int { return h.EMAEngine().InputLen() },
		func() int { return h.BBEngine().InputLen() },
		func() int { return h.RSIEngine().InputLen() },
		func() int { return h.MACDEngine().InputLen() },
	))

	port := os.Getenv("DATA_PORT")
	if port == "" {
		port = "8081"
	}
	log.Println("[DataServer] started on :" + port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
