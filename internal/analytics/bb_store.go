// internal/analytics/bb_store.go
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

const maxBBEntries = 1440

type BBStore struct {
	lb   *chatredis.RedisLoadBalancer
	pool *pgxpool.Pool
}

func NewBBStore(lb *chatredis.RedisLoadBalancer, pool *pgxpool.Pool) *BBStore {
	return &BBStore{lb: lb, pool: pool}
}

func bbKey(instrumentID int) string { return fmt.Sprintf("bb:1m:%d", instrumentID) }

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
		"upper": event.Upper, "middle": event.Middle,
		"lower": event.Lower, "ts": event.Timestamp,
	})
	if err != nil {
		return err
	}
	rdb := s.lb.WriteClient()
	key := bbKey(event.InstrumentID)
	ts := float64(time.Now().Unix())
	if err := rdb.ZAdd(ctx, key, redis.Z{Score: ts, Member: string(payload)}).Err(); err != nil {
		return err
	}
	rdb.ZRemRangeByRank(ctx, key, 0, -maxBBEntries-1)
	return nil
}

func (s *BBStore) writePostgres(ctx context.Context, event BBUpdateEvent) error {
	t := time.Unix(0, event.Timestamp).UTC()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO bb (time, instrument, resolution, upper, middle, lower) VALUES ($1, $2, $3, $4, $5, $6)`,
		t, event.InstrumentID, event.Resolution, event.Upper, event.Middle, event.Lower,
	)
	return err
}

func (s *BBStore) GetLast(ctx context.Context, instrumentID int, n int) ([]redis.Z, error) {
	return s.lb.ZRangeWithScores(ctx, bbKey(instrumentID), int64(-n), -1).Result()
}

func (s *BBStore) GetRange(ctx context.Context, instrumentID int, fromUnix, toUnix int64) ([]redis.Z, error) {
	return s.lb.ZRangeByScoreWithScores(ctx, bbKey(instrumentID), &redis.ZRangeBy{
		Min: strconv.FormatInt(fromUnix, 10),
		Max: strconv.FormatInt(toUnix, 10),
	}).Result()
}
