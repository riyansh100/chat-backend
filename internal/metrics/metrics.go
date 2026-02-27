package metrics

import (
	"log"
	"sync/atomic"
	"time"
)

type HubMetrics struct {
	EventsIngested    atomic.Int64
	EventsBroadcasted atomic.Int64
	MessagesDelivered atomic.Int64
	MessagesDropped   atomic.Int64
	ActiveClients     atomic.Int64
	ActiveRooms       atomic.Int64
}

func (m *HubMetrics) StartLogger() {
	ticker := time.NewTicker(5 * time.Second)

	go func() {
		for range ticker.C {
			log.Printf(`
==== HUB METRICS ====
Events Ingested:    %d
Events Broadcasted: %d
Messages Delivered: %d
Messages Dropped:   %d
Active Clients:     %d
Active Rooms:       %d
=====================`,
				m.EventsIngested.Load(),
				m.EventsBroadcasted.Load(),
				m.MessagesDelivered.Load(),
				m.MessagesDropped.Load(),
				m.ActiveClients.Load(),
				m.ActiveRooms.Load(),
			)
		}
	}()
}
