package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

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

	m := &metrics.HubMetrics{}
	m.StartLogger()

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

	sma := analytics.NewEngine(20)
	go sma.Run()
	hub.smaEngine = sma

	var smaStore *analytics.SMAStore
	if hub.RedisClient != nil {
		smaStore = analytics.NewSMAStore(hub.RedisClient)
		hub.smaStore = smaStore
	}

	// smaToHub routes SMA events through the Hub's Broadcast channel.
	// This is required because Hub owns h.Rooms — reading it directly
	// from another goroutine is a data race.
	smaToHub := func(smaEvent analytics.SMAUpdateEvent) {
		roomName := strconv.Itoa(smaEvent.InstrumentID)
		data, _ := json.Marshal(map[string]interface{}{
			"instrument_id": smaEvent.InstrumentID,
			"value":         smaEvent.Value,
			"timestamp":     smaEvent.Timestamp,
			"resolution":    smaEvent.Resolution,
		})
		hub.Broadcast <- BroadcastEvent{
			Room:   roomName,
			Origin: hub.InstanceID,
			Message: Message{
				Type: "sma_update",
				Data: json.RawMessage(data),
			},
		}
	}

	// 1s: broadcast + persist
	go func() {
		for smaEvent := range hub.smaEngine.Output() {
			smaToHub(smaEvent)
			if smaStore != nil {
				if err := smaStore.Write(context.Background(), smaEvent); err != nil {
					log.Printf("SMA 1s write error (instrument %d): %v", smaEvent.InstrumentID, err)
				}
			}
		}
	}()

	// 1m: broadcast + persist
	go func() {
		for smaEvent := range hub.smaEngine.OutputMin() {
			fmt.Printf("[1m] smaToHub called instrument=%d value=%.4f\n", smaEvent.InstrumentID, smaEvent.Value)
			smaToHub(smaEvent)
			if smaStore != nil {
				if err := smaStore.Write(context.Background(), smaEvent); err != nil {
					log.Printf("SMA 1m write error (instrument %d): %v", smaEvent.InstrumentID, err)
				}
			}
		}
	}()

	return hub
}
