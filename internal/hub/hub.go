package hub

import (
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/riyansh/chat-backend/internal/analytics"
	"github.com/riyansh/chat-backend/internal/cache"
	"github.com/riyansh/chat-backend/internal/metrics"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

type Hub struct {
	Rooms       map[string]*Room
	RedisClient *goredis.Client
	pgPool      *pgxpool.Pool

	Register   chan *Client
	Unregister chan *Client

	JoinRoom  chan JoinRoomEvent
	LeaveRoom chan LeaveRoomEvent

	Broadcast chan BroadcastEvent

	InstanceID string

	redisCache chatredis.Cache

	l1      *cache.L1Cache
	Metrics *metrics.HubMetrics

	// indicator registry — single feed path for all engines
	registry *analytics.Registry

	// per-engine typed access (needed for output channels + history)
	smaEngine  *analytics.Engine
	smaStore   *analytics.SMAStore
	ohlcEngine *analytics.OHLCEngine
	ohlcStore  *analytics.OHLCStore
	emaEngine  *analytics.EMAEngine
	emaStore   *analytics.EMAStore
}

// Registry returns the indicator registry for the background worker.
func (h *Hub) Registry() *analytics.Registry {
	return h.registry
}

// SMAEngine returns the SMA engine (for output channel + metrics).
func (h *Hub) SMAEngine() *analytics.Engine {
	return h.smaEngine
}

// OHLCEngine returns the OHLC engine (for output channel + metrics).
func (h *Hub) OHLCEngine() *analytics.OHLCEngine {
	return h.ohlcEngine
}

// EMAEngine returns the EMA engine (for output channel + metrics).
func (h *Hub) EMAEngine() *analytics.EMAEngine {
	return h.emaEngine
}
