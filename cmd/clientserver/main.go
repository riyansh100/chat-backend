package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/riyansh/chat-backend/internal/history"
	"github.com/riyansh/chat-backend/internal/hub"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
	"github.com/riyansh/chat-backend/internal/ws"
)

func pingNode(ctx context.Context, c *redis.Client, name string) {
	if err := c.Ping(ctx).Err(); err != nil {
		log.Printf("[ClientServer] %s ping FAILED (%v) — lb will route around it", name, err)
	} else {
		log.Printf("[ClientServer] %s connected", name)
	}
}

func main() {
	instanceID := uuid.NewString()
	log.Println("[ClientServer] instanceID:", instanceID)

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

	// load balancer — writes to least-loaded primary, reads scatter-gather across replicas
	lb := chatredis.NewRedisLoadBalancer(pair1Primary, pair1Replica, pair2Primary, pair2Replica)

	redisCache := chatredis.NewRedisCache(chatredis.NewSentinelUniversalClient(), 30*time.Second)

	pgConnStr := "postgres://postgres:pwd@localhost:5432/marketdata?sslmode=disable"
	pool, err := pgxpool.New(ctx, pgConnStr)
	if err != nil {
		log.Fatal("postgres connect failed:", err)
	}
	defer pool.Close()
	log.Println("[ClientServer] Postgres connected")

	h := hub.NewClientHub(instanceID, redisCache, pair1Primary, lb, pool)

	go subscribeAnalyticsEvents(ctx, pair1Primary, h)
	hub.StartRedisSubscriber(ctx, pair1Primary, h)

	go h.Run()
	log.Println("[ClientServer] hub running (no engines)")

	histStore := history.NewStore(lb, pool)
	http.HandleFunc("/history", history.Handler(histStore))

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(h, w, r)
	})
	http.HandleFunc("/ws/ingest", ws.IngestHandler(h))

	port := os.Getenv("CLIENT_PORT")
	if port == "" {
		port = "8080"
	}
	log.Println("[ClientServer] started on :" + port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func subscribeAnalyticsEvents(ctx context.Context, rdb *redis.Client, h *hub.Hub) {
	sub := rdb.Subscribe(ctx, "analytics:events")
	log.Println("[ClientServer] subscribed to analytics:events")

	for msg := range sub.Channel() {
		var env struct {
			Room   string          `json:"room"`
			Type   string          `json:"type"`
			Data   json.RawMessage `json:"data"`
			Topic  string          `json:"topic"`
			Origin string          `json:"origin"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
			continue
		}

		hubMsg := hub.Message{Type: env.Type, Data: env.Data}

		if env.Room != "" {
			h.Broadcast <- hub.BroadcastEvent{
				Room:    env.Room,
				Origin:  env.Origin,
				Message: hubMsg,
			}
		}

		if env.Topic != "" {
			h.SubManager().Fanout(env.Topic, hubMsg)
		}
	}
}
