package analytics

import (
	"sync"
	"time"
)

// MACDUpdateEvent is emitted by the MACD engine on each flush tick.
type MACDUpdateEvent struct {
	InstrumentID int
	MACDLine     float64 // fast EMA - slow EMA
	SignalLine   float64 // EMA(period=signal) of MACDLine
	Histogram    float64 // MACDLine - SignalLine
	Timestamp    int64
	Resolution   string // "1s" or "1m"
}

// macdState holds per-instrument MACD state.
// MACD Line  = EMA(fast) - EMA(slow)
// Signal     = EMA(signal) of MACD Line
// Histogram  = MACD Line - Signal
type macdState struct {
	fastK   float64 // smoothing factor for fast EMA
	slowK   float64 // smoothing factor for slow EMA
	signalK float64 // smoothing factor for signal EMA

	fastEMA   float64
	slowEMA   float64
	signalEMA float64

	fastReady   bool
	slowReady   bool
	signalReady bool

	// tick counters to seed initial EMAs with SMA
	fastPrices   []float64
	slowPrices   []float64
	fastPeriod   int
	slowPeriod   int
	signalPeriod int

	macdHistory []float64 // collects MACD values to seed signal EMA
}

func newMACDState(fast, slow, signal int) *macdState {
	return &macdState{
		fastK:        2.0 / float64(fast+1),
		slowK:        2.0 / float64(slow+1),
		signalK:      2.0 / float64(signal+1),
		fastPrices:   make([]float64, 0, fast),
		slowPrices:   make([]float64, 0, slow),
		fastPeriod:   fast,
		slowPeriod:   slow,
		signalPeriod: signal,
		macdHistory:  make([]float64, 0, signal),
	}
}

// add ingests a price and returns (macdLine, signalLine, histogram, ready).
func (s *macdState) add(price float64) (macdLine, signalLine, histogram float64, ready bool) {
	// seed fast EMA
	if !s.fastReady {
		s.fastPrices = append(s.fastPrices, price)
		if len(s.fastPrices) < s.fastPeriod {
			return 0, 0, 0, false
		}
		// compute initial fast EMA as SMA
		var sum float64
		for _, p := range s.fastPrices {
			sum += p
		}
		s.fastEMA = sum / float64(s.fastPeriod)
		s.fastReady = true
	} else {
		s.fastEMA = price*s.fastK + s.fastEMA*(1-s.fastK)
	}

	// seed slow EMA
	if !s.slowReady {
		s.slowPrices = append(s.slowPrices, price)
		if len(s.slowPrices) < s.slowPeriod {
			return 0, 0, 0, false
		}
		var sum float64
		for _, p := range s.slowPrices {
			sum += p
		}
		s.slowEMA = sum / float64(s.slowPeriod)
		s.slowReady = true
	} else {
		s.slowEMA = price*s.slowK + s.slowEMA*(1-s.slowK)
	}

	macdLine = s.fastEMA - s.slowEMA

	// seed signal EMA
	if !s.signalReady {
		s.macdHistory = append(s.macdHistory, macdLine)
		if len(s.macdHistory) < s.signalPeriod {
			return 0, 0, 0, false
		}
		var sum float64
		for _, v := range s.macdHistory {
			sum += v
		}
		s.signalEMA = sum / float64(s.signalPeriod)
		s.signalReady = true
	} else {
		s.signalEMA = macdLine*s.signalK + s.signalEMA*(1-s.signalK)
	}

	signalLine = s.signalEMA
	histogram = macdLine - signalLine
	return macdLine, signalLine, histogram, true
}

// macdBucket holds the latest MACD values within a flush window.
type macdBucket struct {
	macdLine   float64
	signalLine float64
	histogram  float64
	hasValue   bool
}

