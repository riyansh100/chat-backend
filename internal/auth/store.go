// internal/auth/store.go
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Client represents a logged-in user.
type Client struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

// Store handles all auth + subscription DB operations.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ErrUserExists is returned when a username is already taken.
var ErrUserExists = errors.New("username already taken")

// Register creates a new user with a bcrypt-hashed password.
func (s *Store) Register(ctx context.Context, username, password string) (*Client, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM clients WHERE username=$1)`,
		username,
	).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("register check failed: %w", err)
	}
	if exists {
		return nil, ErrUserExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, fmt.Errorf("bcrypt failed: %w", err)
	}

	var c Client
	err = s.pool.QueryRow(ctx,
		`INSERT INTO clients (username, password) VALUES ($1, $2) RETURNING id, username`,
		username, string(hash),
	).Scan(&c.ID, &c.Username)
	if err != nil {
		return nil, fmt.Errorf("register insert failed: %w", err)
	}
	return &c, nil
}

// Login validates username/password against the stored bcrypt hash.
func (s *Store) Login(ctx context.Context, username, password string) (*Client, error) {
	var c Client
	var hash string
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, password FROM clients WHERE username=$1`,
		username,
	).Scan(&c.ID, &c.Username, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("invalid credentials")
		}
		return nil, fmt.Errorf("login query failed: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	return &c, nil
}

// GetSubscriptions returns all instrument IDs the client is subscribed to.
func (s *Store) GetSubscriptions(ctx context.Context, clientID int) ([]int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT instrument_id FROM client_subscriptions WHERE client_id=$1 ORDER BY instrument_id ASC`,
		clientID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Subscribe adds an instrument subscription for a client. Idempotent.
func (s *Store) Subscribe(ctx context.Context, clientID, instrumentID int) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO client_subscriptions (client_id, instrument_id)
		 VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		clientID, instrumentID,
	)
	return err
}

// Unsubscribe removes an instrument subscription for a client.
func (s *Store) Unsubscribe(ctx context.Context, clientID, instrumentID int) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM client_subscriptions WHERE client_id=$1 AND instrument_id=$2`,
		clientID, instrumentID,
	)
	return err
}
