package main

import (
	"context"
	"log"
	"net/http"
	"os"

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
	// 2. Hub
	// ------------------------------------------------
	h := hub.NewHub(instanceID)
	go h.Run()

	// ------------------------------------------------
	// 3. Redis
	// ------------------------------------------------
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	h.RedisClient = rdb

	ctx := context.Background()
	hub.StartRedisSubscriber(ctx, rdb, h)

	// ------------------------------------------------
	// 4. WebSocket handler
	// ------------------------------------------------
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(h, w, r)
	})

	// ------------------------------------------------
	// 5. Configurable port (KEY CHANGE)
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
