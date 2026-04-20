// internal/nats/client.go
package nats

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamName    = "ANALYTICS"
	StreamSubject = "analytics.events"
	ConsumerName  = "clientserver"
)

// Connect dials NATS (defaults to nats://localhost:4222 if url is empty).
func Connect(url string) (*nats.Conn, error) {
	if url == "" {
		url = nats.DefaultURL
	}
	nc, err := nats.Connect(url,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Printf("[NATS] disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			log.Println("[NATS] reconnected")
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	log.Printf("[NATS] connected to %s", url)
	return nc, nil
}

// EnsureStream creates the ANALYTICS JetStream stream if it doesn't already
// exist. Safe to call on every startup — it is a no-op when the stream is
// already configured correctly.
func EnsureStream(ctx context.Context, nc *nats.Conn) (jetstream.JetStream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream init: %w", err)
	}

	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       StreamName,
		Subjects:   []string{StreamSubject},
		Retention:  jetstream.LimitsPolicy,
		MaxAge:     5 * time.Minute,
		MaxMsgSize: 64 * 1024, // 64 KB — well above our largest frame (46 B)
		Storage:    jetstream.MemoryStorage,
		Replicas:   1,
	})
	if err != nil {
		return nil, fmt.Errorf("create/update stream %s: %w", StreamName, err)
	}
	log.Printf("[NATS] stream %q ready (subject=%s)", StreamName, StreamSubject)
	return js, nil
}

// Publisher wraps a JetStream context for fire-and-forget analytics publishes.
type Publisher struct {
	js jetstream.JetStream
}

// NewPublisher returns a Publisher backed by the given JetStream context.
func NewPublisher(js jetstream.JetStream) *Publisher {
	return &Publisher{js: js}
}

// Publish sends a pre-encoded binary frame to analytics.events.
// The caller is responsible for encoding via internal/binary — this function
// does NOT marshal anything; it writes the raw bytes directly.
// Errors are logged but not returned — analytics publishing is best-effort.
func (p *Publisher) Publish(ctx context.Context, frame []byte) {
	if _, err := p.js.Publish(ctx, StreamSubject, frame); err != nil {
		log.Printf("[NATS] publish error: %v", err)
	}
}
