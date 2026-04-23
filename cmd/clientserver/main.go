// cmd/clientserver/main.go
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"

	"github.com/riyansh/chat-backend/internal/auth"
	"github.com/riyansh/chat-backend/internal/config"
	"github.com/riyansh/chat-backend/internal/history"
	"github.com/riyansh/chat-backend/internal/hub"
	internalnats "github.com/riyansh/chat-backend/internal/nats"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
	"github.com/riyansh/chat-backend/internal/ws"
)

// natsWorkers is the number of goroutines draining the JetStream consumer.
// 16 workers keeps the NATS consumer backlog near-zero at 20k msg/s while
// leaving plenty of cores for WS read/write pumps.
const natsWorkers = 16

func pingNode(ctx context.Context, c *redis.Client, name string) {
	if err := c.Ping(ctx).Err(); err != nil {
		log.Printf("[ClientServer] %s ping FAILED (%v) — lb will route around it", name, err)
	} else {
		log.Printf("[ClientServer] %s connected", name)
	}
}

func main() {
	cfg := config.Load()
	cfg.Print()

	instanceID := uuid.NewString()
	log.Println("[ClientServer] instanceID:", instanceID)

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
	pgCfg, _ := pgxpool.ParseConfig(cfg.PostgresDSN)
	pgCfg.MaxConns = cfg.PGMaxConns
	pgCfg.MinConns = cfg.PGMinConns
	pool, err := pgxpool.NewWithConfig(ctx, pgCfg)
	if err != nil {
		log.Fatal("postgres connect failed:", err)
	}
	defer pool.Close()
	log.Println("[ClientServer] Postgres connected")

	// ---- NATS ----
	nc, err := internalnats.Connect(cfg.NATSURL)
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
	sessionStore := auth.NewSessionStore(pair2Primary, lb)
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

	log.Println("[ClientServer] started on :" + cfg.ClientPort)
	if err := http.ListenAndServe(":"+cfg.ClientPort, nil); err != nil {
		log.Fatal(err)
	}
}

// consumeAnalyticsEvents creates a durable JetStream consumer and fans out
// each analytics event to the hub using natsWorkers parallel goroutines.
//
// Binary frames: the dataserver now publishes raw binary frames (22–46 bytes)
// instead of JSON envelopes (~80–120 bytes). Workers call
// hub.DecodeFrameToMessage which decodes the binary frame and returns a
// hub.Message ready to push to WebSocket clients (which still receive JSON).
//
// Acking before processing is intentional — stale market ticks are worse
// than dropped ones.
func consumeAnalyticsEvents(ctx context.Context, nc *nats.Conn, h *hub.Hub) {
	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatalf("[ClientServer] jetstream init: %v", err)
	}

	consumer, err := js.CreateOrUpdateConsumer(ctx, internalnats.StreamName, jetstream.ConsumerConfig{
		Name:          internalnats.ConsumerName,
		Durable:       internalnats.ConsumerName,
		DeliverPolicy: jetstream.DeliverNewPolicy,
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
	log.Printf("[ClientServer] NATS JetStream consumer started (%d workers) on %s",
		natsWorkers, internalnats.StreamSubject)

	// workCh buffers raw NATS messages before workers pick them up.
	// Sized to 2× MaxAckPending so the pull loop is never blocked by workers.
	workCh := make(chan jetstream.Msg, 8192)

	// Parallel fanout workers decode binary frames → hub.Messages.
	for i := 0; i < natsWorkers; i++ {
		go func() {
			for msg := range workCh {
				room, hubMsg, err := hub.DecodeFrameToMessage(msg.Data())
				if err != nil {
					log.Printf("[NATS worker] decode error: %v", err)
					continue
				}

				// Push model: broadcast to everyone in the room
				h.Broadcast <- hub.BroadcastEvent{
					Room:    room,
					Origin:  "",
					Message: hubMsg,
				}

				// Pull model: fan out to topic subscribers
				topic := hubMsg.Type[:len(hubMsg.Type)-len("_update")] + ":" + room
				h.SubManager().Fanout(topic, hubMsg)
			}
		}()
	}

	// Main loop: pull + ack immediately, dispatch to workers.
	for {
		msg, err := msgs.Next()
		if err != nil {
			log.Printf("[ClientServer] NATS consumer stopped: %v", err)
			close(workCh)
			return
		}
		msg.Ack()

		select {
		case workCh <- msg:
		default:
			// workCh full — workers temporarily saturated.
			// Drop: live price ticks are superseded by the next one anyway.
		}
	}
}
