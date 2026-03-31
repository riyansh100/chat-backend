package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// ---- global counters ----
var (
	totalReceived atomic.Int64
	totalDropped  atomic.Int64
	connFailed    atomic.Int64
	reconnects    atomic.Int64

	latencyMu      sync.Mutex
	latencySamples []int64
)

func recordLatency(us int64) {
	if us <= 0 || us > 30_000_000 {
		return
	}
	latencyMu.Lock()
	latencySamples = append(latencySamples, us)
	latencyMu.Unlock()
}

func flushLatency() (min, avg, max, p95 float64, count int) {
	latencyMu.Lock()
	samples := latencySamples
	latencySamples = nil
	latencyMu.Unlock()

	count = len(samples)
	if count == 0 {
		return
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

	sorted := make([]int64, count)
	copy(sorted, samples)
	p95idx := int(float64(count) * 0.95)
	for i := 1; i <= p95idx && i < count; i++ {
		key := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j] > key {
			sorted[j+1] = sorted[j]
			j--
		}
		sorted[j+1] = key
	}

	toMs := func(v int64) float64 { return float64(v) / 1000.0 }
	return toMs(minVal),
		float64(sum) / float64(count) / 1000.0,
		toMs(maxVal),
		toMs(sorted[p95idx]),
		count
}

type Snapshot struct {
	ElapsedS   float64
	Clients    int
	MsgRate    float64
	TotalMsgs  int64
	Dropped    int64
	ConnFailed int64
	Reconnects int64
	AvgLatMs   float64
	P95LatMs   float64
	MaxLatMs   float64
	HeapMB     float64
}

var (
	snapshotMu sync.Mutex
	snapshots  []Snapshot
)

func addSnapshot(s Snapshot) {
	snapshotMu.Lock()
	snapshots = append(snapshots, s)
	snapshotMu.Unlock()
}

type hubMetrics struct {
	HeapMB float64 `json:"heap_mb"`
}

func pollMetrics(metricsURL string) float64 {
	resp, err := http.Get(metricsURL)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var m hubMetrics
	if err := json.Unmarshal(body, &m); err != nil {
		return -1
	}
	return m.HeapMB
}

func runConsumer(id int, host string, rooms []string, stop chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	u := url.URL{Scheme: "ws", Host: host, Path: "/ws"}

	var conn *websocket.Conn
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		conn, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		log.Printf("[client %d] connect failed: %v", id, err)
		connFailed.Add(1)
		return
	}
	defer conn.Close()

	for _, room := range rooms {
		conn.WriteJSON(map[string]string{"type": "join", "room": room})
	}

	// Read and process inline — no intermediate channel, no tester-side drops.
	// When stop closes we set a short deadline so ReadJSON unblocks promptly.
	go func() {
		<-stop
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	}()

	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		totalReceived.Add(1)

		if data, ok := msg["data"].(map[string]interface{}); ok {
			if ts, ok := data["timestamp"].(float64); ok && ts > 0 {
				latencyUs := (time.Now().UnixNano() - int64(ts)) / 1000
				recordLatency(latencyUs)
			}
			if ts, ok := data["ingested_at"].(float64); ok && ts > 0 {
				latencyUs := (time.Now().UnixNano() - int64(ts)) / 1000
				recordLatency(latencyUs)
			}
		}
	}
}

// allRooms builds a comma-separated string of all 300 instrument IDs.
func allRooms() string {
	ids := make([]string, 300)
	for i := 0; i < 300; i++ {
		ids[i] = fmt.Sprintf("%d", 101+i)
	}
	return strings.Join(ids, ",")
}

