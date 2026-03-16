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
	maxEntries1s = 3600 // 60 minutes of 1s data
	maxEntries1m = 1440 // 24 hours of 1m data
)

type SMAStore struct {
	rdb  *redis.Client
	pool *pgxpool.Pool // optional; nil = no Postgres persistence
}

// NewSMAStore creates an SMAStore. pool may be nil (Redis-only mode).
func NewSMAStore(rdb *redis.Client, pool *pgxpool.Pool) *SMAStore {
	return &SMAStore{rdb: rdb, pool: pool}
}

func key1s(instrumentID int) string {
	return fmt.Sprintf("sma:1s:%d", instrumentID)
}

func key1m(instrumentID int) string {
	return fmt.Sprintf("sma:1m:%d", instrumentID)
}

// Write stores a bucketed SMA value in Redis and (if pool is set) Postgres.
func (s *SMAStore) Write(ctx context.Context, event SMAUpdateEvent) error {
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

func (s *SMAStore) writeRedis(ctx context.Context, event SMAUpdateEvent) error {
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

func (s *SMAStore) writePostgres(ctx context.Context, event SMAUpdateEvent) error {
	t := time.Unix(0, event.Timestamp).UTC()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sma (time, instrument, resolution, value)
		 VALUES ($1, $2, $3, $4)`,
		t,
		event.InstrumentID,
		event.Resolution,
		event.Value,
	)
	return err
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
