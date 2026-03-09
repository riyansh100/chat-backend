package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const maxOHLCEntries = 1440 // 24 hours of 1m candles

type OHLCStore struct {
	rdb  *redis.Client
	pool *pgxpool.Pool
}

func NewOHLCStore(rdb *redis.Client, pool *pgxpool.Pool) *OHLCStore {
	return &OHLCStore{rdb: rdb, pool: pool}
}

func ohlcKey(instrumentID int) string {
	return fmt.Sprintf("ohlc:1m:%d", instrumentID)
}

// Write stores the candle in Redis (fast warm-start) and Postgres (data lake).
func (s *OHLCStore) Write(ctx context.Context, event OHLCEvent) error {
	if err := s.writeRedis(ctx, event); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	if err := s.writePostgres(ctx, event); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	return nil
}

func (s *OHLCStore) writeRedis(ctx context.Context, event OHLCEvent) error {
	payload, err := json.Marshal(map[string]interface{}{
		"open":  event.Open,
		"high":  event.High,
		"low":   event.Low,
		"close": event.Close,
		"ts":    event.Timestamp,
	})
	if err != nil {
		return err
	}

	key := ohlcKey(event.InstrumentID)
	ts := float64(event.Timestamp)

	err = s.rdb.ZAdd(ctx, key, redis.Z{
		Score:  ts,
		Member: string(payload),
	}).Err()
	if err != nil {
		return err
	}

	s.rdb.ZRemRangeByRank(ctx, key, 0, -maxOHLCEntries-1)
	return nil
}

func (s *OHLCStore) writePostgres(ctx context.Context, event OHLCEvent) error {
	t := time.Unix(event.Timestamp, 0).UTC()

	_, err := s.pool.Exec(ctx,
		`INSERT INTO ohlc (time, instrument, resolution, open, high, low, close)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t,
		event.InstrumentID,
		event.Resolution,
		event.Open,
		event.High,
		event.Low,
		event.Close,
	)
	return err
}

// GetLast fetches the most recent n candles from Redis for a given instrument.
func (s *OHLCStore) GetLast(ctx context.Context, instrumentID int, n int) ([]redis.Z, error) {
	return s.rdb.ZRangeWithScores(ctx, ohlcKey(instrumentID), int64(-n), -1).Result()
}

// GetRange fetches candles between two unix timestamps from Redis.
func (s *OHLCStore) GetRange(ctx context.Context, instrumentID int, fromUnix, toUnix int64) ([]redis.Z, error) {
	return s.rdb.ZRangeByScoreWithScores(ctx, ohlcKey(instrumentID), &redis.ZRangeBy{
		Min: strconv.FormatInt(fromUnix, 10),
		Max: strconv.FormatInt(toUnix, 10),
	}).Result()
}
