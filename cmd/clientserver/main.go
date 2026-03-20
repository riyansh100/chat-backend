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

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6380"})
	redisCache := chatredis.NewRedisCache(rdb, 30*time.Second)

	pgConnStr := "postgres://postgres:pwd@localhost:5432/marketdata?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), pgConnStr)
	if err != nil {
		log.Fatal("postgres connect failed:", err)
	}
	defer pool.Close()
	log.Println("[ClientServer] Postgres connected")

	// Hub with NO engines running — client-only mode
	h := hub.NewClientHub(instanceID, redisCache, rdb, pool)
	ctx := context.Background()

	// Subscribe to analytics:events published by data server
	go subscribeAnalyticsEvents(ctx, rdb, h)

	// Also subscribe to existing chat:events for Redis pub/sub fan-out
	hub.StartRedisSubscriber(ctx, rdb, h)

	go h.Run()
	log.Println("[ClientServer] hub running (no engines)")

	// History endpoint
	histStore := history.NewStore(rdb, pool)
	http.HandleFunc("/history", history.Handler(histStore))

	// WebSocket endpoints
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

// subscribeAnalyticsEvents listens on analytics:events (published by data server)
// and fans out each message to the appropriate hub room + pull subscribers.
func subscribeAnalyticsEvents(ctx context.Context, rdb *redis.Client, h *hub.Hub) {
	sub := rdb.Subscribe(ctx, "analytics:events")
	log.Println("[ClientServer] subscribed to analytics:events")

	for msg := range sub.Channel() {
		var env struct {
			Room   string          `json:"room"`
			Type   string          `json:"type"`
			Data   json.RawMessage `json:"data"`
			Topic  string          `json:"topic"` // pull topic e.g. "sma:101"
			Origin string          `json:"origin"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
			continue
		}

		hubMsg := hub.Message{Type: env.Type, Data: env.Data}

		// push path — broadcast to room
		if env.Room != "" {
			h.Broadcast <- hub.BroadcastEvent{
				Room:    env.Room,
				Origin:  env.Origin,
				Message: hubMsg,
			}
		}

		// pull path — fanout to topic subscribers
		if env.Topic != "" {
			h.SubManager().Fanout(env.Topic, hubMsg)
		}
	}
}
