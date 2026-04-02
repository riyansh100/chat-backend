// internal/auth/store.go
package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
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

// Login validates username/password. Returns Client on success, error on failure.
func (s *Store) Login(ctx context.Context, username, password string) (*Client, error) {
	var c Client
	err := s.pool.QueryRow(ctx,
		`SELECT id, username FROM clients WHERE username=$1 AND password=$2`,
		username, password,
	).Scan(&c.ID, &c.Username)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	return &c, nil
}

// GetSubscriptions returns all instrument IDs the client is subscribed to.
func (s *Store) GetSubscriptions(ctx context.Context, clientID int) ([]int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT instrument_id FROM client_subscriptions WHERE client_id=$1 ORDER BY subscribed_at ASC`,
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
