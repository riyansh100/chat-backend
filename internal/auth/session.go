// internal/auth/session.go
package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

const sessionTTL = 24 * time.Hour
const sessionPrefix = "session:"

// SessionStore manages session tokens in Redis.
type SessionStore struct {
	lb *chatredis.RedisLoadBalancer
}

func NewSessionStore(lb *chatredis.RedisLoadBalancer) *SessionStore {
	return &SessionStore{lb: lb}
}

// CreateSession generates a new token for the given clientID, stores it in Redis with 24h TTL.
// Returns the token string.
func (s *SessionStore) CreateSession(ctx context.Context, clientID int) (string, error) {
	token := uuid.NewString()
	key := sessionPrefix + token
	err := s.lb.WriteClient().Set(ctx, key, clientID, sessionTTL).Err()
	if err != nil {
		return "", fmt.Errorf("session create failed: %w", err)
	}
	return token, nil
}

// ValidateSession looks up a token and returns the associated clientID.
// Returns 0, error if the token is missing or expired.
func (s *SessionStore) ValidateSession(ctx context.Context, token string) (int, error) {
	key := sessionPrefix + token
	val, err := s.lb.WriteClient().Get(ctx, key).Int()
	if err != nil {
		return 0, fmt.Errorf("invalid or expired session")
	}
	return val, nil
}

// DeleteSession removes a token from Redis (logout).
func (s *SessionStore) DeleteSession(ctx context.Context, token string) error {
	key := sessionPrefix + token
	return s.lb.WriteClient().Del(ctx, key).Err()
}
