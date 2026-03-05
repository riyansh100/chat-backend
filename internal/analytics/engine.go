package analytics

import (
	"sync"
	"time"
)

type PriceUpdateEvent struct {
	InstrumentID int
	Price        float64
	Timestamp    int64
}

type SMAUpdateEvent struct {
	InstrumentID int
	WindowSize   int
	Value        float64
	Timestamp    int64
}

// bucket holds the latest SMA value computed within a 1-second window
type bucket struct {
	value    float64
	hasValue bool
}

type Engine struct {
	input   chan PriceUpdateEvent
	out     chan SMAUpdateEvent
	states  map[int]*SMAState
	buckets map[int]*bucket // latest SMA per instrument in current second
	window  int
	mu      sync.Mutex
}

func NewEngine(window int) *Engine {
	return &Engine{
		input:   make(chan PriceUpdateEvent, 1024),
		out:     make(chan SMAUpdateEvent, 1024),
		states:  make(map[int]*SMAState),
		buckets: make(map[int]*bucket),
		window:  window,
	}
}

func (e *Engine) Input() chan<- PriceUpdateEvent {
	return e.input
}

func (e *Engine) Output() <-chan SMAUpdateEvent {
	return e.out
}

func (e *Engine) Run() {
	ticker := time.NewTicker(1 * time.Second)
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

// process updates the SMA state and stores the latest value in the bucket.
// it does NOT emit to output — that only happens on flush.
func (e *Engine) process(event PriceUpdateEvent) {
	e.mu.Lock()
	state, ok := e.states[event.InstrumentID]
	if !ok {
		state = NewSMAState(e.window)
		e.states[event.InstrumentID] = state
	}

	b, ok := e.buckets[event.InstrumentID]
	if !ok {
		b = &bucket{}
		e.buckets[event.InstrumentID] = b
	}
	e.mu.Unlock()

	value, ready := state.Add(event.Price)
	if !ready {
		return
	}

	// overwrite bucket with latest SMA value this second
	e.mu.Lock()
	b.value = value
	b.hasValue = true
	e.mu.Unlock()
}

// flush emits one SMAUpdateEvent per instrument that received data this second,
// then clears all buckets for the next window.
func (e *Engine) flush() {
	e.mu.Lock()
	defer e.mu.Unlock()

	ts := time.Now().UnixNano()

	for instrumentID, b := range e.buckets {
		if !b.hasValue {
			continue
		}

		select {
		case e.out <- SMAUpdateEvent{
			InstrumentID: instrumentID,
			WindowSize:   e.window,
			Value:        b.value,
			Timestamp:    ts,
		}:
		default:
			// output channel full — safe drop, don't block flush
		}

		b.hasValue = false
	}
}
