package analytics

import (
	"sync"
	"time"
)

// EMAUpdateEvent is emitted by the EMA engine on each flush tick.
type EMAUpdateEvent struct {
	InstrumentID int
	WindowSize   int
	Value        float64
	Timestamp    int64
	Resolution   string // "1s" or "1m"
}

// emaState holds the per-instrument EMA state.
// EMA formula: EMA_t = price * k + EMA_{t-1} * (1 - k)
// where k = 2 / (window + 1)
type emaState struct {
	value    float64
	hasValue bool
	k        float64 // smoothing factor
}

func newEMAState(window int) *emaState {
	return &emaState{
		k: 2.0 / float64(window+1),
	}
}

func (s *emaState) add(price float64) (float64, bool) {
	if !s.hasValue {
		// seed with first price
		s.value = price
		s.hasValue = true
		return s.value, true
	}
	s.value = price*s.k + s.value*(1-s.k)
	return s.value, true
}

// EMAEngine processes price ticks and emits EMA values at 1s and 1m resolutions.
// Architecture mirrors Engine (SMA) exactly — single goroutine, ticker-based flush.
type EMAEngine struct {
	input      chan PriceUpdateEvent
	out        chan EMAUpdateEvent // 1s output
	outMin     chan EMAUpdateEvent // 1m output
	states     map[int]*emaState
	buckets    map[int]*bucket // latest EMA per instrument per second
	minBuckets map[int]*bucket // latest 1s EMA seen within the minute
	window     int
	mu         sync.Mutex
}

func NewEMAEngine(window int) *EMAEngine {
	return &EMAEngine{
		input:      make(chan PriceUpdateEvent, 1024),
		out:        make(chan EMAUpdateEvent, 1024),
		outMin:     make(chan EMAUpdateEvent, 1024),
		states:     make(map[int]*emaState),
		buckets:    make(map[int]*bucket),
		minBuckets: make(map[int]*bucket),
		window:     window,
	}
}

func (e *EMAEngine) Input() chan<- PriceUpdateEvent {
	return e.input
}

// InputLen returns the number of unprocessed events in the input channel.
func (e *EMAEngine) InputLen() int {
	return len(e.input)
}

// Output returns the 1-second EMA stream.
func (e *EMAEngine) Output() <-chan EMAUpdateEvent {
	return e.out
}

// OutputMin returns the 1-minute EMA stream.
func (e *EMAEngine) OutputMin() <-chan EMAUpdateEvent {
	return e.outMin
}

func (e *EMAEngine) Run() {
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

func (e *EMAEngine) process(event PriceUpdateEvent) {
	e.mu.Lock()
	state, ok := e.states[event.InstrumentID]
	if !ok {
		state = newEMAState(e.window)
		e.states[event.InstrumentID] = state
	}

	b, ok := e.buckets[event.InstrumentID]
	if !ok {
		b = &bucket{}
		e.buckets[event.InstrumentID] = b
	}
	e.mu.Unlock()

	value, ready := state.add(event.Price)
	if !ready {
		return
	}

	e.mu.Lock()
	b.value = value
	b.hasValue = true
	e.mu.Unlock()
}

func (e *EMAEngine) flushSeconds() {
	e.mu.Lock()
	defer e.mu.Unlock()

	ts := time.Now().UnixNano()

	for instrumentID, b := range e.buckets {
		if !b.hasValue {
			continue
		}

		select {
		case e.out <- EMAUpdateEvent{
			InstrumentID: instrumentID,
			WindowSize:   e.window,
			Value:        b.value,
			Timestamp:    ts,
			Resolution:   "1s",
		}:
		default:
		}

		mb, ok := e.minBuckets[instrumentID]
		if !ok {
			mb = &bucket{}
			e.minBuckets[instrumentID] = mb
		}
		mb.value = b.value
		mb.hasValue = true

		b.hasValue = false
	}
}

func (e *EMAEngine) flushMinutes() {
	e.mu.Lock()
	defer e.mu.Unlock()

	ts := time.Now().UnixNano()

	for instrumentID, mb := range e.minBuckets {
		if !mb.hasValue {
			continue
		}

		select {
		case e.outMin <- EMAUpdateEvent{
			InstrumentID: instrumentID,
			WindowSize:   e.window,
			Value:        mb.value,
			Timestamp:    ts,
			Resolution:   "1m",
		}:
		default:
		}

		mb.hasValue = false
	}
}
