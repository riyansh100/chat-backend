package analytics

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	maxEntries1s = 3600 // 60 minutes of 1s data
	maxEntries1m = 1440 // 24 hours of 1m data
)

type SMAStore struct {
	rdb *redis.Client
}

func NewSMAStore(rdb *redis.Client) *SMAStore {
	return &SMAStore{rdb: rdb}
}

func key1s(instrumentID int) string {
	return fmt.Sprintf("sma:1s:%d", instrumentID)
}

func key1m(instrumentID int) string {
	return fmt.Sprintf("sma:1m:%d", instrumentID)
}

// Write stores a bucketed SMA value under the correct resolution key.
func (s *SMAStore) Write(ctx context.Context, event SMAUpdateEvent) error {
	var k string
	var maxEntries int64

	switch event.Resolution {
	case "1m":
		k = key1m(event.InstrumentID)
		maxEntries = maxEntries1m
	default: // "1s"
		k = key1s(event.InstrumentID)
		maxEntries = maxEntries1s
	}

	ts := time.Now().Unix()

	err := s.rdb.ZAdd(ctx, k, redis.Z{
		Score:  float64(ts),
		Member: strconv.FormatFloat(event.Value, 'f', 6, 64),
	}).Err()
	if err != nil {
		return err
	}

	s.rdb.ZRemRangeByRank(ctx, k, 0, -maxEntries-1)
	return nil
}

// GetLast fetches the most recent n SMA values for a given resolution.
func (s *SMAStore) GetLast(ctx context.Context, instrumentID int, n int, resolution string) ([]redis.Z, error) {
	var k string
	if resolution == "1m" {
		k = key1m(instrumentID)
	} else {
		k = key1s(instrumentID)
	}
	return s.rdb.ZRangeWithScores(ctx, k, int64(-n), -1).Result()
}

// GetRange fetches SMA values between two unix timestamps for a given resolution.
func (s *SMAStore) GetRange(ctx context.Context, instrumentID int, fromUnix, toUnix int64, resolution string) ([]redis.Z, error) {
	var k string
	if resolution == "1m" {
		k = key1m(instrumentID)
	} else {
		k = key1s(instrumentID)
	}
	return s.rdb.ZRangeByScoreWithScores(ctx, k, &redis.ZRangeBy{
		Min: strconv.FormatInt(fromUnix, 10),
		Max: strconv.FormatInt(toUnix, 10),
	}).Result()
}
