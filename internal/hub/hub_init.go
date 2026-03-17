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
		InstanceID:  instanceID,
		Rooms:       make(map[string]*Room),
		redisCache:  redisCache,
		l1:          l1Cache,
		RedisClient: rdb,
		pgPool:      pool,
		Metrics:     m,
		Register:    make(chan *Client),
		Unregister:  make(chan *Client),
		JoinRoom:    make(chan JoinRoomEvent),
		LeaveRoom:   make(chan LeaveRoomEvent),
		Broadcast:   make(chan BroadcastEvent),
	}

	// ---- Indicator registry ----
	hub.registry = analytics.NewRegistry()

	// ---- SMA engine ----
	sma := analytics.NewEngine(20)
	go sma.Run()
	hub.smaEngine = sma
	hub.registry.Register(sma) // register into feed path

	var smaStore *analytics.SMAStore
	if hub.RedisClient != nil {
		smaStore = analytics.NewSMAStore(hub.RedisClient, hub.pgPool)
		hub.smaStore = smaStore
	}

	smaToHub := func(e analytics.SMAUpdateEvent) {
		data, _ := json.Marshal(map[string]interface{}{
			"instrument_id": e.InstrumentID,
			"value":         e.Value,
			"timestamp":     e.Timestamp,
			"resolution":    e.Resolution,
		})
		hub.Broadcast <- BroadcastEvent{
			Room:    strconv.Itoa(e.InstrumentID),
			Origin:  hub.InstanceID,
			Message: Message{Type: "sma_update", Data: json.RawMessage(data)},
		}
	}

	go func() {
		for e := range hub.smaEngine.Output() {
			smaToHub(e)
			if smaStore != nil {
				if err := smaStore.Write(context.Background(), e); err != nil {
					log.Printf("SMA 1s write error (instrument %d): %v", e.InstrumentID, err)
				}
			}
		}
	}()
	go func() {
		for e := range hub.smaEngine.OutputMin() {
			smaToHub(e)
			if smaStore != nil {
				if err := smaStore.Write(context.Background(), e); err != nil {
					log.Printf("SMA 1m write error (instrument %d): %v", e.InstrumentID, err)
				}
			}
		}
	}()

	// ---- OHLC engine ----
	ohlc := analytics.NewOHLCEngine()
	go ohlc.Run()
	hub.ohlcEngine = ohlc
	hub.registry.Register(ohlc) // register into feed path

	var ohlcStore *analytics.OHLCStore
	if hub.RedisClient != nil && hub.pgPool != nil {
		ohlcStore = analytics.NewOHLCStore(hub.RedisClient, hub.pgPool)
		hub.ohlcStore = ohlcStore
	}

	go func() {
		for e := range hub.ohlcEngine.Output() {
			data, _ := json.Marshal(map[string]interface{}{
				"instrument_id": e.InstrumentID,
				"resolution":    e.Resolution,
				"open":          e.Open,
				"high":          e.High,
				"low":           e.Low,
				"close":         e.Close,
				"timestamp":     e.Timestamp,
			})
			hub.Broadcast <- BroadcastEvent{
				Room:    strconv.Itoa(e.InstrumentID),
				Origin:  hub.InstanceID,
				Message: Message{Type: "ohlc_update", Data: json.RawMessage(data)},
			}
			if ohlcStore != nil {
				if err := ohlcStore.Write(context.Background(), e); err != nil {
					log.Printf("OHLC write error (instrument %d): %v", e.InstrumentID, err)
				}
			}
		}
	}()

	// ---- EMA engine ----
	ema := analytics.NewEMAEngine(20)
	go ema.Run()
	hub.emaEngine = ema
	hub.registry.Register(ema) // register into feed path

	var emaStore *analytics.EMAStore
	if hub.RedisClient != nil {
		emaStore = analytics.NewEMAStore(hub.RedisClient, hub.pgPool)
		hub.emaStore = emaStore
	}

	emaToHub := func(e analytics.EMAUpdateEvent) {
		data, _ := json.Marshal(map[string]interface{}{
			"instrument_id": e.InstrumentID,
			"value":         e.Value,
			"timestamp":     e.Timestamp,
			"resolution":    e.Resolution,
		})
		hub.Broadcast <- BroadcastEvent{
			Room:    strconv.Itoa(e.InstrumentID),
			Origin:  hub.InstanceID,
			Message: Message{Type: "ema_update", Data: json.RawMessage(data)},
		}
	}

	go func() {
		for e := range hub.emaEngine.Output() {
			emaToHub(e)
			if emaStore != nil {
				if err := emaStore.Write(context.Background(), e); err != nil {
					log.Printf("EMA 1s write error (instrument %d): %v", e.InstrumentID, err)
				}
			}
		}
	}()
	go func() {
		for e := range hub.emaEngine.OutputMin() {
			emaToHub(e)
			if emaStore != nil {
				if err := emaStore.Write(context.Background(), e); err != nil {
					log.Printf("EMA 1m write error (instrument %d): %v", e.InstrumentID, err)
				}
			}
		}
	}()

	return hub
}
