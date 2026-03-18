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
		Subscribe:   make(chan SubscribeEvent),
		Unsubscribe: make(chan UnsubscribeEvent),
	}

	hub.subManager = NewSubscriptionManager()
	hub.registry = analytics.NewRegistry()

	// ---- SMA ----
	sma := analytics.NewEngine(20)
	go sma.Run()
	hub.smaEngine = sma
	hub.registry.Register(sma)

	var smaStore *analytics.SMAStore
	if rdb != nil {
		smaStore = analytics.NewSMAStore(rdb, pool)
		hub.smaStore = smaStore
	}

	smaToHub := func(e analytics.SMAUpdateEvent) {
		data, _ := json.Marshal(map[string]interface{}{
			"instrument_id": e.InstrumentID,
			"value":         e.Value,
			"timestamp":     e.Timestamp,
			"resolution":    e.Resolution,
		})
		msg := Message{Type: "sma_update", Data: json.RawMessage(data)}
		room := strconv.Itoa(e.InstrumentID)

		// push path — existing room broadcast
		hub.Broadcast <- BroadcastEvent{Room: room, Origin: hub.InstanceID, Message: msg}

		// pull path — fanout to explicit subscribers
		topic := "sma:" + room
		hub.subManager.Fanout(topic, msg)
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

	// ---- OHLC ----
	ohlc := analytics.NewOHLCEngine()
	go ohlc.Run()
	hub.ohlcEngine = ohlc
	hub.registry.Register(ohlc)

	var ohlcStore *analytics.OHLCStore
	if rdb != nil && pool != nil {
		ohlcStore = analytics.NewOHLCStore(rdb, pool)
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
			msg := Message{Type: "ohlc_update", Data: json.RawMessage(data)}
			room := strconv.Itoa(e.InstrumentID)

			hub.Broadcast <- BroadcastEvent{Room: room, Origin: hub.InstanceID, Message: msg}
			hub.subManager.Fanout("ohlc:"+room, msg)

			if ohlcStore != nil {
				if err := ohlcStore.Write(context.Background(), e); err != nil {
					log.Printf("OHLC write error (instrument %d): %v", e.InstrumentID, err)
				}
			}
		}
	}()

	// ---- EMA ----
	ema := analytics.NewEMAEngine(20)
	go ema.Run()
	hub.emaEngine = ema
	hub.registry.Register(ema)

	var emaStore *analytics.EMAStore
	if rdb != nil {
		emaStore = analytics.NewEMAStore(rdb, pool)
		hub.emaStore = emaStore
	}

	emaToHub := func(e analytics.EMAUpdateEvent) {
		data, _ := json.Marshal(map[string]interface{}{
			"instrument_id": e.InstrumentID,
			"value":         e.Value,
			"timestamp":     e.Timestamp,
			"resolution":    e.Resolution,
		})
		msg := Message{Type: "ema_update", Data: json.RawMessage(data)}
		room := strconv.Itoa(e.InstrumentID)

		hub.Broadcast <- BroadcastEvent{Room: room, Origin: hub.InstanceID, Message: msg}
		hub.subManager.Fanout("ema:"+room, msg)
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
