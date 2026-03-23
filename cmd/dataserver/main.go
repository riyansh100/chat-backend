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
	"github.com/riyansh/chat-backend/internal/leader"
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

	port := os.Getenv("DATA_PORT")
	if port == "" {
		port = "8081"
	}

	// ---- metrics placeholder (populated once we become leader) ----
	// We always expose /metrics so nginx upstream health checks work,
	// but engine input lens will be 0 on a standby instance.
	var (
		hubRef     *hub.Hub
		metricsReg = &metricsRegistry{}
	)

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if hubRef == nil {
			// standby — return minimal valid JSON
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"role":"standby"}`))
			return
		}
		metrics.Handler(
			hubRef.Metrics,
			func() int { return hubRef.SMAEngine().InputLen() },
			func() int { return hubRef.OHLCEngine().InputLen() },
			func() int { return hubRef.EMAEngine().InputLen() },
			func() int { return hubRef.BBEngine().InputLen() },
			func() int { return hubRef.RSIEngine().InputLen() },
			func() int { return hubRef.MACDEngine().InputLen() },
		)(w, r)
	})
	_ = metricsReg // suppress unused warning

	// ---- start HTTP server immediately (before election) ----
	// This lets nginx upstream health checks pass even on standby.
	go func() {
		log.Println("[DataServer] HTTP listening on :" + port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Fatal(err)
		}
	}()

	// ---- leader election ----
	ctx := context.Background()
	elect := leader.NewElection(rdb, instanceID)

	elect.Run(ctx,

		// ---- onElected: called when we win the lease ----
		func(leaderCtx context.Context) {
			log.Println("[DataServer] elected as leader — starting engines")

			h := hub.NewHub(instanceID, redisCache, rdb, pool)
			hubRef = h

			go h.Run()

			feedSource := os.Getenv("FEED_SOURCE")
			if feedSource == "" {
				feedSource = "binance"
			}

			bgWorker := background.NewWorker(feedSource, h.Registry())
			go bgWorker.Start(leaderCtx)
			log.Printf("[DataServer] analytics worker started (source=%s)", feedSource)

			histStore := history.NewStore(rdb, pool)
			go history.StartRollupJob(leaderCtx, histStore)
			log.Println("[DataServer] hourly rollup job started")

			// block until leadership context is cancelled
			<-leaderCtx.Done()
			log.Println("[DataServer] leader context cancelled — engines stopping")
			hubRef = nil
		},

		// ---- onRevoked: called if we unexpectedly lose the lease ----
		func() {
			log.Println("[DataServer] leadership revoked — entering standby mode")
			hubRef = nil
		},
	)
}

// metricsRegistry is a placeholder to keep the import clean.
type metricsRegistry struct{}
