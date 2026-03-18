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

const maxBBEntries = 1440 // 24 hours of 1m candles

type BBStore struct {
	rdb  *redis.Client
	pool *pgxpool.Pool
}

func NewBBStore(rdb *redis.Client, pool *pgxpool.Pool) *BBStore {
	return &BBStore{rdb: rdb, pool: pool}
}

func bbKey(instrumentID int) string {
	return fmt.Sprintf("bb:1m:%d", instrumentID)
}

// Write stores a BB event in Redis (warm-start) and Postgres (data lake).
func (s *BBStore) Write(ctx context.Context, event BBUpdateEvent) error {
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

func (s *BBStore) writeRedis(ctx context.Context, event BBUpdateEvent) error {
	payload, err := json.Marshal(map[string]interface{}{
		"upper":  event.Upper,
		"middle": event.Middle,
		"lower":  event.Lower,
		"ts":     event.Timestamp,
	})
	if err != nil {
		return err
	}

	key := bbKey(event.InstrumentID)
	ts := float64(time.Now().Unix())

	err = s.rdb.ZAdd(ctx, key, redis.Z{
		Score:  ts,
		Member: string(payload),
	}).Err()
	if err != nil {
		return err
	}

	s.rdb.ZRemRangeByRank(ctx, key, 0, -maxBBEntries-1)
	return nil
}

func (s *BBStore) writePostgres(ctx context.Context, event BBUpdateEvent) error {
	t := time.Unix(0, event.Timestamp).UTC()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO bb (time, instrument, resolution, upper, middle, lower)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		t,
		event.InstrumentID,
		event.Resolution,
		event.Upper,
		event.Middle,
		event.Lower,
	)
	return err
}

// GetLast fetches the most recent n BB values from Redis.
func (s *BBStore) GetLast(ctx context.Context, instrumentID int, n int) ([]redis.Z, error) {
	return s.rdb.ZRangeWithScores(ctx, bbKey(instrumentID), int64(-n), -1).Result()
}

// GetRange fetches BB values between two unix timestamps from Redis.
func (s *BBStore) GetRange(ctx context.Context, instrumentID int, fromUnix, toUnix int64) ([]redis.Z, error) {
	return s.rdb.ZRangeByScoreWithScores(ctx, bbKey(instrumentID), &redis.ZRangeBy{
		Min: strconv.FormatInt(fromUnix, 10),
		Max: strconv.FormatInt(toUnix, 10),
	}).Result()
}
