// internal/hub/hub_init.go
package hub

import (
	"context"
	"encoding/json"
	"log"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
	"github.com/riyansh/chat-backend/internal/analytics"
	"github.com/riyansh/chat-backend/internal/cache"
	"github.com/riyansh/chat-backend/internal/history"
	"github.com/riyansh/chat-backend/internal/metrics"
	internalnats "github.com/riyansh/chat-backend/internal/nats"
	"github.com/riyansh/chat-backend/internal/redis"
)

type analyticsEvent struct {
	Room   string          `json:"room"`
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data"`
	Topic  string          `json:"topic"`
	Origin string          `json:"origin"`
}

// NewHub creates a full hub with all 6 engines (data server mode).
// nc must be connected; EnsureStream is called here so the stream exists
// before any engine goroutine tries to publish.
func NewHub(instanceID string, redisCache redis.Cache, rdb *goredis.Client, lb *redis.RedisLoadBalancer, pool *pgxpool.Pool, nc *nats.Conn) *Hub {
	l1Cache, err := cache.NewL1Cache()
	if err != nil {
		panic(err)
	}

	m := &metrics.HubMetrics{}
	m.StartLogger()

	js, err := internalnats.EnsureStream(context.Background(), nc)
	if err != nil {
		panic(err)
	}
	pub := internalnats.NewPublisher(js)

	hub := &Hub{
		InstanceID:  instanceID,
		Rooms:       make(map[string]*Room),
		redisCache:  redisCache,
		l1:          l1Cache,
		RedisClient: rdb,
		NatsConn:    nc,
		natsPub:     pub,
		lb:          lb,
		pgPool:      pool,
		Metrics:     m,
		Register:    make(chan *Client, 512),
		Unregister:  make(chan *Client, 512),
		JoinRoom:    make(chan JoinRoomEvent, 2048),
		LeaveRoom:   make(chan LeaveRoomEvent, 512),
		Broadcast:   make(chan BroadcastEvent, 4096),
		Subscribe:   make(chan SubscribeEvent, 256),
		Unsubscribe: make(chan UnsubscribeEvent, 256),
	}

	hub.subManager = NewSubscriptionManager()
	hub.registry = analytics.NewRegistry()

	histStore := history.NewStore(lb, pool)

	publish := func(ev analyticsEvent) {
		pub.Publish(context.Background(), ev)
	}

	// ---- SMA ----
	sma := analytics.NewEngine(20)
	go sma.Run()
	hub.smaEngine = sma
	hub.registry.Register(sma)
	smaStore := analytics.NewSMAStore(lb, pool)
	hub.smaStore = smaStore

	smaToHub := func(e analytics.SMAUpdateEvent) {
		data, _ := json.Marshal(map[string]interface{}{
			"instrument_id": e.InstrumentID, "value": e.Value,
			"timestamp": e.Timestamp, "resolution": e.Resolution,
		})
		room := strconv.Itoa(e.InstrumentID)
		publish(analyticsEvent{
			Room: room, Type: "sma_update", Data: json.RawMessage(data),
			Topic: "sma:" + room, Origin: instanceID,
		})
		histStore.Write1m(context.Background(), "sma", e.InstrumentID,
			e.Timestamp/1e9, strconv.FormatFloat(e.Value, 'f', 6, 64))
	}
	go func() {
		for e := range hub.smaEngine.Output() {
			smaToHub(e)
			if err := smaStore.Write(context.Background(), e); err != nil {
				log.Printf("SMA 1s write error (instrument %d): %v", e.InstrumentID, err)
			}
		}
	}()
	go func() {
		for e := range hub.smaEngine.OutputMin() {
			smaToHub(e)
			if err := smaStore.Write(context.Background(), e); err != nil {
				log.Printf("SMA 1m write error (instrument %d): %v", e.InstrumentID, err)
			}
		}
	}()

	// ---- OHLC ----
	ohlc := analytics.NewOHLCEngine()
	go ohlc.Run()
	hub.ohlcEngine = ohlc
	hub.registry.Register(ohlc)
	ohlcStore := analytics.NewOHLCStore(lb, pool)
	hub.ohlcStore = ohlcStore

	go func() {
		for e := range hub.ohlcEngine.Output() {
			data, _ := json.Marshal(map[string]interface{}{
				"instrument_id": e.InstrumentID, "resolution": e.Resolution,
				"open": e.Open, "high": e.High, "low": e.Low, "close": e.Close,
				"timestamp": e.Timestamp,
			})
			room := strconv.Itoa(e.InstrumentID)
			publish(analyticsEvent{
				Room: room, Type: "ohlc_update", Data: json.RawMessage(data),
				Topic: "ohlc:" + room, Origin: instanceID,
			})
			candle, _ := json.Marshal(map[string]float64{
				"open": e.Open, "high": e.High, "low": e.Low, "close": e.Close,
			})
			histStore.Write1m(context.Background(), "ohlc", e.InstrumentID, e.Timestamp, string(candle))
			if err := ohlcStore.Write(context.Background(), e); err != nil {
				log.Printf("OHLC write error (instrument %d): %v", e.InstrumentID, err)
			}
		}
	}()

	// ---- EMA ----
	ema := analytics.NewEMAEngine(20)
	go ema.Run()
	hub.emaEngine = ema
	hub.registry.Register(ema)
	emaStore := analytics.NewEMAStore(lb, pool)
	hub.emaStore = emaStore

	emaToHub := func(e analytics.EMAUpdateEvent) {
		data, _ := json.Marshal(map[string]interface{}{
			"instrument_id": e.InstrumentID, "value": e.Value,
			"timestamp": e.Timestamp, "resolution": e.Resolution,
		})
		room := strconv.Itoa(e.InstrumentID)
		publish(analyticsEvent{
			Room: room, Type: "ema_update", Data: json.RawMessage(data),
			Topic: "ema:" + room, Origin: instanceID,
		})
		histStore.Write1m(context.Background(), "ema", e.InstrumentID,
			e.Timestamp/1e9, strconv.FormatFloat(e.Value, 'f', 6, 64))
	}
	go func() {
		for e := range hub.emaEngine.Output() {
			emaToHub(e)
			if err := emaStore.Write(context.Background(), e); err != nil {
				log.Printf("EMA 1s write error (instrument %d): %v", e.InstrumentID, err)
			}
		}
	}()
	go func() {
		for e := range hub.emaEngine.OutputMin() {
			emaToHub(e)
			if err := emaStore.Write(context.Background(), e); err != nil {
				log.Printf("EMA 1m write error (instrument %d): %v", e.InstrumentID, err)
			}
		}
	}()

	// ---- Bollinger Bands ----
	bb := analytics.NewBBEngine(20, 2.0)
	go bb.Run()
	hub.bbEngine = bb
	hub.registry.Register(bb)
	bbStore := analytics.NewBBStore(lb, pool)
	hub.bbStore = bbStore

	go func() {
		for e := range hub.bbEngine.Output() {
			data, _ := json.Marshal(map[string]interface{}{
				"instrument_id": e.InstrumentID, "upper": e.Upper,
				"middle": e.Middle, "lower": e.Lower,
				"timestamp": e.Timestamp, "resolution": e.Resolution,
			})
			room := strconv.Itoa(e.InstrumentID)
			publish(analyticsEvent{
				Room: room, Type: "bb_update", Data: json.RawMessage(data),
				Topic: "bb:" + room, Origin: instanceID,
			})
			band, _ := json.Marshal(map[string]float64{
				"upper": e.Upper, "middle": e.Middle, "lower": e.Lower,
			})
			histStore.Write1m(context.Background(), "bb", e.InstrumentID, e.Timestamp/1e9, string(band))
			if err := bbStore.Write(context.Background(), e); err != nil {
				log.Printf("BB write error (instrument %d): %v", e.InstrumentID, err)
			}
		}
	}()

	// ---- RSI ----
	rsi := analytics.NewRSIEngine(14)
	go rsi.Run()
	hub.rsiEngine = rsi
	hub.registry.Register(rsi)
	rsiStore := analytics.NewRSIStore(lb, pool)
	hub.rsiStore = rsiStore

	rsiToHub := func(e analytics.RSIUpdateEvent) {
		data, _ := json.Marshal(map[string]interface{}{
			"instrument_id": e.InstrumentID, "value": e.Value,
			"timestamp": e.Timestamp, "resolution": e.Resolution,
		})
		room := strconv.Itoa(e.InstrumentID)
		publish(analyticsEvent{
			Room: room, Type: "rsi_update", Data: json.RawMessage(data),
			Topic: "rsi:" + room, Origin: instanceID,
		})
		histStore.Write1m(context.Background(), "rsi", e.InstrumentID,
			e.Timestamp/1e9, strconv.FormatFloat(e.Value, 'f', 6, 64))
	}
	go func() {
		for e := range hub.rsiEngine.Output() {
			rsiToHub(e)
			if err := rsiStore.Write(context.Background(), e); err != nil {
				log.Printf("RSI 1s write error (instrument %d): %v", e.InstrumentID, err)
			}
		}
	}()
	go func() {
		for e := range hub.rsiEngine.OutputMin() {
			rsiToHub(e)
			if err := rsiStore.Write(context.Background(), e); err != nil {
				log.Printf("RSI 1m write error (instrument %d): %v", e.InstrumentID, err)
			}
		}
	}()

	// ---- MACD ----
	macd := analytics.NewMACDEngine(12, 26, 9)
	go macd.Run()
	hub.macdEngine = macd
	hub.registry.Register(macd)
	macdStore := analytics.NewMACDStore(lb, pool)
	hub.macdStore = macdStore

	macdToHub := func(e analytics.MACDUpdateEvent) {
		data, _ := json.Marshal(map[string]interface{}{
			"instrument_id": e.InstrumentID, "macd_line": e.MACDLine,
			"signal_line": e.SignalLine, "histogram": e.Histogram,
			"timestamp": e.Timestamp, "resolution": e.Resolution,
		})
		room := strconv.Itoa(e.InstrumentID)
		publish(analyticsEvent{
			Room: room, Type: "macd_update", Data: json.RawMessage(data),
			Topic: "macd:" + room, Origin: instanceID,
		})
		mv, _ := json.Marshal(map[string]float64{
			"macd_line": e.MACDLine, "signal_line": e.SignalLine, "histogram": e.Histogram,
		})
		histStore.Write1m(context.Background(), "macd", e.InstrumentID, e.Timestamp/1e9, string(mv))
	}
	go func() {
		for e := range hub.macdEngine.Output() {
			macdToHub(e)
			if err := macdStore.Write(context.Background(), e); err != nil {
				log.Printf("MACD 1s write error (instrument %d): %v", e.InstrumentID, err)
			}
		}
	}()
	go func() {
		for e := range hub.macdEngine.OutputMin() {
			macdToHub(e)
			if err := macdStore.Write(context.Background(), e); err != nil {
				log.Printf("MACD 1m write error (instrument %d): %v", e.InstrumentID, err)
			}
		}
	}()

	return hub
}

