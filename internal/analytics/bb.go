package analytics

import (
	"math"
	"sync"
	"time"
)

// BBUpdateEvent is emitted by the BB engine on each 1m flush.
type BBUpdateEvent struct {
	InstrumentID int
	Upper        float64
	Middle       float64
	Lower        float64
	Timestamp    int64
	Resolution   string // always "1m"
}

// bbState holds the sliding window needed to compute BB.
// Middle = SMA(window), StdDev = population stddev of window.
// Upper = Middle + k*StdDev, Lower = Middle - k*StdDev
type bbState struct {
	window []float64
	size   int
	k      float64 // multiplier, default 2.0
}

func newBBState(size int, k float64) *bbState {
	return &bbState{
		window: make([]float64, 0, size),
		size:   size,
		k:      k,
	}
}

// add appends price and returns (upper, middle, lower, ready).
// Returns ready=false until window is full.
func (s *bbState) add(price float64) (upper, middle, lower float64, ready bool) {
	if len(s.window) == s.size {
		s.window = s.window[1:]
	}
	s.window = append(s.window, price)

	if len(s.window) < s.size {
		return 0, 0, 0, false
	}

	// mean
	var sum float64
	for _, v := range s.window {
		sum += v
	}
	mean := sum / float64(s.size)

	// population stddev
	var variance float64
	for _, v := range s.window {
		diff := v - mean
		variance += diff * diff
	}
	stddev := math.Sqrt(variance / float64(s.size))

	return mean + s.k*stddev, mean, mean - s.k*stddev, true
}

// bbCandle holds the latest BB values within a 1m bucket.
type bbCandle struct {
	upper    float64
	middle   float64
	lower    float64
	hasValue bool
}

// BBEngine processes price ticks and emits BB values at 1m resolution.
// Architecture mirrors EMAEngine — single goroutine, ticker-based flush.
type BBEngine struct {
	input   chan PriceUpdateEvent
	out     chan BBUpdateEvent // 1m output
	states  map[int]*bbState
	buckets map[int]*bbCandle // latest BB per instrument within the minute
	window  int
	k       float64
	mu      sync.Mutex
}

// NewBBEngine creates a new Bollinger Bands engine.
// window=20 and k=2.0 are the standard parameters.
func NewBBEngine(window int, k float64) *BBEngine {
	return &BBEngine{
		input:   make(chan PriceUpdateEvent, 1024),
		out:     make(chan BBUpdateEvent, 1024),
		states:  make(map[int]*bbState),
		buckets: make(map[int]*bbCandle),
		window:  window,
		k:       k,
	}
}

func (e *BBEngine) Input() chan<- PriceUpdateEvent {
	return e.input
}

// InputLen returns the number of unprocessed events in the input channel.
func (e *BBEngine) InputLen() int {
	return len(e.input)
}

// Output returns the 1-minute BB stream.
func (e *BBEngine) Output() <-chan BBUpdateEvent {
	return e.out
}

func (e *BBEngine) Run() {
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

func (e *BBEngine) process(event PriceUpdateEvent) {
	e.mu.Lock()
	state, ok := e.states[event.InstrumentID]
	if !ok {
		state = newBBState(e.window, e.k)
		e.states[event.InstrumentID] = state
	}

	candle, ok := e.buckets[event.InstrumentID]
	if !ok {
		candle = &bbCandle{}
		e.buckets[event.InstrumentID] = candle
	}
	e.mu.Unlock()

	upper, middle, lower, ready := state.add(event.Price)
	if !ready {
		return
	}

	e.mu.Lock()
	candle.upper = upper
	candle.middle = middle
	candle.lower = lower
	candle.hasValue = true
	e.mu.Unlock()
}

func (e *BBEngine) flush() {
	e.mu.Lock()
	defer e.mu.Unlock()

	ts := time.Now().UnixNano()

	for instrumentID, candle := range e.buckets {
		if !candle.hasValue {
			continue
		}

		select {
		case e.out <- BBUpdateEvent{
			InstrumentID: instrumentID,
			Upper:        candle.upper,
			Middle:       candle.middle,
			Lower:        candle.lower,
			Timestamp:    ts,
			Resolution:   "1m",
		}:
		default:
		}

		candle.hasValue = false
	}
}
