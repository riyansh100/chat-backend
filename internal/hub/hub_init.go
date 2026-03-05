package hub

import (
	"context"
	"log"

	goredis "github.com/redis/go-redis/v9"
	"github.com/riyansh/chat-backend/internal/analytics"
	"github.com/riyansh/chat-backend/internal/cache"
	"github.com/riyansh/chat-backend/internal/metrics"
	"github.com/riyansh/chat-backend/internal/redis"
)

func NewHub(instanceID string, redisCache redis.Cache, rdb *goredis.Client) *Hub {
	l1Cache, err := cache.NewL1Cache()
	if err != nil {
		panic(err)
	}

	// Initialize Metrics
	m := &metrics.HubMetrics{}
	m.StartLogger()

	// Create Hub FIRST
	hub := &Hub{
		InstanceID: instanceID,

		Rooms:       make(map[string]*Room),
		redisCache:  redisCache,
		l1:          l1Cache,
		RedisClient: rdb,

		Metrics: m,

		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		JoinRoom:   make(chan JoinRoomEvent),
		LeaveRoom:  make(chan LeaveRoomEvent),
		Broadcast:  make(chan BroadcastEvent),
	}

	// Initialize SMA Engine
	sma := analytics.NewEngine(20)
	go sma.Run()

	hub.smaEngine = sma

	// Initialize SMA Redis store
	var smaStore *analytics.SMAStore
	if hub.RedisClient != nil {
		smaStore = analytics.NewSMAStore(hub.RedisClient)
		hub.smaStore = smaStore
	}

	// Listen for SMA output: rebroadcast to live clients + persist to Redis
	go func() {
		for smaEvent := range hub.smaEngine.Output() {
			hub.broadcastSMA(smaEvent)

			if smaStore != nil {
				if err := smaStore.Write(context.Background(), smaEvent); err != nil {
					log.Printf("SMA store write error (instrument %d): %v", smaEvent.InstrumentID, err)
				}
			}
		}
	}()

	return hub
}
