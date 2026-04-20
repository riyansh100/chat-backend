// internal/history/rollup.go
package history

import (
	"context"
	"log"
	"math"
	"time"

	bincod "github.com/riyansh/chat-backend/internal/binary"
	"github.com/riyansh/chat-backend/internal/domain/trading"
)

var indicators = []string{"sma", "ema", "rsi", "macd", "bb", "ohlc"}

// StartRollupJob runs the hourly rollup on a ticker.
// Every hour it reads the last 60 1m binary entries per indicator per
// instrument, averages (or merges for OHLC), and writes a binary 1h member.
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
	points, err := store.GetLastN(ctx, indicator, instrumentID, "1m", 60)
	if err != nil || len(points) == 0 {
		return err
	}

	var value []byte

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

	if value == nil {
		return nil
	}

	return store.Write1h(ctx, indicator, instrumentID, ts, value)
}

// avgScalar averages scalar binary points and returns a binary 8-byte member.
func avgScalar(points []Point) []byte {
	var sum float64
	var count int
	for _, p := range points {
		v, err := bincod.DecodeScalar(p.Value)
		if err != nil {
			continue
		}
		sum += v
		count++
	}
	if count == 0 {
		return nil
	}
	return bincod.EncodeScalar(sum / float64(count))
}

// avgMACD averages macd_line / signal_line / histogram binary points.
func avgMACD(points []Point) []byte {
	var macdSum, signalSum, histSum float64
	var count int
	for _, p := range points {
		macdLine, signalLine, histogram, err := bincod.DecodeMACD(p.Value)
		if err != nil {
			continue
		}
		macdSum += macdLine
		signalSum += signalLine
		histSum += histogram
		count++
	}
	if count == 0 {
		return nil
	}
	n := float64(count)
	return bincod.EncodeMACD(macdSum/n, signalSum/n, histSum/n)
}

// avgBB averages upper / middle / lower binary points.
func avgBB(points []Point) []byte {
	var upperSum, middleSum, lowerSum float64
	var count int
	for _, p := range points {
		upper, middle, lower, err := bincod.DecodeBB(p.Value)
		if err != nil {
			continue
		}
		upperSum += upper
		middleSum += middle
		lowerSum += lower
		count++
	}
	if count == 0 {
		return nil
	}
	n := float64(count)
	return bincod.EncodeBB(upperSum/n, middleSum/n, lowerSum/n)
}

// mergeOHLC builds a proper 1h candle from 1m binary candles.
// Open = first open, High = max high, Low = min low, Close = last close.
func mergeOHLC(points []Point) []byte {
	var open, high, low, close float64
	high = -math.MaxFloat64
	low = math.MaxFloat64
	first := true

	for _, p := range points {
		o, h, l, c, err := bincod.DecodeOHLC(p.Value)
		if err != nil {
			continue
		}
		if first {
			open = o
			first = false
		}
		if h > high {
			high = h
		}
		if l < low {
			low = l
		}
		close = c
	}

	if first {
		return nil
	}
	return bincod.EncodeOHLC(open, high, low, close)
}
