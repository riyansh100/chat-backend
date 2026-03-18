package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var (
	totalReceived atomic.Int64
	totalDropped  atomic.Int64

	latencyMu      sync.Mutex
	latencySamples []int64 // microseconds
)

func recordLatency(us int64) {
	latencyMu.Lock()
	latencySamples = append(latencySamples, us)
	latencyMu.Unlock()
}

func flushLatency() (min, avg, max float64, count int) {
	latencyMu.Lock()
	samples := latencySamples
	latencySamples = nil
	latencyMu.Unlock()

	count = len(samples)
	if count == 0 {
		return 0, 0, 0, 0
	}

	var sum int64
	minVal := int64(math.MaxInt64)
	maxVal := int64(0)
	for _, s := range samples {
		sum += s
		if s < minVal {
			minVal = s
		}
		if s > maxVal {
			maxVal = s
		}
	}
	return float64(minVal) / 1000.0,
		float64(sum) / float64(count) / 1000.0,
		float64(maxVal) / 1000.0,
		count
}

func main() {
	clients := flag.Int("clients", 10, "number of concurrent consumers")
	topics := flag.String("topics", "sma:101,ema:101", "comma-separated topics each client subscribes to")
	duration := flag.Int("duration", 60, "test duration in seconds")
	host := flag.String("host", "localhost:8080", "server address")
	flag.Parse()

	topicList := splitTopics(*topics)

	log.Printf("🚀 Pull load test starting: %d clients × topics=%s for %ds",
		*clients, *topics, *duration)
	log.Printf("   Each client subscribes to %d topic(s)", len(topicList))

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < *clients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runConsumer(id, *host, topicList, stop)
		}(i)
		time.Sleep(10 * time.Millisecond)
	}

	// reporter
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

				minL, avgL, maxL, lcount := flushLatency()

				if lcount > 0 {
					log.Printf("📊 [%.0fs] clients=%d  msgs/5s=%d  rate=%.1f/s  total=%d  dropped=%d  latency(min/avg/max)=%.2fms/%.2fms/%.2fms  samples=%d",
						elapsed, *clients, delta, float64(delta)/5.0,
						current, totalDropped.Load(),
						minL, avgL, maxL, lcount)
				} else {
					log.Printf("📊 [%.0fs] clients=%d  msgs/5s=%d  rate=%.1f/s  total=%d  dropped=%d  latency=n/a",
						elapsed, *clients, delta, float64(delta)/5.0,
						current, totalDropped.Load())
				}
			}
		}
	}()

	time.Sleep(time.Duration(*duration) * time.Second)
	close(stop)
	wg.Wait()

	minL, avgL, maxL, lcount := flushLatency()
	fmt.Printf("\n✅ Pull test complete.\n")
	fmt.Printf("   Topics per client       : %s\n", *topics)
	fmt.Printf("   Total messages received : %d\n", totalReceived.Load())
	fmt.Printf("   Total messages dropped  : %d\n", totalDropped.Load())
	fmt.Printf("   Avg rate                : %.1f msg/s across %d clients\n",
		float64(totalReceived.Load())/float64(*duration), *clients)
	if lcount > 0 {
		fmt.Printf("   Final latency batch     : min=%.2fms  avg=%.2fms  max=%.2fms  samples=%d\n",
			minL, avgL, maxL, lcount)
	}
}

func runConsumer(id int, host string, topics []string, stop chan struct{}) {
	u := url.URL{Scheme: "ws", Host: host, Path: "/ws"}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Printf("client %d connect failed: %v", id, err)
		totalDropped.Add(1)
		return
	}
	defer conn.Close()

	// subscribe to all topics via pull model
	for _, topic := range topics {
		if err := conn.WriteJSON(map[string]string{
			"type":  "subscribe",
			"topic": topic,
		}); err != nil {
			log.Printf("client %d subscribe failed: %v", id, err)
			return
		}
	}

	msgCh := make(chan map[string]interface{}, 256)

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
			// unsubscribe cleanly before exit
			for _, topic := range topics {
				conn.WriteJSON(map[string]string{
					"type":  "unsubscribe",
					"topic": topic,
				})
			}
			return

		case msg := <-msgCh:
			msgType, _ := msg["type"].(string)

			// skip ack messages (subscribed/unsubscribed), only count data
			if msgType == "subscribed" || msgType == "unsubscribed" {
				continue
			}

			totalReceived.Add(1)

			// latency measurement if timestamp is present in data
			if data, ok := msg["data"].(map[string]interface{}); ok {
				if ts, ok := data["timestamp"].(float64); ok && ts > 0 {
					latencyUs := (time.Now().UnixNano() - int64(ts)) / 1000
					if latencyUs > 0 && latencyUs < 10_000_000 { // sanity: ignore >10s
						recordLatency(latencyUs)
					}
				}
			}
		}
	}
}

func splitTopics(s string) []string {
	var result []string
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}
