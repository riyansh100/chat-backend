package trading

// Domain-level trading intents (NO hub, NO websocket)

type Event interface{}

// Market data update
type PriceUpdateEvent struct {
	Instrument string
	Price      float64
	Timestamp  int64
}

// Order placement
type OrderEvent struct {
	Instrument string
	Side       string // BUY / SELL
	Quantity   int
	Price      float64
}

// Trade execution
type TradeEvent struct {
	Instrument string
	Price      float64
	Quantity   int
}
