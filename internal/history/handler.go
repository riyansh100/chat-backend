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
func Handler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		// --- parse + validate params ---
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

		hours := 3 // default
		if hoursStr != "" {
			if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 && h <= 168 {
				hours = h
			}
		}

		// --- compute time range ---
		now := time.Now().Unix()
		fromUnix := now - int64(hours*3600)

		// --- fetch from Redis ---
		ctx := r.Context()
		points, err := store.GetRange(ctx, indicator, instrumentID, resolution, fromUnix, now)

		// --- fallback to Postgres if Redis miss ---
		if err != nil || len(points) == 0 {
			points, err = store.FallbackFromPostgres(ctx, indicator, instrumentID, fromUnix, now)
			if err != nil {
				http.Error(w, "data unavailable", http.StatusInternalServerError)
				return
			}
		}

		// --- respond ---
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"instrument": instrumentID,
			"indicator":  indicator,
			"resolution": resolution,
			"hours":      hours,
			"from":       fromUnix,
			"to":         now,
			"count":      len(points),
			"points":     points,
		})
	}
}
