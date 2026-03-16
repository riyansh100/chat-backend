package analytics

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	maxEMAEntries1s = 3600 // 60 minutes of 1s data
	maxEMAEntries1m = 1440 // 24 hours of 1m data
)

type EMAStore struct {
	rdb  *redis.Client
	pool *pgxpool.Pool
}

func NewEMAStore(rdb *redis.Client, pool *pgxpool.Pool) *EMAStore {
	return &EMAStore{rdb: rdb, pool: pool}
}

func emaKey1s(instrumentID int) string {
	return fmt.Sprintf("ema:1s:%d", instrumentID)
}

func emaKey1m(instrumentID int) string {
	return fmt.Sprintf("ema:1m:%d", instrumentID)
}

// Write stores an EMA value in Redis sorted set and Postgres.
func (s *EMAStore) Write(ctx context.Context, event EMAUpdateEvent) error {
	if err := s.writeRedis(ctx, event); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	if s.pool != nil {
		if err := s.writePostgres(ctx, event); err != nil {
			return fmt.Errorf("postgres: %w", err)
		}
	}
	return nil
}

func (s *EMAStore) writeRedis(ctx context.Context, event EMAUpdateEvent) error {
	var k string
	var maxEntries int64

	switch event.Resolution {
	case "1m":
		k = emaKey1m(event.InstrumentID)
		maxEntries = maxEMAEntries1m
	default: // "1s"
		k = emaKey1s(event.InstrumentID)
		maxEntries = maxEMAEntries1s
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

func (s *EMAStore) writePostgres(ctx context.Context, event EMAUpdateEvent) error {
	t := time.Unix(0, event.Timestamp).UTC()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO ema (time, instrument, resolution, value)
		 VALUES ($1, $2, $3, $4)`,
		t,
		event.InstrumentID,
		event.Resolution,
		event.Value,
	)
	return err
}

// GetLast fetches the most recent n EMA values for a given resolution.
func (s *EMAStore) GetLast(ctx context.Context, instrumentID int, n int, resolution string) ([]redis.Z, error) {
	var k string
	if resolution == "1m" {
		k = emaKey1m(instrumentID)
	} else {
		k = emaKey1s(instrumentID)
	}
	return s.rdb.ZRangeWithScores(ctx, k, int64(-n), -1).Result()
}

// GetRange fetches EMA values between two unix timestamps for a given resolution.
func (s *EMAStore) GetRange(ctx context.Context, instrumentID int, fromUnix, toUnix int64, resolution string) ([]redis.Z, error) {
	var k string
	if resolution == "1m" {
		k = emaKey1m(instrumentID)
	} else {
		k = emaKey1s(instrumentID)
	}
	return s.rdb.ZRangeByScoreWithScores(ctx, k, &redis.ZRangeBy{
		Min: strconv.FormatInt(fromUnix, 10),
		Max: strconv.FormatInt(toUnix, 10),
	}).Result()
}
