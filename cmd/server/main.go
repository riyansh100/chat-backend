package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	chatredis "github.com/riyansh/chat-backend/internal/redis"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/riyansh/chat-backend/internal/hub"
	"github.com/riyansh/chat-backend/internal/ws"
)

func main() {
	// ------------------------------------------------
	// 1. Instance identity
	// ------------------------------------------------
	instanceID := uuid.NewString()
	log.Println("instanceID:", instanceID)

	// ------------------------------------------------
	// 2. Redis client (CREATE FIRST)
	// ------------------------------------------------
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// ------------------------------------------------
	// 3. Redis in-memory cache (KV)
	// ------------------------------------------------
	redisCache := chatredis.NewRedisCache(rdb, 30*time.Second)

	// ------------------------------------------------
	// 4. Hub
	// ------------------------------------------------
	h := hub.NewHub(instanceID, redisCache)
	h.RedisClient = rdb

	ctx := context.Background()
	hub.StartRedisSubscriber(ctx, rdb, h)

	// Start Hub loop
	go h.Run()

	// ------------------------------------------------
	// 5. WebSocket handlers
	// ------------------------------------------------
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(h, w, r)
	})

	http.HandleFunc("/ws/ingest", ws.IngestHandler(h))

	// ------------------------------------------------
	// 6. Server
	// ------------------------------------------------
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Server started on :" + port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
