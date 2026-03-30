// internal/redis/cache.go
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

// ---- pair 1 clients (existing ports) ----

// NewPair1PrimaryClient returns a sentinel-aware write client for pair1.
// Sentinel :26380 monitors primary :6381, replica :6380.
func NewPair1PrimaryClient() *redis.Client {
	return redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    "mymaster",
		SentinelAddrs: []string{"127.0.0.1:26380"},
		PoolSize:      10,
		DialTimeout:   5 * time.Second,
		ReadTimeout:   3 * time.Second,
	})
}

// NewPair1ReplicaClient returns a direct read client for pair1 replica.
func NewPair1ReplicaClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:6380",
		PoolSize:    10,
		DialTimeout: 5 * time.Second,
		ReadTimeout: 3 * time.Second,
	})
}

// ---- pair 2 clients (new ports) ----

// NewPair2PrimaryClient returns a sentinel-aware write client for pair2.
// Sentinel :26381 monitors primary :6383, replica :6382.
func NewPair2PrimaryClient() *redis.Client {
	return redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    "mymaster2",
		SentinelAddrs: []string{"127.0.0.1:26381"},
		PoolSize:      10,
		DialTimeout:   5 * time.Second,
		ReadTimeout:   3 * time.Second,
	})
}

// NewPair2ReplicaClient returns a direct read client for pair2 replica.
func NewPair2ReplicaClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:6382",
		PoolSize:    10,
		DialTimeout: 5 * time.Second,
		ReadTimeout: 3 * time.Second,
	})
}

// ---- legacy helpers (kept for compatibility) ----

// NewSentinelClient kept for leader election in dataserver (always needs pair1 primary).
func NewSentinelClient() *redis.Client {
	return NewPair1PrimaryClient()
}

// NewReplicaClient kept for compatibility.
func NewReplicaClient() *redis.Client {
	return NewPair1ReplicaClient()
}

// NewSafeReadClient kept for compatibility — wraps single replica with primary fallback.
// Prefer RedisLoadBalancer for new code.
type SafeReadClient struct {
	replica *redis.Client
	primary *redis.Client
}

func NewSafeReadClient(replica, primary *redis.Client) *SafeReadClient {
	return &SafeReadClient{replica: replica, primary: primary}
}

func (s *SafeReadClient) ZRangeWithScores(ctx context.Context, key string, start, stop int64) *redis.ZSliceCmd {
	cmd := s.replica.ZRangeWithScores(ctx, key, start, stop)
	if cmd.Err() != nil && cmd.Err() != redis.Nil {
		log.Printf("[SafeRead] replica ZRangeWithScores failed (%v), falling back to primary", cmd.Err())
		return s.primary.ZRangeWithScores(ctx, key, start, stop)
	}
	return cmd
}

func (s *SafeReadClient) ZRangeByScoreWithScores(ctx context.Context, key string, opt *redis.ZRangeBy) *redis.ZSliceCmd {
	cmd := s.replica.ZRangeByScoreWithScores(ctx, key, opt)
	if cmd.Err() != nil && cmd.Err() != redis.Nil {
		log.Printf("[SafeRead] replica ZRangeByScoreWithScores failed (%v), falling back to primary", cmd.Err())
		return s.primary.ZRangeByScoreWithScores(ctx, key, opt)
	}
	return cmd
}

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