func main() {
	clients := flag.Int("clients", 200, "concurrent consumers (phase1=200, phase2=500, phase3=1000)")
	rooms := flag.String("rooms", allRooms(), "comma-separated instrument IDs (default: all 300)")
	duration := flag.Int("duration", 120, "test duration in seconds")
	host := flag.String("host", "localhost:80", "nginx address")
	metrics := flag.String("metrics", "http://localhost:8081/metrics", "dataserver metrics URL")
	phase := flag.String("phase", "1", "phase label for output (1/2/3)")
	rampMs := flag.Int("ramp-ms", 10, "ms delay between each client connect (reduce for faster ramp)")
	flag.Parse()

	roomList := strings.Split(*rooms, ",")

	fmt.Printf("\n========================================\n")
	fmt.Printf("  LOAD TEST  phase=%s  clients=%d\n", *phase, *clients)
	fmt.Printf("  instruments=%d  duration=%ds\n", len(roomList), *duration)
	fmt.Printf("  target=%s  ramp=%dms/client\n", *host, *rampMs)
	fmt.Printf("========================================\n")

	// phase hints
	switch *phase {
	case "1":
		fmt.Println("  PHASE 1 — baseline (200 clients, all 300 instruments)")
	case "2":
		fmt.Println("  PHASE 2 — load (500 clients, all 300 instruments)")
	case "3":
		fmt.Println("  PHASE 3 — stress (1000 clients, all 300 instruments)")
		fmt.Println("  TIP: kill leader at t=30s, kill Redis primary at t=60s")
	}
	fmt.Println()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	start := time.Now()

	for i := 0; i < *clients; i++ {
		wg.Add(1)
		go runConsumer(i, *host, roomList, stop, &wg)
		time.Sleep(time.Duration(*rampMs) * time.Millisecond)
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		var lastTotal int64

		fmt.Printf("%-8s %-8s %-12s %-10s %-10s %-10s %-10s %-10s\n",
			"time(s)", "rate/s", "total", "dropped", "avg_ms", "p95_ms", "max_ms", "heap_mb")
		fmt.Println(strings.Repeat("-", 82))

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				elapsed := time.Since(start).Seconds()
				current := totalReceived.Load()
				delta := current - lastTotal
				lastTotal = current
				rate := float64(delta) / 10.0

				_, avgL, maxL, p95L, lcount := flushLatency()
				heapMB := pollMetrics(*metrics)

				snap := Snapshot{
					ElapsedS:   elapsed,
					Clients:    *clients,
					MsgRate:    rate,
					TotalMsgs:  current,
					Dropped:    totalDropped.Load(),
					ConnFailed: connFailed.Load(),
					Reconnects: reconnects.Load(),
					HeapMB:     heapMB,
				}
				if lcount > 0 {
					snap.AvgLatMs = avgL
					snap.P95LatMs = p95L
					snap.MaxLatMs = maxL
				}
				addSnapshot(snap)

				if lcount > 0 {
					fmt.Printf("%-8.0f %-8.1f %-12d %-10d %-10.2f %-10.2f %-10.2f %-10.2f\n",
						elapsed, rate, current, totalDropped.Load(),
						avgL, p95L, maxL, heapMB)
				} else {
					fmt.Printf("%-8.0f %-8.1f %-12d %-10d %-10s %-10s %-10s %-10.2f\n",
						elapsed, rate, current, totalDropped.Load(),
						"n/a", "n/a", "n/a", heapMB)
				}
			}
		}
	}()

	time.Sleep(time.Duration(*duration) * time.Second)
	close(stop)
	wg.Wait()

	_, avgL, maxL, p95L, lcount := flushLatency()

	fmt.Printf("\n========================================\n")
	fmt.Printf("  FINAL SUMMARY  phase=%s\n", *phase)
	fmt.Printf("========================================\n")
	fmt.Printf("  Clients           : %d\n", *clients)
	fmt.Printf("  Instruments       : %d\n", len(roomList))
	fmt.Printf("  Duration          : %ds\n", *duration)
	fmt.Printf("  Total received    : %d\n", totalReceived.Load())
	fmt.Printf("  Total dropped     : %d\n", totalDropped.Load())
	fmt.Printf("  Connect failures  : %d\n", connFailed.Load())
	fmt.Printf("  Avg rate          : %.1f msg/s\n",
		float64(totalReceived.Load())/float64(*duration))
	if lcount > 0 {
		fmt.Printf("  Latency avg       : %.2fms\n", avgL)
		fmt.Printf("  Latency p95       : %.2fms\n", p95L)
		fmt.Printf("  Latency max       : %.2fms\n", maxL)
	}
	fmt.Printf("  Drop rate         : %.2f%%\n",
		100.0*float64(totalDropped.Load())/
			math.Max(1, float64(totalReceived.Load()+totalDropped.Load())))

	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	if len(snapshots) > 0 {
		fmt.Printf("\n  TIMELINE\n  %-8s %-10s %-10s %-10s %-10s\n",
			"time(s)", "rate/s", "avg_ms", "p95_ms", "heap_mb")
		fmt.Println("  " + strings.Repeat("-", 52))
		for _, s := range snapshots {
			fmt.Printf("  %-8.0f %-10.1f %-10.2f %-10.2f %-10.2f\n",
				s.ElapsedS, s.MsgRate, s.AvgLatMs, s.P95LatMs, s.HeapMB)
		}
	}
	fmt.Println()
}
