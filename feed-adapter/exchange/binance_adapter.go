package exchange

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

type NormalizedPriceEvent struct {
	Type       string  `json:"type"`
	Instrument string  `json:"instrument"`
	Price      float64 `json:"price"`
	Timestamp  int64   `json:"ts"`
}

type binanceTradeMsg struct {
	EventType string `json:"e"`
	Symbol    string `json:"s"`
	Price     string `json:"p"`
	TradeTime int64  `json:"T"`
}

type BinanceAdapter struct {
	Endpoint string
	Out      chan<- NormalizedPriceEvent
}

func (a *BinanceAdapter) Start(ctx context.Context) {
	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := a.connectAndStream(ctx)
		if err != nil {
			log.Println("binance reconnecting in", backoff)
			time.Sleep(backoff)

			if backoff < 10*time.Second {
				backoff *= 2
			}
			continue
		}

		backoff = time.Second
	}
}

func (a *BinanceAdapter) connectAndStream(ctx context.Context) error {
	u, err := url.Parse(a.Endpoint)
	if err != nil {
		return err
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Println("connected to Binance")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		//log.Println("RAW:", string(msg))
		//log.Println("READ OK")

		event, err := normalize(msg)
		if err != nil || event.Type == "" {
			continue
		}

		//log.Println("NORMALIZED OK:", event)

		select {
		case a.Out <- event:
		default:
			// drop if slow consumer
		}
	}
}

func normalize(msg []byte) (NormalizedPriceEvent, error) {
	// First try to detect multi-stream wrapper
	var wrapper struct {
		Stream string          `json:"stream"`
		Data   json.RawMessage `json:"data"`
	}

	// Try to unmarshal into wrapper
	if err := json.Unmarshal(msg, &wrapper); err == nil && wrapper.Data != nil {
		// Multi-stream format → unwrap
		msg = wrapper.Data
	}

	var raw map[string]interface{}

	// Parse actual trade payload
	if err := json.Unmarshal(msg, &raw); err != nil {
		return NormalizedPriceEvent{}, err
	}

	// Ensure this is a trade event
	eventType, _ := raw["e"].(string)
	if eventType != "trade" {
		return NormalizedPriceEvent{}, nil
	}

	symbol, _ := raw["s"].(string)
	priceStr, _ := raw["p"].(string)

	// Binance sometimes sends numbers differently — handle safely
	var ts int64
	switch v := raw["T"].(type) {
	case float64:
		ts = int64(v)
	case int64:
		ts = v
	default:
		return NormalizedPriceEvent{}, nil
	}

	if symbol == "" || priceStr == "" {
		return NormalizedPriceEvent{}, nil
	}

	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return NormalizedPriceEvent{}, err
	}

	return NormalizedPriceEvent{
		Type:       "price_update",
		Instrument: canonicalInstrument(symbol),
		Price:      price,
		Timestamp:  ts,
	}, nil
}

func canonicalInstrument(sym string) string {
	if len(sym) <= 4 {
		return sym
	}
	return sym[:len(sym)-4] + "_" + sym[len(sym)-4:]
}
