package redis

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache interface {
	SetLastPrice(ctx context.Context, instrument string, payload any) error
	GetLastPrice(ctx context.Context, instrument string) ([]byte, error)
}

type RedisCache struct {
	client redis.UniversalClient
	ttl    time.Duration
}

func NewRedisCache(client redis.UniversalClient, ttl time.Duration) *RedisCache {
	return &RedisCache{client: client, ttl: ttl}
}

func (r *RedisCache) key(instrument string) string {
	return "last_price:" + instrument
}

func (r *RedisCache) SetLastPrice(ctx context.Context, instrument string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.key(instrument), b, r.ttl).Err()
}

func (r *RedisCache) GetLastPrice(ctx context.Context, instrument string) ([]byte, error) {
	return r.client.Get(ctx, r.key(instrument)).Bytes()
}

// NewSentinelClient creates a Sentinel-aware Redis client that always points
// to the current primary. Use this for all WRITES.
func NewSentinelClient() *redis.Client {
	return redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    "mymaster",
		SentinelAddrs: []string{"127.0.0.1:26380"},
		PoolSize:      10,
		DialTimeout:   5 * time.Second,
		ReadTimeout:   3 * time.Second,
	})
}

// NewReplicaClient creates a direct client to the replica for READ operations.
// Reads bypass the primary, reducing its load and giving ~1-3ms lower latency.
func NewReplicaClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:6380",
		PoolSize:    10,
		DialTimeout: 5 * time.Second,
		ReadTimeout: 3 * time.Second,
	})
}

// SafeReadClient wraps a replica client with automatic fallback to the primary
// if the replica is unavailable (e.g. during sentinel failover promotion).
// All reads go to replica first; on any error, the same read is retried on primary.
type SafeReadClient struct {
	replica *redis.Client
	primary *redis.Client
}

func NewSafeReadClient(replica, primary *redis.Client) *SafeReadClient {
	return &SafeReadClient{replica: replica, primary: primary}
}

// ZRangeWithScores reads from replica, falls back to primary on error.
func (s *SafeReadClient) ZRangeWithScores(ctx context.Context, key string, start, stop int64) *redis.ZSliceCmd {
	cmd := s.replica.ZRangeWithScores(ctx, key, start, stop)
	if cmd.Err() != nil && cmd.Err() != redis.Nil {
		log.Printf("[SafeRead] replica ZRangeWithScores failed (%v), falling back to primary", cmd.Err())
		return s.primary.ZRangeWithScores(ctx, key, start, stop)
	}
	return cmd
}

// ZRangeByScoreWithScores reads from replica, falls back to primary on error.
func (s *SafeReadClient) ZRangeByScoreWithScores(ctx context.Context, key string, opt *redis.ZRangeBy) *redis.ZSliceCmd {
	cmd := s.replica.ZRangeByScoreWithScores(ctx, key, opt)
	if cmd.Err() != nil && cmd.Err() != redis.Nil {
		log.Printf("[SafeRead] replica ZRangeByScoreWithScores failed (%v), falling back to primary", cmd.Err())
		return s.primary.ZRangeByScoreWithScores(ctx, key, opt)
	}
	return cmd
}

// Get reads from replica, falls back to primary on error.
func (s *SafeReadClient) Get(ctx context.Context, key string) *redis.StringCmd {
	cmd := s.replica.Get(ctx, key)
	if cmd.Err() != nil && cmd.Err() != redis.Nil {
		log.Printf("[SafeRead] replica Get failed (%v), falling back to primary", cmd.Err())
		return s.primary.Get(ctx, key)
	}
	return cmd
}

// NewSentinelUniversalClient returns a UniversalClient for use with RedisCache.
func NewSentinelUniversalClient() redis.UniversalClient {
	return redis.NewUniversalClient(&redis.UniversalOptions{
		MasterName: "mymaster",
		Addrs:      []string{"127.0.0.1:26380"},
		PoolSize:   10,
	})
}
