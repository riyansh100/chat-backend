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

// Handler serves GET /history?instrument=101&indicator=sma&hours=3&resolution=1m
//
// Fallback policy: only hit Postgres when Redis returns an actual error or
// zero points. Previously we required >= expectedPoints(hours, resolution)
// which caused Postgres fallbacks on every request when the backend had been
// running < hours (e.g. 30min uptime → only 30 points vs 144 required for
// 3h × 0.8). Under 300-user load that meant 300 simultaneous Postgres queries
// → 1900ms p95 on /history.
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
		// Do NOT require a minimum point count — Redis may legitimately have
		// fewer points than the full window if the server recently restarted.
		// Serving partial Redis data is always faster than a Postgres round-trip.
		if err != nil || len(points) == 0 {
			pgPoints, pgErr := store.FallbackFromPostgres(ctx, indicator, instrumentID, fromUnix, now)
			if pgErr == nil && len(pgPoints) > 0 {
				points = pgPoints
				source = "postgres"
				// backfill Redis async so next request is a Redis hit
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
