package hub

import (
	"context"
	"encoding/json"
	"log"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/riyansh/chat-backend/internal/analytics"
	"github.com/riyansh/chat-backend/internal/cache"
	"github.com/riyansh/chat-backend/internal/metrics"
	"github.com/riyansh/chat-backend/internal/redis"
)

func NewHub(instanceID string, redisCache redis.Cache, rdb *goredis.Client, pool *pgxpool.Pool) *Hub {
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
		pgPool:      pool,

		Metrics: m,

		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		JoinRoom:   make(chan JoinRoomEvent),
		LeaveRoom:  make(chan LeaveRoomEvent),
		Broadcast:  make(chan BroadcastEvent),
	}

	// SMA engine
	sma := analytics.NewEngine(20)
	go sma.Run()
	hub.smaEngine = sma

	var smaStore *analytics.SMAStore
	if hub.RedisClient != nil {
		smaStore = analytics.NewSMAStore(hub.RedisClient)
		hub.smaStore = smaStore
	}

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

	go func() {
		for smaEvent := range hub.smaEngine.OutputMin() {
			smaToHub(smaEvent)
			if smaStore != nil {
				if err := smaStore.Write(context.Background(), smaEvent); err != nil {
					log.Printf("SMA 1m write error (instrument %d): %v", smaEvent.InstrumentID, err)
				}
			}
		}
	}()

	// OHLC engine
	ohlc := analytics.NewOHLCEngine()
	go ohlc.Run()
	hub.ohlcEngine = ohlc

	var ohlcStore *analytics.OHLCStore
	if hub.RedisClient != nil && hub.pgPool != nil {
		ohlcStore = analytics.NewOHLCStore(hub.RedisClient, hub.pgPool)
		hub.ohlcStore = ohlcStore
	}

	// OHLC: broadcast to live clients + persist to Redis and Postgres
	go func() {
		for ohlcEvent := range hub.ohlcEngine.Output() {
			roomName := strconv.Itoa(ohlcEvent.InstrumentID)
			data, _ := json.Marshal(map[string]interface{}{
				"instrument_id": ohlcEvent.InstrumentID,
				"resolution":    ohlcEvent.Resolution,
				"open":          ohlcEvent.Open,
				"high":          ohlcEvent.High,
				"low":           ohlcEvent.Low,
				"close":         ohlcEvent.Close,
				"timestamp":     ohlcEvent.Timestamp,
			})
			hub.Broadcast <- BroadcastEvent{
				Room:   roomName,
				Origin: hub.InstanceID,
				Message: Message{
					Type: "ohlc_update",
					Data: json.RawMessage(data),
				},
			}

			if ohlcStore != nil {
				if err := ohlcStore.Write(context.Background(), ohlcEvent); err != nil {
					log.Printf("OHLC write error (instrument %d): %v", ohlcEvent.InstrumentID, err)
				}
			}
		}
	}()

	return hub
}
