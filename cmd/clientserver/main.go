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

func main() {
	instanceID := uuid.NewString()
	log.Println("[ClientServer] instanceID:", instanceID)

	// Sentinel-aware client — follows primary on failover automatically
	rdb := chatredis.NewSentinelClient()
	defer rdb.Close()

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("[ClientServer] Redis sentinel ping failed:", err)
	}
	log.Println("[ClientServer] Redis (sentinel) connected")

	redisCache := chatredis.NewRedisCache(rdb, 30*time.Second)

	pgConnStr := "postgres://postgres:pwd@localhost:5432/marketdata?sslmode=disable"
	pool, err := pgxpool.New(ctx, pgConnStr)
	if err != nil {
		log.Fatal("postgres connect failed:", err)
	}
	defer pool.Close()
	log.Println("[ClientServer] Postgres connected")

	h := hub.NewClientHub(instanceID, redisCache, rdb, pool)

	go subscribeAnalyticsEvents(ctx, rdb, h)
	hub.StartRedisSubscriber(ctx, rdb, h)

	go h.Run()
	log.Println("[ClientServer] hub running (no engines)")

	histStore := history.NewStore(rdb, pool)
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
