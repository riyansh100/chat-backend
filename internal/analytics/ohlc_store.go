// internal/analytics/ohlc_store.go
package analytics

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	bincod "github.com/riyansh/chat-backend/internal/binary"
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
	// 32-byte binary member replaces the old JSON object string
	member := bincod.EncodeOHLC(event.Open, event.High, event.Low, event.Close)
	rdb := s.lb.WriteClient()
	key := ohlcKey(event.InstrumentID)
	if err := rdb.ZAdd(ctx, key, redis.Z{
		Score:  float64(event.Timestamp),
		Member: member,
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
