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
	client *redis.Client
	ttl    time.Duration
}

func NewRedisCache(client *redis.Client, ttl time.Duration) *RedisCache {
	return &RedisCache{
		client: client,
		ttl:    ttl,
	}
}

func (r *RedisCache) key(instrument string) string {
	return "last_price:" + instrument
}

func (r *RedisCache) SetLastPrice(
	ctx context.Context,
	instrument string,
	payload any,
) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return r.client.Set(ctx, r.key(instrument), b, r.ttl).Err()
}

func (r *RedisCache) GetLastPrice(
	ctx context.Context,
	instrument string,
) ([]byte, error) {
	return r.client.Get(ctx, r.key(instrument)).Bytes()
}
