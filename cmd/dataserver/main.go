// cmd/dataserver/main.go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"time"

	"github.com/riyansh/chat-backend/internal/background"
	"github.com/riyansh/chat-backend/internal/config"
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
	cfg := config.Load()
	cfg.Print()

	instanceID := uuid.NewString()
	log.Println("[DataServer] instanceID:", instanceID)

	ctx := context.Background()

	// ---- Redis clients (addresses from config) ----
	pair1Primary := chatredis.NewPair1PrimaryClient(cfg)
	pair1Replica := chatredis.NewPair1ReplicaClient(cfg)
	defer pair1Primary.Close()
	defer pair1Replica.Close()

	pair2Primary := chatredis.NewPair2PrimaryClient(cfg)
	pair2Replica := chatredis.NewPair2ReplicaClient(cfg)
	defer pair2Primary.Close()
	defer pair2Replica.Close()

	pingNode(ctx, pair1Primary, "pair1-primary("+cfg.Pair1PrimaryAddr+")")
	pingNode(ctx, pair1Replica, "pair1-replica("+cfg.Pair1ReplicaAddr+")")
	pingNode(ctx, pair2Primary, "pair2-primary("+cfg.Pair2PrimaryAddr+")")
	pingNode(ctx, pair2Replica, "pair2-replica("+cfg.Pair2ReplicaAddr+")")

	lb := chatredis.NewRedisLoadBalancer(pair1Primary, pair1Replica, pair2Primary, pair2Replica)

	redisCache := chatredis.NewRedisCache(chatredis.NewSentinelUniversalClient(cfg), 30*time.Second)

	// ---- Postgres ----
	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatal("postgres connect failed:", err)
	}
	defer pool.Close()
	log.Println("[DataServer] Postgres connected")

	// ---- NATS ----
	nc, err := internalnats.Connect(cfg.NATSURL)
	if err != nil {
		log.Fatal("[DataServer] NATS connect failed:", err)
	}
	defer nc.Drain()

	// ---- HTTP ----
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
		log.Println("[DataServer] HTTP listening on :" + cfg.DataPort)
		if err := http.ListenAndServe(":"+cfg.DataPort, nil); err != nil {
			log.Fatal(err)
		}
	}()

	// ---- leader election ----
	elect := leader.NewElection(pair1Primary, instanceID)

	elect.Run(ctx,
		func(leaderCtx context.Context) {
			log.Println("[DataServer] elected as leader — starting engines")

			h := hub.NewHub(instanceID, redisCache, pair1Primary, lb, pool, nc)
			hubRef = h
			go h.Run()

			bgWorker := background.NewWorker(cfg.FeedSource, h.Registry())
			go bgWorker.Start(leaderCtx)
			log.Printf("[DataServer] analytics worker started (source=%s)", cfg.FeedSource)

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
