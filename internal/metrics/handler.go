package metrics

import (
	"encoding/json"
	"net/http"
	"runtime"
)

// Snapshot holds a point-in-time reading of all system metrics.
type Snapshot struct {
	// Hub counters
	EventsIngested    int64 `json:"events_ingested"`
	EventsBroadcasted int64 `json:"events_broadcasted"`
	MessagesDelivered int64 `json:"messages_delivered"`
	MessagesDropped   int64 `json:"messages_dropped"`
	ActiveClients     int64 `json:"active_clients"`
	ActiveRooms       int64 `json:"active_rooms"`

	// Engine health — how full are the input channels (cap=1024 each)
	SMAInputLen  int `json:"sma_input_len"`
	OHLCInputLen int `json:"ohlc_input_len"`

	// Go runtime
	Goroutines int     `json:"goroutines"`
	HeapMB     float64 `json:"heap_mb"`
	SysMB      float64 `json:"sys_mb"`
}

// Handler returns an HTTP handler that serves a JSON metrics snapshot.
// smaLen and ohlcLen are functions so we read channel length at request time.
func Handler(m *HubMetrics, smaLen func() int, ohlcLen func() int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		snap := Snapshot{
			EventsIngested:    m.EventsIngested.Load(),
			EventsBroadcasted: m.EventsBroadcasted.Load(),
			MessagesDelivered: m.MessagesDelivered.Load(),
			MessagesDropped:   m.MessagesDropped.Load(),
			ActiveClients:     m.ActiveClients.Load(),
			ActiveRooms:       m.ActiveRooms.Load(),

			SMAInputLen:  smaLen(),
			OHLCInputLen: ohlcLen(),

			Goroutines: runtime.NumGoroutine(),
			HeapMB:     float64(mem.HeapAlloc) / 1024 / 1024,
			SysMB:      float64(mem.Sys) / 1024 / 1024,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snap)
	}
}
