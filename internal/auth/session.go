// internal/auth/session.go
package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const sessionTTL = 24 * time.Hour
const sessionPrefix = "session:"

type SessionStore struct {
	rdb *redis.Client // dedicated client — NOT the load balancer
}

func NewSessionStore(rdb *redis.Client) *SessionStore {
	return &SessionStore{rdb: rdb}
}

func (s *SessionStore) CreateSession(ctx context.Context, clientID int) (string, error) {
	token := uuid.NewString()
	err := s.rdb.Set(ctx, sessionPrefix+token, clientID, sessionTTL).Err()
	if err != nil {
		return "", fmt.Errorf("session create failed: %w", err)
	}
	return token, nil
}

func (s *SessionStore) ValidateSession(ctx context.Context, token string) (int, error) {
	val, err := s.rdb.Get(ctx, sessionPrefix+token).Int()
	if err != nil {
		return 0, fmt.Errorf("invalid or expired session")
	}
	return val, nil
}

func (s *SessionStore) DeleteSession(ctx context.Context, token string) error {
	return s.rdb.Del(ctx, sessionPrefix+token).Err()
}
