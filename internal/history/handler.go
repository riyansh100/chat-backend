package history

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

var validIndicators = map[string]bool{
	"sma": true, "ema": true, "rsi": true,
	"macd": true, "bb": true, "ohlc": true,
}

var validResolutions = map[string]bool{
	"1m": true, "1h": true,
}

// expectedPoints returns the minimum points we expect Redis to have.
// if Redis has less, we fall back to Postgres.
func expectedPoints(hours int, resolution string) int {
	if resolution == "1h" {
		return hours
	}
	// 1m: 1 point/min, allow 20% buffer for gaps
	return int(float64(hours*60) * 0.8)
}

// Handler serves GET /history?instrument=101&indicator=sma&hours=3&resolution=1m
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

		// --- fall back to Postgres if Redis is empty or insufficient ---
		if err != nil || len(points) < expectedPoints(hours, resolution) {
			pgPoints, pgErr := store.FallbackFromPostgres(ctx, indicator, instrumentID, fromUnix, now)
			if pgErr == nil && len(pgPoints) > 0 {
				points = pgPoints
				source = "postgres"
				// backfill Redis async so next request is fast
				go store.BackfillRedis(indicator, instrumentID, resolution, pgPoints)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instrument": instrumentID,
			"indicator":  indicator,
			"resolution": resolution,
			"hours":      hours,
			"from":       fromUnix,
			"to":         now,
			"count":      len(points),
			"source":     source,
			"points":     points,
		})
	}
}
