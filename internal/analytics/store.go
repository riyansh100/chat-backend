package analytics

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	smaKeyPrefix = "sma:1s:" // sma:1s:{instrumentID}
	maxEntries   = 3600      // keep last 60 minutes (1 entry/sec × 3600s)
)

// SMAStore writes bucketed SMA values into Redis Sorted Sets.
// Key:   sma:1s:{instrumentID}
// Score: unix timestamp (seconds)
// Value: SMA value as string
type SMAStore struct {
	rdb *redis.Client
}

func NewSMAStore(rdb *redis.Client) *SMAStore {
	return &SMAStore{rdb: rdb}
}

func (s *SMAStore) key(instrumentID int) string {
	return fmt.Sprintf("%s%d", smaKeyPrefix, instrumentID)
}

// Write stores one SMA value for an instrument at the current timestamp,
// then trims the sorted set to the last maxEntries entries.
func (s *SMAStore) Write(ctx context.Context, event SMAUpdateEvent) error {
	key := s.key(event.InstrumentID)
	ts := time.Now().Unix() // score = unix seconds

	// Add to sorted set: score=timestamp, member=sma_value
	err := s.rdb.ZAdd(ctx, key, redis.Z{
		Score:  float64(ts),
		Member: strconv.FormatFloat(event.Value, 'f', 6, 64),
	}).Err()
	if err != nil {
		return err
	}

	// Trim: keep only last maxEntries (removes oldest by rank)
	s.rdb.ZRemRangeByRank(ctx, key, 0, -maxEntries-1)

	return nil
}

// GetRange fetches SMA values for an instrument between two unix timestamps.
// Returns slice of (timestamp, value) pairs in ascending order.
func (s *SMAStore) GetRange(ctx context.Context, instrumentID int, fromUnix, toUnix int64) ([]redis.Z, error) {
	key := s.key(instrumentID)
	return s.rdb.ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
		Min: strconv.FormatInt(fromUnix, 10),
		Max: strconv.FormatInt(toUnix, 10),
	}).Result()
}

// GetLast fetches the most recent N SMA values for an instrument.
func (s *SMAStore) GetLast(ctx context.Context, instrumentID int, n int) ([]redis.Z, error) {
	key := s.key(instrumentID)
	return s.rdb.ZRangeWithScores(ctx, key, int64(-n), -1).Result()
}
