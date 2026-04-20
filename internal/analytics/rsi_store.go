// internal/analytics/rsi_store.go
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

const (
	maxRSIEntries1s = 3600
	maxRSIEntries1m = 1440
)

type RSIStore struct {
	lb   *chatredis.RedisLoadBalancer
	pool *pgxpool.Pool
}

func NewRSIStore(lb *chatredis.RedisLoadBalancer, pool *pgxpool.Pool) *RSIStore {
	return &RSIStore{lb: lb, pool: pool}
}

func rsiKey1s(instrumentID int) string { return fmt.Sprintf("rsi:1s:%d", instrumentID) }
func rsiKey1m(instrumentID int) string { return fmt.Sprintf("rsi:1m:%d", instrumentID) }

func (s *RSIStore) Write(ctx context.Context, event RSIUpdateEvent) error {
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

func (s *RSIStore) writeRedis(ctx context.Context, event RSIUpdateEvent) error {
	var k string
	var maxEntries int64
	switch event.Resolution {
	case "1m":
		k = rsiKey1m(event.InstrumentID)
		maxEntries = maxRSIEntries1m
	default:
		k = rsiKey1s(event.InstrumentID)
		maxEntries = maxRSIEntries1s
	}
	rdb := s.lb.WriteClient()
	ts := time.Now().Unix()
	member := bincod.EncodeScalar(event.Value)
	if err := rdb.ZAdd(ctx, k, redis.Z{
		Score:  float64(ts),
		Member: member,
	}).Err(); err != nil {
		return err
	}
	rdb.ZRemRangeByRank(ctx, k, 0, -maxEntries-1)
	return nil
}

func (s *RSIStore) writePostgres(ctx context.Context, event RSIUpdateEvent) error {
	t := time.Unix(0, event.Timestamp).UTC()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO rsi (time, instrument, resolution, value) VALUES ($1, $2, $3, $4)`,
		t, event.InstrumentID, event.Resolution, event.Value,
	)
	return err
}

func (s *RSIStore) GetLast(ctx context.Context, instrumentID int, n int, resolution string) ([]redis.Z, error) {
	var k string
	if resolution == "1m" {
		k = rsiKey1m(instrumentID)
	} else {
		k = rsiKey1s(instrumentID)
	}
	return s.lb.ZRangeWithScores(ctx, k, int64(-n), -1).Result()
}

func (s *RSIStore) GetRange(ctx context.Context, instrumentID int, fromUnix, toUnix int64, resolution string) ([]redis.Z, error) {
	var k string
	if resolution == "1m" {
		k = rsiKey1m(instrumentID)
	} else {
		k = rsiKey1s(instrumentID)
	}
	return s.lb.ZRangeByScoreWithScores(ctx, k, &redis.ZRangeBy{
		Min: strconv.FormatInt(fromUnix, 10),
		Max: strconv.FormatInt(toUnix, 10),
	}).Result()
}
