// internal/history/handler.go
package history

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	bincod "github.com/riyansh/chat-backend/internal/binary"
)

var validIndicators = map[string]bool{
	"sma": true, "ema": true, "rsi": true,
	"macd": true, "bb": true, "ohlc": true,
}

var validResolutions = map[string]bool{
	"1m": true, "1h": true,
}

// decodePoint converts a binary Point.Value back into a JSON-friendly map.
// indicator is one of sma|ema|rsi|macd|bb|ohlc.
func decodePoint(indicator string, p Point) map[string]interface{} {
	ts := p.Ts
	switch indicator {
	case "sma", "ema", "rsi":
		v, err := bincod.DecodeScalar(p.Value)
		if err != nil {
			return map[string]interface{}{"ts": ts, "value": nil}
		}
		return map[string]interface{}{"ts": ts, "value": v}

	case "macd":
		macdLine, signalLine, histogram, err := bincod.DecodeMACD(p.Value)
		if err != nil {
			return map[string]interface{}{"ts": ts}
		}
		return map[string]interface{}{
			"ts": ts, "macd_line": macdLine,
			"signal_line": signalLine, "histogram": histogram,
		}

	case "bb":
		upper, middle, lower, err := bincod.DecodeBB(p.Value)
		if err != nil {
			return map[string]interface{}{"ts": ts}
		}
		return map[string]interface{}{
			"ts": ts, "upper": upper, "middle": middle, "lower": lower,
		}

	case "ohlc":
		open, high, low, close, err := bincod.DecodeOHLC(p.Value)
		if err != nil {
			return map[string]interface{}{"ts": ts}
		}
		return map[string]interface{}{
			"ts": ts, "open": open, "high": high, "low": low, "close": close,
		}
	}
	return map[string]interface{}{"ts": ts}
}

// Handler serves GET /history?instrument=101&indicator=sma&hours=3&resolution=1m
//
// Binary Points are decoded to JSON here — this is the only place in the
// codebase where binary→JSON conversion occurs for history data, and it only
// runs on the path to the frontend client.
func Handler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		instrumentStr := q.Get("instrument")
		indicator := q.Get("indicator")
		hoursStr := q.Get("hours")
		resolution := q.Get("resolution")

		if instrumentStr == "" || indicator == "" {
			http.Error(w, "missing instrument or indicator", http.StatusBadRequest)
			return
		}

		instrumentID, err := strconv.Atoi(instrumentStr)
		if err != nil {
			http.Error(w, "invalid instrument", http.StatusBadRequest)
			return
		}

		if !validIndicators[indicator] {
			http.Error(w, "invalid indicator (sma|ema|rsi|macd|bb|ohlc)", http.StatusBadRequest)
			return
		}

		if resolution == "" {
			resolution = "1m"
		}
		if !validResolutions[resolution] {
			http.Error(w, "invalid resolution (1m|1h)", http.StatusBadRequest)
			return
		}

		hours := 3
		if hoursStr != "" {
			if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 && h <= 168 {
				hours = h
			}
		}

		now := time.Now().Unix()
		fromUnix := now - int64(hours*3600)
		ctx := r.Context()

		source := "redis"

		// --- try Redis first ---
		points, err := store.GetRange(ctx, indicator, instrumentID, resolution, fromUnix, now)

		// --- fall back to Postgres only on error or truly empty ---
		if err != nil || len(points) == 0 {
			pgPoints, pgErr := store.FallbackFromPostgres(ctx, indicator, instrumentID, fromUnix, now)
			if pgErr == nil && len(pgPoints) > 0 {
				points = pgPoints
				source = "postgres"
				// backfill Redis async so next request is a Redis hit
				go store.BackfillRedis(indicator, instrumentID, resolution, pgPoints)
			}
		}

		// Decode binary members → JSON-friendly maps (frontend boundary)
		decoded := make([]map[string]interface{}, 0, len(points))
		for _, p := range points {
			decoded = append(decoded, decodePoint(indicator, p))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instrument": instrumentID,
			"indicator":  indicator,
			"resolution": resolution,
			"hours":      hours,
			"from":       fromUnix,
			"to":         now,
			"count":      len(decoded),
			"source":     source,
			"points":     decoded,
		})
	}
}
