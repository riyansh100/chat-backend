package redis

import (
	"context"
	"encoding/json"
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

// NewRedisCache creates a direct single-node client (kept for compatibility).
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

// NewSentinelClient creates a Sentinel-aware Redis client.
// All reads and writes go to the current primary — sentinel handles
// promoting the replica and redirecting the client transparently.
func NewSentinelClient() *redis.Client {
	return redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    "mymaster",
		SentinelAddrs: []string{"127.0.0.1:26380"},

		// connection pool sizing
		PoolSize:    10,
		DialTimeout: 5 * time.Second,
		ReadTimeout: 3 * time.Second,
	})
}

// NewSentinelUniversalClient returns a UniversalClient (satisfies redis.UniversalClient)
// for use with RedisCache and anywhere else that needs the interface.
func NewSentinelUniversalClient() redis.UniversalClient {
	return redis.NewUniversalClient(&redis.UniversalOptions{
		MasterName: "mymaster",
		Addrs:      []string{"127.0.0.1:26380"},
		PoolSize:   10,
	})
}
