// internal/analytics/ohlc_store.go
package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

const maxOHLCEntries = 1440

type OHLCStore struct {
	lb   *chatredis.RedisLoadBalancer
	pool *pgxpool.Pool
}

func NewOHLCStore(lb *chatredis.RedisLoadBalancer, pool *pgxpool.Pool) *OHLCStore {
	return &OHLCStore{lb: lb, pool: pool}
}

func ohlcKey(instrumentID int) string { return fmt.Sprintf("ohlc:1m:%d", instrumentID) }

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
		"open": event.Open, "high": event.High,
		"low": event.Low, "close": event.Close, "ts": event.Timestamp,
	})
	if err != nil {
		return err
	}
	rdb := s.lb.WriteClient()
	key := ohlcKey(event.InstrumentID)
	if err := rdb.ZAdd(ctx, key, redis.Z{
		Score:  float64(event.Timestamp),
		Member: string(payload),
	}).Err(); err != nil {
		return err
	}
	rdb.ZRemRangeByRank(ctx, key, 0, -maxOHLCEntries-1)
	return nil
}

func (s *OHLCStore) writePostgres(ctx context.Context, event OHLCEvent) error {
	t := time.Unix(event.Timestamp, 0).UTC()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO ohlc (time, instrument, resolution, open, high, low, close) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t, event.InstrumentID, event.Resolution, event.Open, event.High, event.Low, event.Close,
	)
	return err
}

func (s *OHLCStore) GetLast(ctx context.Context, instrumentID int, n int) ([]redis.Z, error) {
	return s.lb.ZRangeWithScores(ctx, ohlcKey(instrumentID), int64(-n), -1).Result()
}

func (s *OHLCStore) GetRange(ctx context.Context, instrumentID int, fromUnix, toUnix int64) ([]redis.Z, error) {
	return s.lb.ZRangeByScoreWithScores(ctx, ohlcKey(instrumentID), &redis.ZRangeBy{
		Min: strconv.FormatInt(fromUnix, 10),
		Max: strconv.FormatInt(toUnix, 10),
	}).Result()
}
