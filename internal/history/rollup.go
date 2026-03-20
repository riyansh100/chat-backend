package history

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"time"

	"github.com/riyansh/chat-backend/internal/domain/trading"
)

var indicators = []string{"sma", "ema", "rsi", "macd", "bb", "ohlc"}

// StartRollupJob runs the hourly rollup on a ticker.
// Every hour it reads the last 60 1m entries per indicator per instrument,
// averages them (or merges for OHLC), and writes to hist:1h.
func StartRollupJob(ctx context.Context, store *Store) {
	// run once immediately on startup to pre-warm
	runRollup(ctx, store)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runRollup(ctx, store)
		}
	}
}

func runRollup(ctx context.Context, store *Store) {
	log.Println("[Rollup] starting hourly rollup...")
	now := time.Now().Unix()

	for _, indicator := range indicators {
		for _, inst := range trading.Instruments {
			if err := rollupOne(ctx, store, indicator, inst.ID, now); err != nil {
				log.Printf("[Rollup] %s:%d error: %v", indicator, inst.ID, err)
			}
		}
	}
	log.Println("[Rollup] done.")
}

func rollupOne(ctx context.Context, store *Store, indicator string, instrumentID int, ts int64) error {
	// fetch last 60 1m points
	points, err := store.GetLastN(ctx, indicator, instrumentID, "1m", 60)
	if err != nil || len(points) == 0 {
		return err
	}

	var value string

	switch indicator {
	case "sma", "ema", "rsi":
		value = avgScalar(points)

	case "macd":
		value = avgMACD(points)

	case "bb":
		value = avgBB(points)

	case "ohlc":
		value = mergeOHLC(points)
	}

	if value == "" {
		return nil
	}

	return store.Write1h(ctx, indicator, instrumentID, ts, value)
}

// avgScalar averages a slice of scalar float points.
func avgScalar(points []Point) string {
	var sum float64
	var count int
	for _, p := range points {
		v, err := strconv.ParseFloat(p.Value, 64)
		if err != nil {
			continue
		}
		sum += v
		count++
	}
	if count == 0 {
		return ""
	}
	return strconv.FormatFloat(sum/float64(count), 'f', 6, 64)
}

// avgMACD averages macd_line, signal_line, histogram across all 1m points.
func avgMACD(points []Point) string {
	var macdSum, signalSum, histSum float64
	var count int
	for _, p := range points {
		var m struct {
			MACDLine   float64 `json:"macd_line"`
			SignalLine float64 `json:"signal_line"`
			Histogram  float64 `json:"histogram"`
		}
		if err := json.Unmarshal([]byte(p.Value), &m); err != nil {
			continue
		}
		macdSum += m.MACDLine
		signalSum += m.SignalLine
		histSum += m.Histogram
		count++
	}
	if count == 0 {
		return ""
	}
	b, _ := json.Marshal(map[string]float64{
		"macd_line":   macdSum / float64(count),
		"signal_line": signalSum / float64(count),
		"histogram":   histSum / float64(count),
	})
	return string(b)
}

// avgBB averages upper, middle, lower across all 1m points.
func avgBB(points []Point) string {
	var upperSum, middleSum, lowerSum float64
	var count int
	for _, p := range points {
		var b struct {
			Upper  float64 `json:"upper"`
			Middle float64 `json:"middle"`
			Lower  float64 `json:"lower"`
		}
		if err := json.Unmarshal([]byte(p.Value), &b); err != nil {
			continue
		}
		upperSum += b.Upper
		middleSum += b.Middle
		lowerSum += b.Lower
		count++
	}
	if count == 0 {
		return ""
	}
	out, _ := json.Marshal(map[string]float64{
		"upper":  upperSum / float64(count),
		"middle": middleSum / float64(count),
		"lower":  lowerSum / float64(count),
	})
	return string(out)
}

// mergeOHLC builds a proper 1h candle from 1m candles.
// Open = first candle's open, High = max high, Low = min low, Close = last candle's close.
func mergeOHLC(points []Point) string {
	type candle struct {
		Open  float64 `json:"open"`
		High  float64 `json:"high"`
		Low   float64 `json:"low"`
		Close float64 `json:"close"`
	}

	var open, high, low, close float64
	first := true

	for _, p := range points {
		var c candle
		if err := json.Unmarshal([]byte(p.Value), &c); err != nil {
			continue
		}
		if first {
			open = c.Open
			high = c.High
			low = c.Low
			close = c.Close
			first = false
			continue
		}
		if c.High > high {
			high = c.High
		}
		if c.Low < low {
			low = c.Low
		}
		close = c.Close
	}

	if first {
		return ""
	}

	out, _ := json.Marshal(map[string]float64{
		"open": open, "high": high, "low": low, "close": close,
	})
	return string(out)
}