// MACDEngine processes price ticks and emits MACD values at 1s and 1m resolutions.
type MACDEngine struct {
	input      chan PriceUpdateEvent
	out        chan MACDUpdateEvent // 1s output
	outMin     chan MACDUpdateEvent // 1m output
	states     map[int]*macdState
	buckets    map[int]*macdBucket
	minBuckets map[int]*macdBucket
	fast       int
	slow       int
	signal     int
	mu         sync.Mutex
}

// NewMACDEngine creates a MACD engine with standard parameters (12, 26, 9).
func NewMACDEngine(fast, slow, signal int) *MACDEngine {
	return &MACDEngine{
		input:      make(chan PriceUpdateEvent, 1024),
		out:        make(chan MACDUpdateEvent, 1024),
		outMin:     make(chan MACDUpdateEvent, 1024),
		states:     make(map[int]*macdState),
		buckets:    make(map[int]*macdBucket),
		minBuckets: make(map[int]*macdBucket),
		fast:       fast,
		slow:       slow,
		signal:     signal,
	}
}

func (e *MACDEngine) Input() chan<- PriceUpdateEvent {
	return e.input
}

func (e *MACDEngine) InputLen() int {
	return len(e.input)
}

func (e *MACDEngine) Output() <-chan MACDUpdateEvent {
	return e.out
}

func (e *MACDEngine) OutputMin() <-chan MACDUpdateEvent {
	return e.outMin
}

func (e *MACDEngine) Run() {
	tickerSec := time.NewTicker(1 * time.Second)
	tickerMin := time.NewTicker(1 * time.Minute)
	defer tickerSec.Stop()
	defer tickerMin.Stop()

	for {
		select {
		case event := <-e.input:
			e.process(event)
		case <-tickerSec.C:
			e.flushSeconds()
		case <-tickerMin.C:
			e.flushMinutes()
		}
	}
}

func (e *MACDEngine) process(event PriceUpdateEvent) {
	e.mu.Lock()
	state, ok := e.states[event.InstrumentID]
	if !ok {
		state = newMACDState(e.fast, e.slow, e.signal)
		e.states[event.InstrumentID] = state
	}
	b, ok := e.buckets[event.InstrumentID]
	if !ok {
		b = &macdBucket{}
		e.buckets[event.InstrumentID] = b
	}
	e.mu.Unlock()

	macdLine, signalLine, histogram, ready := state.add(event.Price)
	if !ready {
		return
	}

	e.mu.Lock()
	b.macdLine = macdLine
	b.signalLine = signalLine
	b.histogram = histogram
	b.hasValue = true
	e.mu.Unlock()
}

func (e *MACDEngine) flushSeconds() {
	e.mu.Lock()
	defer e.mu.Unlock()

	ts := time.Now().UnixNano()

	for instrumentID, b := range e.buckets {
		if !b.hasValue {
			continue
		}

		select {
		case e.out <- MACDUpdateEvent{
			InstrumentID: instrumentID,
			MACDLine:     b.macdLine,
			SignalLine:   b.signalLine,
			Histogram:    b.histogram,
			Timestamp:    ts,
			Resolution:   "1s",
		}:
		default:
		}

		mb, ok := e.minBuckets[instrumentID]
		if !ok {
			mb = &macdBucket{}
			e.minBuckets[instrumentID] = mb
		}
		mb.macdLine = b.macdLine
		mb.signalLine = b.signalLine
		mb.histogram = b.histogram
		mb.hasValue = true
		b.hasValue = false
	}
}

func (e *MACDEngine) flushMinutes() {
	e.mu.Lock()
	defer e.mu.Unlock()

	ts := time.Now().UnixNano()

	for instrumentID, mb := range e.minBuckets {
		if !mb.hasValue {
			continue
		}

		select {
		case e.outMin <- MACDUpdateEvent{
			InstrumentID: instrumentID,
			MACDLine:     mb.macdLine,
			SignalLine:   mb.signalLine,
			Histogram:    mb.histogram,
			Timestamp:    ts,
			Resolution:   "1m",
		}:
		default:
		}

		mb.hasValue = false
	}
}
