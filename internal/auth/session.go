// internal/auth/session.go
package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

const sessionTTL = 24 * time.Hour
const sessionPrefix = "session:"

type SessionStore struct {
	rdb *redis.Client                // pinned write client (pair2Primary) — for Create/Delete
	lb  *chatredis.RedisLoadBalancer // scatter-gather reads for ValidateSession
}

// NewSessionStore creates a SessionStore.
// rdb  = pair2Primary — still used for writes (Create, Delete).
// lb   = RedisLoadBalancer — used for reads (ValidateSession) to distribute load.
func NewSessionStore(rdb *redis.Client, lb *chatredis.RedisLoadBalancer) *SessionStore {
	return &SessionStore{rdb: rdb, lb: lb}
}

func (s *SessionStore) CreateSession(ctx context.Context, clientID int) (string, error) {
	token := uuid.NewString()
	// Write always goes to the pinned primary — consistent with replication lag.
	err := s.rdb.Set(ctx, sessionPrefix+token, clientID, sessionTTL).Err()
	if err != nil {
		return "", fmt.Errorf("session create failed: %w", err)
	}
	return token, nil
}

// ValidateSession reads via the load balancer's scatter-gather replicas,
// falling back to the primary if replicas miss (replication lag edge case).
func (s *SessionStore) ValidateSession(ctx context.Context, token string) (int, error) {
	key := sessionPrefix + token

	// Fix 2: read via lb scatter-gather — distributes 200+ concurrent login
	// ValidateSession calls across replicas instead of hammering pair2Primary.
	cmd := s.lb.Get(ctx, key)
	if cmd.Err() != nil {
		// Replica miss (lag) — fall back to primary
		v, err2 := s.rdb.Get(ctx, key).Int()
		if err2 != nil {
			return 0, fmt.Errorf("invalid or expired session")
		}
		return v, nil
	}

	clientID, err := cmd.Int()
	if err != nil {
		return 0, fmt.Errorf("invalid session value")
	}
	return clientID, nil
}

func (s *SessionStore) DeleteSession(ctx context.Context, token string) error {
	return s.rdb.Del(ctx, sessionPrefix+token).Err()
}
