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

type Engine struct {
	input  chan PriceUpdateEvent
	out    chan SMAUpdateEvent
	states map[int]*SMAState
	window int
	mu     sync.Mutex
}

func NewEngine(window int) *Engine {
	return &Engine{
		input:  make(chan PriceUpdateEvent, 1024),
		out:    make(chan SMAUpdateEvent, 1024),
		states: make(map[int]*SMAState),
		window: window,
	}
}

func (e *Engine) Input() chan<- PriceUpdateEvent {
	return e.input
}

func (e *Engine) Output() <-chan SMAUpdateEvent {
	return e.out
}

func (e *Engine) Run() {
	for event := range e.input {
		e.process(event)
	}
}

func (e *Engine) process(event PriceUpdateEvent) {
	e.mu.Lock()
	state, ok := e.states[event.InstrumentID]
	if !ok {
		state = NewSMAState(e.window)
		e.states[event.InstrumentID] = state
	}
	e.mu.Unlock()

	value, ready := state.Add(event.Price)
	if !ready {
		return
	}

	e.out <- SMAUpdateEvent{
		InstrumentID: event.InstrumentID,
		WindowSize:   e.window,
		Value:        value,
		Timestamp:    time.Now().UnixNano(),
	}
}
