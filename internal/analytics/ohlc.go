package analytics

import (
	"sync"
	"time"
)

type OHLCEvent struct {
	InstrumentID int
	Resolution   string
	Open         float64
	High         float64
	Low          float64
	Close        float64
	Timestamp    int64 // unix seconds — when the candle closed
}

// ohlcCandle accumulates price ticks for one instrument within one bucket
type ohlcCandle struct {
	open     float64
	high     float64
	low      float64
	close    float64
	hasValue bool
}

func (c *ohlcCandle) add(price float64) {
	if !c.hasValue {
		c.open = price
		c.high = price
		c.low = price
		c.close = price
		c.hasValue = true
		return
	}
	if price > c.high {
		c.high = price
	}
	if price < c.low {
		c.low = price
	}
	c.close = price
}

type OHLCEngine struct {
	input   chan PriceUpdateEvent
	out     chan OHLCEvent
	candles map[int]*ohlcCandle
	mu      sync.Mutex
}

func NewOHLCEngine() *OHLCEngine {
	return &OHLCEngine{
		input:   make(chan PriceUpdateEvent, 1024),
		out:     make(chan OHLCEvent, 1024),
		candles: make(map[int]*ohlcCandle),
	}
}

func (e *OHLCEngine) Input() chan<- PriceUpdateEvent {
	return e.input
}

func (e *OHLCEngine) Output() <-chan OHLCEvent {
	return e.out
}

func (e *OHLCEngine) Run() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case event := <-e.input:
			e.process(event)
		case <-ticker.C:
			e.flush()
		}
	}
}

func (e *OHLCEngine) process(event PriceUpdateEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	c, ok := e.candles[event.InstrumentID]
	if !ok {
		c = &ohlcCandle{}
		e.candles[event.InstrumentID] = c
	}
	c.add(event.Price)
}

func (e *OHLCEngine) flush() {
	e.mu.Lock()
	defer e.mu.Unlock()

	ts := time.Now().Unix()

	for instrumentID, c := range e.candles {
		if !c.hasValue {
			continue
		}

		select {
		case e.out <- OHLCEvent{
			InstrumentID: instrumentID,
			Resolution:   "1m",
			Open:         c.open,
			High:         c.high,
			Low:          c.low,
			Close:        c.close,
			Timestamp:    ts,
		}:
		default:
		}

		// reset candle for next minute
		e.candles[instrumentID] = &ohlcCandle{}
	}
}