// NewClientHub creates a hub with NO engines (client server mode).
// nc is stored on the hub so clientserver/main.go can start the NATS
// JetStream consumer after calling this function.
func NewClientHub(instanceID string, redisCache redis.Cache, rdb *goredis.Client, lb *redis.RedisLoadBalancer, pool *pgxpool.Pool, nc *nats.Conn) *Hub {
	l1Cache, err := cache.NewL1Cache()
	if err != nil {
		panic(err)
	}

	m := &metrics.HubMetrics{}
	m.StartLogger()

	return &Hub{
		InstanceID:  instanceID,
		Rooms:       make(map[string]*Room),
		redisCache:  redisCache,
		l1:          l1Cache,
		RedisClient: rdb,
		NatsConn:    nc,
		lb:          lb,
		pgPool:      pool,
		Metrics:     m,
		Register:    make(chan *Client, 512),
		Unregister:  make(chan *Client, 512),
		JoinRoom:    make(chan JoinRoomEvent, 2048),
		LeaveRoom:   make(chan LeaveRoomEvent, 512),
		Broadcast:   make(chan BroadcastEvent, 4096),
		Subscribe:   make(chan SubscribeEvent, 256),
		Unsubscribe: make(chan UnsubscribeEvent, 256),
		subManager:  NewSubscriptionManager(),
	}
}
