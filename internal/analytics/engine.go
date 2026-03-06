package analytics

import (
	"fmt"
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
	Resolution   string // "1s" or "1m"
}

// bucket holds the latest SMA value computed within a time window
type bucket struct {
	value    float64
	hasValue bool
}

type Engine struct {
	input      chan PriceUpdateEvent
	out        chan SMAUpdateEvent // 1s output
	outMin     chan SMAUpdateEvent // 1m output
	states     map[int]*SMAState
	buckets    map[int]*bucket // 1s: latest SMA per instrument per second
	minBuckets map[int]*bucket // 1m: latest 1s SMA seen within the minute
	window     int
	mu         sync.Mutex
}

func NewEngine(window int) *Engine {
	return &Engine{
		input:      make(chan PriceUpdateEvent, 1024),
		out:        make(chan SMAUpdateEvent, 1024),
		outMin:     make(chan SMAUpdateEvent, 1024),
		states:     make(map[int]*SMAState),
		buckets:    make(map[int]*bucket),
		minBuckets: make(map[int]*bucket),
		window:     window,
	}
}

func (e *Engine) Input() chan<- PriceUpdateEvent {
	return e.input
}

// Output returns the 1-second SMA stream
func (e *Engine) Output() <-chan SMAUpdateEvent {
	return e.out
}

// OutputMin returns the 1-minute SMA stream
func (e *Engine) OutputMin() <-chan SMAUpdateEvent {
	return e.outMin
}

func (e *Engine) Run() {
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

// process updates the SMA state and stores the latest value in the 1s bucket.
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

	e.mu.Lock()
	b.value = value
	b.hasValue = true
	e.mu.Unlock()
}

// flushSeconds emits one 1s SMAUpdateEvent per instrument and feeds
// the latest value into the 1m bucket for aggregation.
func (e *Engine) flushSeconds() {
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

// flushMinutes emits one 1m SMAUpdateEvent per instrument,
// using the last 1s SMA value seen within that minute.
func (e *Engine) flushMinutes() {
	e.mu.Lock()
	fmt.Printf("[1m FLUSH] checking %d minBuckets\n", len(e.minBuckets))
	defer e.mu.Unlock()

	ts := time.Now().UnixNano()

	for instrumentID, mb := range e.minBuckets {
		if !mb.hasValue {
			continue
		}

		select {
		case e.outMin <- SMAUpdateEvent{
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
