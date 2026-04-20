// internal/hub/hub_init.go
package hub

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
	"github.com/riyansh/chat-backend/internal/analytics"
	bincod "github.com/riyansh/chat-backend/internal/binary"
	"github.com/riyansh/chat-backend/internal/cache"
	"github.com/riyansh/chat-backend/internal/history"
	"github.com/riyansh/chat-backend/internal/metrics"
	internalnats "github.com/riyansh/chat-backend/internal/nats"
	"github.com/riyansh/chat-backend/internal/redis"
)

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

	// ---- SMA ----
	sma := analytics.NewEngine(20)
	go sma.Run()
	hub.smaEngine = sma
	hub.registry.Register(sma)
	smaStore := analytics.NewSMAStore(lb, pool)
	hub.smaStore = smaStore

	smaToHub := func(e analytics.SMAUpdateEvent) {
		// Binary NATS frame: 22 bytes (was ~80 bytes of JSON)
		frame := bincod.EncodeScalarFrame(bincod.TypeSMA, e.InstrumentID, e.Resolution, e.Timestamp, e.Value)
		pub.Publish(context.Background(), frame)
		// History store: 8-byte binary member (was FormatFloat string ~10-18 bytes)
		histStore.Write1m(context.Background(), "sma", e.InstrumentID,
			e.Timestamp/1e9, bincod.EncodeScalar(e.Value))
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
			// Binary NATS frame: 46 bytes (was ~110 bytes of JSON)
			frame := bincod.EncodeOHLCFrame(e.InstrumentID, e.Resolution, e.Timestamp,
				e.Open, e.High, e.Low, e.Close)
			pub.Publish(context.Background(), frame)
			// History store: 32-byte binary member (was ~60 bytes of JSON)
			histStore.Write1m(context.Background(), "ohlc", e.InstrumentID,
				e.Timestamp, bincod.EncodeOHLC(e.Open, e.High, e.Low, e.Close))
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
		frame := bincod.EncodeScalarFrame(bincod.TypeEMA, e.InstrumentID, e.Resolution, e.Timestamp, e.Value)
		pub.Publish(context.Background(), frame)
		histStore.Write1m(context.Background(), "ema", e.InstrumentID,
			e.Timestamp/1e9, bincod.EncodeScalar(e.Value))
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
			// Binary NATS frame: 38 bytes (was ~100 bytes of JSON)
			frame := bincod.EncodeBBFrame(e.InstrumentID, e.Resolution, e.Timestamp,
				e.Upper, e.Middle, e.Lower)
			pub.Publish(context.Background(), frame)
			// History store: 24-byte binary member (was ~50 bytes of JSON)
			histStore.Write1m(context.Background(), "bb", e.InstrumentID,
				e.Timestamp/1e9, bincod.EncodeBB(e.Upper, e.Middle, e.Lower))
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
		frame := bincod.EncodeScalarFrame(bincod.TypeRSI, e.InstrumentID, e.Resolution, e.Timestamp, e.Value)
		pub.Publish(context.Background(), frame)
		histStore.Write1m(context.Background(), "rsi", e.InstrumentID,
			e.Timestamp/1e9, bincod.EncodeScalar(e.Value))
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
		// Binary NATS frame: 38 bytes (was ~110 bytes of JSON)
		frame := bincod.EncodeMACDFrame(e.InstrumentID, e.Resolution, e.Timestamp,
			e.MACDLine, e.SignalLine, e.Histogram)
		pub.Publish(context.Background(), frame)
		// History store: 24-byte binary member (was ~60 bytes of JSON)
		histStore.Write1m(context.Background(), "macd", e.InstrumentID,
			e.Timestamp/1e9, bincod.EncodeMACD(e.MACDLine, e.SignalLine, e.Histogram))
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

// typeTagToEventType maps a binary type tag to the WS event-type string
// sent to frontend clients (unchanged — frontend still receives JSON).
func typeTagToEventType(tag byte) string {
	switch tag {
	case bincod.TypeSMA:
		return "sma_update"
	case bincod.TypeEMA:
		return "ema_update"
	case bincod.TypeRSI:
		return "rsi_update"
	case bincod.TypeOHLC:
		return "ohlc_update"
	case bincod.TypeBB:
		return "bb_update"
	case bincod.TypeMACD:
		return "macd_update"
	default:
		return "unknown"
	}
}

// DecodeFrameToMessage converts a raw binary NATS frame into a hub Message
// whose Data field is a plain map — ready to be JSON-marshalled and pushed
// to the WebSocket client.  This is the only place binary→JSON conversion
// occurs, and it only runs on the clientserver side.
func DecodeFrameToMessage(raw []byte) (room string, msg Message, err error) {
	f, err := bincod.DecodeFrame(raw)
	if err != nil {
		return "", Message{}, err
	}

	room = strconv.Itoa(int(f.InstrumentID))
	res := bincod.ResString(f.Resolution)
	eventType := typeTagToEventType(f.TypeTag)

	var data map[string]interface{}

	switch f.TypeTag {
	case bincod.TypeSMA, bincod.TypeEMA, bincod.TypeRSI:
		v, decErr := f.ScalarValue()
		if decErr != nil {
			return "", Message{}, decErr
		}
		data = map[string]interface{}{
			"instrument_id": int(f.InstrumentID),
			"value":         v,
			"timestamp":     f.Timestamp,
			"resolution":    res,
		}

	case bincod.TypeOHLC:
		open, high, low, close, decErr := f.OHLCValues()
		if decErr != nil {
			return "", Message{}, decErr
		}
		data = map[string]interface{}{
			"instrument_id": int(f.InstrumentID),
			"open":          open,
			"high":          high,
			"low":           low,
			"close":         close,
			"timestamp":     f.Timestamp,
			"resolution":    res,
		}

	case bincod.TypeBB:
		upper, middle, lower, decErr := f.BBValues()
		if decErr != nil {
			return "", Message{}, decErr
		}
		data = map[string]interface{}{
			"instrument_id": int(f.InstrumentID),
			"upper":         upper,
			"middle":        middle,
			"lower":         lower,
			"timestamp":     f.Timestamp,
			"resolution":    res,
		}

	case bincod.TypeMACD:
		macdLine, signalLine, histogram, decErr := f.MACDValues()
		if decErr != nil {
			return "", Message{}, decErr
		}
		data = map[string]interface{}{
			"instrument_id": int(f.InstrumentID),
			"macd_line":     macdLine,
			"signal_line":   signalLine,
			"histogram":     histogram,
			"timestamp":     f.Timestamp,
			"resolution":    res,
		}

	default:
		return "", Message{}, fmt.Errorf("DecodeFrameToMessage: unknown type tag 0x%02x", f.TypeTag)
	}

	return room, Message{
		Type: eventType,
		Room: room,
		Data: data,
	}, nil
}
