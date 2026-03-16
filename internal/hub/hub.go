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

	smaEngine  *analytics.Engine
	smaStore   *analytics.SMAStore
	ohlcEngine *analytics.OHLCEngine
	ohlcStore  *analytics.OHLCStore
	emaEngine  *analytics.EMAEngine
	emaStore   *analytics.EMAStore
}

// SMAEngine returns the Hub's SMA engine for direct feeding by the background worker.
func (h *Hub) SMAEngine() *analytics.Engine {
	return h.smaEngine
}

// OHLCEngine returns the Hub's OHLC engine for direct feeding by the background worker.
func (h *Hub) OHLCEngine() *analytics.OHLCEngine {
	return h.ohlcEngine
}

// EMAEngine returns the Hub's EMA engine for direct feeding by the background worker.
func (h *Hub) EMAEngine() *analytics.EMAEngine {
	return h.emaEngine
}
