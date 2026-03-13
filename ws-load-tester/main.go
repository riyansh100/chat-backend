package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var (
	totalReceived atomic.Int64
	totalDropped  atomic.Int64
)

func main() {
	clients := flag.Int("clients", 10, "number of concurrent consumers")
	rooms := flag.String("rooms", "101,102,103", "comma-separated instrument IDs each client subscribes to")
	duration := flag.Int("duration", 60, "how long to run the test in seconds")
	host := flag.String("host", "localhost:8080", "server address")
	flag.Parse()

	log.Printf("🚀 Load test starting: %d clients × rooms=%s for %ds", *clients, *rooms, *duration)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// spin up N consumers
	for i := 0; i < *clients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runConsumer(id, *host, *rooms, stop)
		}(i)
		// small stagger to avoid thundering herd on connect
		time.Sleep(10 * time.Millisecond)
	}

	// reporter — prints throughput every 5 seconds
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		var lastTotal int64
		start := time.Now()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				current := totalReceived.Load()
				delta := current - lastTotal
				lastTotal = current
				elapsed := time.Since(start).Seconds()
				log.Printf("📊 [%.0fs] clients=%d  msgs/5s=%d  rate=%.1f/s  total=%d  dropped=%d",
					elapsed,
					*clients,
					delta,
					float64(delta)/5.0,
					current,
					totalDropped.Load(),
				)
			}
		}
	}()

	// run for duration then stop
	time.Sleep(time.Duration(*duration) * time.Second)
	close(stop)
	wg.Wait()

	fmt.Printf("\n✅ Test complete.\n")
	fmt.Printf("   Total messages received: %d\n", totalReceived.Load())
	fmt.Printf("   Total messages dropped:  %d\n", totalDropped.Load())
	fmt.Printf("   Avg rate: %.1f msg/s across %d clients\n",
		float64(totalReceived.Load())/float64(*duration),
		*clients,
	)
}

func runConsumer(id int, host string, rooms string, stop chan struct{}) {
	u := url.URL{Scheme: "ws", Host: host, Path: "/ws"}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Printf("client %d connect failed: %v", id, err)
		totalDropped.Add(1)
		return
	}
	defer conn.Close()

	// subscribe to all requested rooms
	for _, room := range splitRooms(rooms) {
		conn.WriteJSON(map[string]string{"type": "join", "room": room})
	}

	// read messages until stop
	msgCh := make(chan map[string]interface{}, 128)

	// reader goroutine
	go func() {
		for {
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			select {
			case msgCh <- msg:
			default:
				totalDropped.Add(1)
			}
		}
	}()

	for {
		select {
		case <-stop:
			return
		case msg := <-msgCh:
			totalReceived.Add(1)
			// measure latency for price_update messages that carry ingested_at
			if data, ok := msg["data"].(map[string]interface{}); ok {
				if ts, ok := data["ingested_at"].(float64); ok && ts > 0 {
					latencyUs := (time.Now().UnixNano() - int64(ts)) / 1000
					if id == 0 {
						// only client 0 logs latency to avoid spam
						log.Printf("⚡ client-0 latency: %dµs", latencyUs)
					}
				}
			}
		}
	}
}

func splitRooms(rooms string) []string {
	var result []string
	current := ""
	for _, c := range rooms {
		if c == ',' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
