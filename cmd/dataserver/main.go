// cmd/dataserver/main.go
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
	internalnats "github.com/riyansh/chat-backend/internal/nats"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

func pingNode(ctx context.Context, c *redis.Client, name string) {
	if err := c.Ping(ctx).Err(); err != nil {
		log.Printf("[DataServer] %s ping FAILED (%v) — lb will route around it", name, err)
	} else {
		log.Printf("[DataServer] %s connected", name)
	}
}

func main() {
	instanceID := uuid.NewString()
	log.Println("[DataServer] instanceID:", instanceID)

	ctx := context.Background()

	// ---- pair 1: primary :6381 (sentinel :26380), replica :6380 ----
	pair1Primary := chatredis.NewPair1PrimaryClient()
	pair1Replica := chatredis.NewPair1ReplicaClient()
	defer pair1Primary.Close()
	defer pair1Replica.Close()

	// ---- pair 2: primary :6383 (sentinel :26381), replica :6382 ----
	pair2Primary := chatredis.NewPair2PrimaryClient()
	pair2Replica := chatredis.NewPair2ReplicaClient()
	defer pair2Primary.Close()
	defer pair2Replica.Close()

	pingNode(ctx, pair1Primary, "pair1-primary(:6381)")
	pingNode(ctx, pair1Replica, "pair1-replica(:6380)")
	pingNode(ctx, pair2Primary, "pair2-primary(:6383)")
	pingNode(ctx, pair2Replica, "pair2-replica(:6382)")

	// load balancer: writes->least-loaded primary, reads->scatter-gather replicas
	lb := chatredis.NewRedisLoadBalancer(pair1Primary, pair1Replica, pair2Primary, pair2Replica)

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

	// ---- NATS ----
	natsURL := os.Getenv("NATS_URL") // defaults to nats://localhost:4222
	nc, err := internalnats.Connect(natsURL)
	if err != nil {
		log.Fatal("[DataServer] NATS connect failed:", err)
	}
	defer nc.Drain()

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

	http.HandleFunc("/redis-status", func(w http.ResponseWriter, r *http.Request) {
		lb.LogStatus()
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("redis lb status logged to stdout\n"))
	})

	go func() {
		log.Println("[DataServer] HTTP listening on :" + port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Fatal(err)
		}
	}()

	elect := leader.NewElection(pair1Primary, instanceID)

	elect.Run(ctx,
		func(leaderCtx context.Context) {
			log.Println("[DataServer] elected as leader — starting engines")

			h := hub.NewHub(instanceID, redisCache, pair1Primary, lb, pool, nc)
			hubRef = h
			go h.Run()

			feedSource := os.Getenv("FEED_SOURCE")
			if feedSource == "" {
				feedSource = "binance"
			}
			bgWorker := background.NewWorker(feedSource, h.Registry())
			go bgWorker.Start(leaderCtx)
			log.Printf("[DataServer] analytics worker started (source=%s)", feedSource)

			histStore := history.NewStore(lb, pool)
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
