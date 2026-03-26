package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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

	// write client — primary via sentinel, auto-follows on failover
	rdb := chatredis.NewSentinelClient()
	defer rdb.Close()

	// read client — replica direct, falls back to primary on error
	replicaRDB := chatredis.NewReplicaClient()
	defer replicaRDB.Close()

	readRDB := chatredis.NewSafeReadClient(replicaRDB, rdb)

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("[DataServer] Redis primary ping failed:", err)
	}
	log.Println("[DataServer] Redis primary (write) connected")

	if err := replicaRDB.Ping(ctx).Err(); err != nil {
		log.Printf("[DataServer] Redis replica ping failed (%v) — reads will fall back to primary", err)
	} else {
		log.Println("[DataServer] Redis replica (read) connected")
	}

	redisCache := chatredis.NewRedisCache(chatredis.NewSentinelUniversalClient(), 30*time.Second)

	pgConnStr := "postgres://postgres:pwd@localhost:5432/marketdata?sslmode=disable"
	pool, err := pgxpool.New(ctx, pgConnStr)
	if err != nil {
		log.Fatal("postgres connect failed:", err)
	}
	defer pool.Close()
	log.Println("[DataServer] Postgres connected")

	port := os.Getenv("DATA_PORT")
	if port == "" {
		port = "8081"
	}

	var hubRef *hub.Hub

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if hubRef == nil {
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

	go func() {
		log.Println("[DataServer] HTTP listening on :" + port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Fatal(err)
		}
	}()

	elect := leader.NewElection(rdb, instanceID)

	elect.Run(ctx,

		func(leaderCtx context.Context) {
			log.Println("[DataServer] elected as leader — starting engines")

			h := hub.NewHub(instanceID, redisCache, rdb, readRDB, pool)
			hubRef = h

			go h.Run()

			feedSource := os.Getenv("FEED_SOURCE")
			if feedSource == "" {
				feedSource = "binance"
			}

			bgWorker := background.NewWorker(feedSource, h.Registry())
			go bgWorker.Start(leaderCtx)
			log.Printf("[DataServer] analytics worker started (source=%s)", feedSource)

			histStore := history.NewStore(rdb, readRDB, pool)
			go history.StartRollupJob(leaderCtx, histStore)
			log.Println("[DataServer] hourly rollup job started")

			<-leaderCtx.Done()
			log.Println("[DataServer] leader context cancelled — engines stopping")
			hubRef = nil
		},

		func() {
			log.Println("[DataServer] leadership revoked — entering standby mode")
			hubRef = nil
		},
	)
}
