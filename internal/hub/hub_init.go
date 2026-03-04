package hub

import (
	"github.com/riyansh/chat-backend/internal/analytics"
	"github.com/riyansh/chat-backend/internal/cache"
	"github.com/riyansh/chat-backend/internal/metrics"
	"github.com/riyansh/chat-backend/internal/redis"
)

func NewHub(instanceID string, redisCache redis.Cache) *Hub {
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

		Rooms:      make(map[string]*Room),
		redisCache: redisCache,
		l1:         l1Cache,

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

	// Listen for SMA output and rebroadcast
	go func() {
		for smaEvent := range hub.smaEngine.Output() {
			hub.broadcastSMA(smaEvent)
		}
	}()

	return hub
}
