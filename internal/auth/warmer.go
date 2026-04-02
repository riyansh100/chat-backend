// internal/auth/warmer.go
package auth

import (
	"context"
	"log"
	"time"

	"github.com/riyansh/chat-backend/internal/history"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

var indicators = []string{"sma", "ema", "rsi", "macd", "bb", "ohlc"}

// Warmer pre-loads Redis history + last price for a client's subscribed instruments.
type Warmer struct {
	histStore *history.Store
	lb        *chatredis.RedisLoadBalancer
}

func NewWarmer(histStore *history.Store, lb *chatredis.RedisLoadBalancer) *Warmer {
	return &Warmer{histStore: histStore, lb: lb}
}

// WarmAsync kicks off cache warming in a background goroutine (non-blocking).
func (w *Warmer) WarmAsync(clientID int, instrumentIDs []int) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, instrID := range instrumentIDs {
			w.warmInstrument(ctx, instrID)
		}
		log.Printf("[Warmer] client %d: warmed %d instruments", clientID, len(instrumentIDs))
	}()
}

// warmInstrument loads last 60 1m points per indicator from Postgres → writes to Redis.
func (w *Warmer) warmInstrument(ctx context.Context, instrID int) {
	now := time.Now().Unix()
	from := now - 60*60 // last 1 hour

	for _, ind := range indicators {
		// check if Redis already has data — skip if warm
		key := history.Key1m(ind, instrID)
		count, err := w.lb.WriteClient().ZCard(ctx, key).Result()
		if err == nil && count > 10 {
			continue // already warm enough
		}

		// load from Postgres
		points, err := w.histStore.FallbackFromPostgres(ctx, ind, instrID, from, now)
		if err != nil || len(points) == 0 {
			continue
		}

		// backfill Redis
		w.histStore.BackfillRedis(ind, instrID, "1m", points)
	}
}
