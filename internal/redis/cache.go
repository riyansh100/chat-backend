// internal/redis/cache.go
package redis

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/riyansh/chat-backend/internal/config"
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

// ---- config-driven constructors ----

// NewPair1PrimaryClient returns a sentinel-aware write client for pair1.
func NewPair1PrimaryClient(cfg *config.Config) *redis.Client {
	return redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    cfg.Pair1MasterName,
		SentinelAddrs: []string{cfg.Pair1SentinelAddr},
		PoolSize:      cfg.RedisPoolSize,
		DialTimeout:   cfg.RedisDialTimeout,
		ReadTimeout:   cfg.RedisReadTimeout,
	})
}

// NewPair1ReplicaClient returns a direct read client for pair1 replica.
func NewPair1ReplicaClient(cfg *config.Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:        cfg.Pair1ReplicaAddr,
		PoolSize:    cfg.RedisPoolSize,
		DialTimeout: cfg.RedisDialTimeout,
		ReadTimeout: cfg.RedisReadTimeout,
	})
}

// NewPair2PrimaryClient returns a sentinel-aware write client for pair2.
func NewPair2PrimaryClient(cfg *config.Config) *redis.Client {
	return redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    cfg.Pair2MasterName,
		SentinelAddrs: []string{cfg.Pair2SentinelAddr},
		PoolSize:      cfg.RedisPoolSize,
		DialTimeout:   cfg.RedisDialTimeout,
		ReadTimeout:   cfg.RedisReadTimeout,
	})
}

// NewPair2ReplicaClient returns a direct read client for pair2 replica.
func NewPair2ReplicaClient(cfg *config.Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:        cfg.Pair2ReplicaAddr,
		PoolSize:    cfg.RedisPoolSize,
		DialTimeout: cfg.RedisDialTimeout,
		ReadTimeout: cfg.RedisReadTimeout,
	})
}

// NewSentinelUniversalClient returns a UniversalClient for use with RedisCache.
func NewSentinelUniversalClient(cfg *config.Config) redis.UniversalClient {
	return redis.NewUniversalClient(&redis.UniversalOptions{
		MasterName: cfg.Pair1MasterName,
		Addrs:      []string{cfg.Pair1SentinelAddr},
		PoolSize:   cfg.RedisPoolSize,
	})
}

// ---- legacy helpers kept for compatibility ----

// NewSentinelClient kept for leader election (always needs pair1 primary).
// Deprecated: prefer NewPair1PrimaryClient(cfg).
func NewSentinelClient() *redis.Client {
	return redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    "mymaster",
		SentinelAddrs: []string{"127.0.0.1:26380"},
		PoolSize:      10,
		DialTimeout:   5 * time.Second,
		ReadTimeout:   3 * time.Second,
	})
}

// NewReplicaClient kept for compatibility.
// Deprecated: prefer NewPair1ReplicaClient(cfg).
func NewReplicaClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:6380",
		PoolSize:    10,
		DialTimeout: 5 * time.Second,
		ReadTimeout: 3 * time.Second,
	})
}

// SafeReadClient kept for compatibility — wraps single replica with primary fallback.
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
