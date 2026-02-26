package cache

import "github.com/dgraph-io/ristretto"

type L1Cache struct {
	cache *ristretto.Cache
}

func NewL1Cache() (*L1Cache, error) {
	c, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     1 << 26, // ~64MB
		BufferItems: 64,
	})
	if err != nil {
		return nil, err
	}

	return &L1Cache{cache: c}, nil
}

func (l *L1Cache) Set(key string, value interface{}) {
	l.cache.Set(key, value, 1)
}

func (l *L1Cache) Get(key string) (interface{}, bool) {
	return l.cache.Get(key)
}
