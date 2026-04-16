// cmd/clientserver/main.go
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
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"

	"github.com/riyansh/chat-backend/internal/auth"
	"github.com/riyansh/chat-backend/internal/history"
	"github.com/riyansh/chat-backend/internal/hub"
	internalnats "github.com/riyansh/chat-backend/internal/nats"
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

	lb := chatredis.NewRedisLoadBalancer(pair1Primary, pair1Replica, pair2Primary, pair2Replica)

	redisCache := chatredis.NewRedisCache(chatredis.NewSentinelUniversalClient(), 30*time.Second)

	pgConnStr := "postgres://postgres:pwd@localhost:5432/marketdata?sslmode=disable"
	cfg, _ := pgxpool.ParseConfig(pgConnStr)
	cfg.MaxConns = 50 // Fix 3: was 20 — saturated under 200 concurrent logins (bcrypt ~300ms each)
	cfg.MinConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		log.Fatal("postgres connect failed:", err)
	}
	defer pool.Close()
	log.Println("[ClientServer] Postgres connected")

	// ---- NATS ----
	natsURL := os.Getenv("NATS_URL") // defaults to nats://localhost:4222
	nc, err := internalnats.Connect(natsURL)
	if err != nil {
		log.Fatal("[ClientServer] NATS connect failed:", err)
	}
	defer nc.Drain()

	h := hub.NewClientHub(instanceID, redisCache, pair1Primary, lb, pool, nc)

	hub.StartRedisSubscriber(ctx, pair1Primary, h)

	go consumeAnalyticsEvents(ctx, nc, h)
	go h.Run()
	log.Println("[ClientServer] hub running (no engines)")

	// ---- history ----
	histStore := history.NewStore(lb, pool)
	http.HandleFunc("/history", history.Handler(histStore))

	// ---- auth ----
	authStore := auth.NewStore(pool)
	sessionStore := auth.NewSessionStore(pair2Primary, lb) // Fix 2: lb for scatter-gather reads
	authHandler := auth.NewHandler(authStore, histStore, lb, sessionStore)

	// public
	http.HandleFunc("/login", authHandler.Login)
	http.HandleFunc("/register", authHandler.Register)

	// protected
	http.HandleFunc("/logout", auth.AuthMiddleware(sessionStore, authHandler.Logout))
	http.HandleFunc("/subscribe", auth.AuthMiddleware(sessionStore, authHandler.Subscribe))
	http.HandleFunc("/unsubscribe", auth.AuthMiddleware(sessionStore, authHandler.Unsubscribe))
	http.HandleFunc("/subscriptions", auth.AuthMiddleware(sessionStore, authHandler.GetSubscriptions))

	// websocket
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(h, sessionStore, w, r)
	})

	port := os.Getenv("CLIENT_PORT")
	if port == "" {
		port = "8080"
	}
	log.Println("[ClientServer] started on :" + port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

// consumeAnalyticsEvents is the replacement for the old subscribeAnalyticsEvents.
// It creates a durable push consumer on the ANALYTICS stream and fans out each
// event to the hub's Broadcast channel and topic subscriptions — identical
// behaviour to the Redis pub/sub path, just over NATS JetStream.
func consumeAnalyticsEvents(ctx context.Context, nc *nats.Conn, h *hub.Hub) {
	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatalf("[ClientServer] jetstream init: %v", err)
	}

	consumer, err := js.CreateOrUpdateConsumer(ctx, internalnats.StreamName, jetstream.ConsumerConfig{
		Name:          internalnats.ConsumerName,
		Durable:       internalnats.ConsumerName,
		DeliverPolicy: jetstream.DeliverNewPolicy, // only live events; no replay on restart
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: internalnats.StreamSubject,
		MaxAckPending: 4096,
	})
	if err != nil {
		log.Fatalf("[ClientServer] create consumer: %v", err)
	}

	msgs, err := consumer.Messages()
	if err != nil {
		log.Fatalf("[ClientServer] consumer.Messages: %v", err)
	}
	log.Println("[ClientServer] NATS JetStream consumer started on", internalnats.StreamSubject)

	for {
		msg, err := msgs.Next()
		if err != nil {
			// Context cancelled or connection closed — exit cleanly.
			log.Printf("[ClientServer] NATS consumer stopped: %v", err)
			return
		}
		msg.Ack()

		var env struct {
			Room   string          `json:"room"`
			Type   string          `json:"type"`
			Data   json.RawMessage `json:"data"`
			Topic  string          `json:"topic"`
			Origin string          `json:"origin"`
		}
		if err := json.Unmarshal(msg.Data(), &env); err != nil {
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
