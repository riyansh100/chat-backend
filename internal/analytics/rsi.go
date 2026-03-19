package analytics

import (
	"sync"
	"time"
)

// RSIUpdateEvent is emitted by the RSI engine on each flush tick.
type RSIUpdateEvent struct {
	InstrumentID int
	Value        float64
	Timestamp    int64
	Resolution   string // "1s" or "1m"
}

// rsiState holds per-instrument RSI state using Wilder's smoothing method.
// RSI = 100 - (100 / (1 + RS)), where RS = avgGain / avgLoss
type rsiState struct {
	period  int
	prices  []float64 // rolling window of last `period+1` prices
	avgGain float64
	avgLoss float64
	seeded  bool // true once we have enough data for first avg
}

func newRSIState(period int) *rsiState {
	return &rsiState{
		period: period,
		prices: make([]float64, 0, period+1),
	}
}

// add ingests a new price and returns (rsi, ready).
// Returns ready=false until the initial window is filled.
func (s *rsiState) add(price float64) (float64, bool) {
	s.prices = append(s.prices, price)

	// need at least period+1 prices to compute first RSI
	if len(s.prices) < s.period+1 {
		return 0, false
	}

	if !s.seeded {
		// first RSI: simple average of first `period` changes
		var gainSum, lossSum float64
		for i := 1; i <= s.period; i++ {
			change := s.prices[i] - s.prices[i-1]
			if change > 0 {
				gainSum += change
			} else {
				lossSum -= change
			}
		}
		s.avgGain = gainSum / float64(s.period)
		s.avgLoss = lossSum / float64(s.period)
		s.seeded = true
		// trim to keep only the last price for next delta
		s.prices = s.prices[len(s.prices)-1:]
	} else {
		// Wilder's smoothing for subsequent values
		change := s.prices[len(s.prices)-1] - s.prices[len(s.prices)-2]
		var gain, loss float64
		if change > 0 {
			gain = change
		} else {
			loss = -change
		}
		s.avgGain = (s.avgGain*float64(s.period-1) + gain) / float64(s.period)
		s.avgLoss = (s.avgLoss*float64(s.period-1) + loss) / float64(s.period)
		// keep only last price
		s.prices = s.prices[len(s.prices)-1:]
	}

	if s.avgLoss == 0 {
		return 100, true
	}
	rs := s.avgGain / s.avgLoss
	return 100 - (100 / (1 + rs)), true
}

// RSIEngine processes price ticks and emits RSI values at 1s and 1m resolutions.
// Architecture mirrors EMAEngine exactly.
type RSIEngine struct {
	input      chan PriceUpdateEvent
	out        chan RSIUpdateEvent // 1s output
	outMin     chan RSIUpdateEvent // 1m output
	states     map[int]*rsiState
	buckets    map[int]*bucket // latest RSI per instrument per second
	minBuckets map[int]*bucket // latest 1s RSI seen within the minute
	period     int
	mu         sync.Mutex
}

func NewRSIEngine(period int) *RSIEngine {
	return &RSIEngine{
		input:      make(chan PriceUpdateEvent, 1024),
		out:        make(chan RSIUpdateEvent, 1024),
		outMin:     make(chan RSIUpdateEvent, 1024),
		states:     make(map[int]*rsiState),
		buckets:    make(map[int]*bucket),
		minBuckets: make(map[int]*bucket),
		period:     period,
	}
}

func (e *RSIEngine) Input() chan<- PriceUpdateEvent {
	return e.input
}

func (e *RSIEngine) InputLen() int {
	return len(e.input)
}

func (e *RSIEngine) Output() <-chan RSIUpdateEvent {
	return e.out
}

func (e *RSIEngine) OutputMin() <-chan RSIUpdateEvent {
	return e.outMin
}

func (e *RSIEngine) Run() {
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

func (e *RSIEngine) process(event PriceUpdateEvent) {
	e.mu.Lock()
	state, ok := e.states[event.InstrumentID]
	if !ok {
		state = newRSIState(e.period)
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

func (e *RSIEngine) flushSeconds() {
	e.mu.Lock()
	defer e.mu.Unlock()

	ts := time.Now().UnixNano()

	for instrumentID, b := range e.buckets {
		if !b.hasValue {
			continue
		}

		select {
		case e.out <- RSIUpdateEvent{
			InstrumentID: instrumentID,
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

func (e *RSIEngine) flushMinutes() {
	e.mu.Lock()
	defer e.mu.Unlock()

	ts := time.Now().UnixNano()

	for instrumentID, mb := range e.minBuckets {
		if !mb.hasValue {
			continue
		}

		select {
		case e.outMin <- RSIUpdateEvent{
			InstrumentID: instrumentID,
			Value:        mb.value,
			Timestamp:    ts,
			Resolution:   "1m",
		}:
		default:
		}

		mb.hasValue = false
	}
}
