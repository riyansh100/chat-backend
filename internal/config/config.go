// internal/config/config.go
//
// Single source of truth for all runtime configuration.
// Every value can be overridden by an environment variable.
// Sane defaults let the binary run locally with zero env setup.
//
// Usage:
//
//	cfg := config.Load()
//	fmt.Println(cfg.DataPort)          // "8081"
//	fmt.Println(cfg.Pair1PrimaryAddr)  // "127.0.0.1:6381"
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime configuration for both binaries.
// Fields are exported so callers can read them directly.
type Config struct {
	// ---- ports ----
	DataPort   string // DATA_PORT   (dataserver HTTP)
	ClientPort string // CLIENT_PORT (clientserver HTTP)

	// ---- postgres ----
	PostgresDSN string // POSTGRES_DSN

	// ---- NATS ----
	NATSURL string // NATS_URL

	// ---- feed source ----
	FeedSource string // FEED_SOURCE: "binance" | "mock"

	// ---- Redis pair 1 ----
	Pair1PrimaryAddr  string        // REDIS_PAIR1_PRIMARY   (direct addr, used by replica and LB)
	Pair1ReplicaAddr  string        // REDIS_PAIR1_REPLICA
	Pair1SentinelAddr string        // REDIS_PAIR1_SENTINEL
	Pair1MasterName   string        // REDIS_PAIR1_MASTER
	RedisPoolSize     int           // REDIS_POOL_SIZE       (applied to all nodes)
	RedisDialTimeout  time.Duration // REDIS_DIAL_TIMEOUT
	RedisReadTimeout  time.Duration // REDIS_READ_TIMEOUT

	// ---- Redis pair 2 ----
	Pair2PrimaryAddr  string // REDIS_PAIR2_PRIMARY
	Pair2ReplicaAddr  string // REDIS_PAIR2_REPLICA
	Pair2SentinelAddr string // REDIS_PAIR2_SENTINEL
	Pair2MasterName   string // REDIS_PAIR2_MASTER

	// ---- session ----
	SessionTTLHours int // SESSION_TTL_HOURS  (default 24)

	// ---- postgres pool ----
	PGMaxConns int32 // PG_MAX_CONNS
	PGMinConns int32 // PG_MIN_CONNS
}

// Load reads environment variables and fills a Config with sane defaults.
// Call once at startup; pass the returned value everywhere.
func Load() *Config {
	return &Config{
		// ports
		DataPort:   getEnv("DATA_PORT", "8081"),
		ClientPort: getEnv("CLIENT_PORT", "8080"),

		// postgres
		PostgresDSN: getEnv("POSTGRES_DSN",
			"postgres://postgres:pwd@localhost:5432/marketdata?sslmode=disable"),

		// NATS
		NATSURL: getEnv("NATS_URL", "nats://127.0.0.1:4222"),

		// feed
		FeedSource: getEnv("FEED_SOURCE", "binance"),

		// redis pair 1
		Pair1PrimaryAddr:  getEnv("REDIS_PAIR1_PRIMARY", "127.0.0.1:6381"),
		Pair1ReplicaAddr:  getEnv("REDIS_PAIR1_REPLICA", "127.0.0.1:6380"),
		Pair1SentinelAddr: getEnv("REDIS_PAIR1_SENTINEL", "127.0.0.1:26380"),
		Pair1MasterName:   getEnv("REDIS_PAIR1_MASTER", "mymaster"),

		// redis pair 2
		Pair2PrimaryAddr:  getEnv("REDIS_PAIR2_PRIMARY", "127.0.0.1:6383"),
		Pair2ReplicaAddr:  getEnv("REDIS_PAIR2_REPLICA", "127.0.0.1:6382"),
		Pair2SentinelAddr: getEnv("REDIS_PAIR2_SENTINEL", "127.0.0.1:26381"),
		Pair2MasterName:   getEnv("REDIS_PAIR2_MASTER", "mymaster2"),

		// redis pool
		RedisPoolSize:    getEnvInt("REDIS_POOL_SIZE", 10),
		RedisDialTimeout: getEnvDuration("REDIS_DIAL_TIMEOUT", 5*time.Second),
		RedisReadTimeout: getEnvDuration("REDIS_READ_TIMEOUT", 3*time.Second),

		// session
		SessionTTLHours: getEnvInt("SESSION_TTL_HOURS", 24),

		// pg pool
		PGMaxConns: int32(getEnvInt("PG_MAX_CONNS", 50)),
		PGMinConns: int32(getEnvInt("PG_MIN_CONNS", 4)),
	}
}

// Print logs every config key/value at startup so you can verify
// what the binary actually loaded. Prints to stdout (not log) so
// it appears before the logger is initialised.
func (c *Config) Print() {
	fmt.Println("=== TradeFlow Config ===")
	fmt.Printf("  DATA_PORT            = %s\n", c.DataPort)
	fmt.Printf("  CLIENT_PORT          = %s\n", c.ClientPort)
	fmt.Printf("  POSTGRES_DSN         = %s\n", c.PostgresDSN)
	fmt.Printf("  NATS_URL             = %s\n", c.NATSURL)
	fmt.Printf("  FEED_SOURCE          = %s\n", c.FeedSource)
	fmt.Printf("  REDIS_PAIR1_PRIMARY  = %s\n", c.Pair1PrimaryAddr)
	fmt.Printf("  REDIS_PAIR1_REPLICA  = %s\n", c.Pair1ReplicaAddr)
	fmt.Printf("  REDIS_PAIR1_SENTINEL = %s\n", c.Pair1SentinelAddr)
	fmt.Printf("  REDIS_PAIR1_MASTER   = %s\n", c.Pair1MasterName)
	fmt.Printf("  REDIS_PAIR2_PRIMARY  = %s\n", c.Pair2PrimaryAddr)
	fmt.Printf("  REDIS_PAIR2_REPLICA  = %s\n", c.Pair2ReplicaAddr)
	fmt.Printf("  REDIS_PAIR2_SENTINEL = %s\n", c.Pair2SentinelAddr)
	fmt.Printf("  REDIS_PAIR2_MASTER   = %s\n", c.Pair2MasterName)
	fmt.Printf("  REDIS_POOL_SIZE      = %d\n", c.RedisPoolSize)
	fmt.Printf("  SESSION_TTL_HOURS    = %d\n", c.SessionTTLHours)
	fmt.Printf("  PG_MAX_CONNS         = %d\n", c.PGMaxConns)
	fmt.Printf("  PG_MIN_CONNS         = %d\n", c.PGMinConns)
	fmt.Println("========================")
}

// ---- helpers ----

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
