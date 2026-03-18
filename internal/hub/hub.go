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

	JoinRoom    chan JoinRoomEvent
	LeaveRoom   chan LeaveRoomEvent
	Broadcast   chan BroadcastEvent
	Subscribe   chan SubscribeEvent
	Unsubscribe chan UnsubscribeEvent

	InstanceID string
	redisCache chatredis.Cache
	l1         *cache.L1Cache
	Metrics    *metrics.HubMetrics

	subManager *SubscriptionManager
	registry   *analytics.Registry

	smaEngine  *analytics.Engine
	smaStore   *analytics.SMAStore
	ohlcEngine *analytics.OHLCEngine
	ohlcStore  *analytics.OHLCStore
	emaEngine  *analytics.EMAEngine
	emaStore   *analytics.EMAStore
	bbEngine   *analytics.BBEngine
	bbStore    *analytics.BBStore
}

func (h *Hub) Registry() *analytics.Registry     { return h.registry }
func (h *Hub) SMAEngine() *analytics.Engine      { return h.smaEngine }
func (h *Hub) OHLCEngine() *analytics.OHLCEngine { return h.ohlcEngine }
func (h *Hub) EMAEngine() *analytics.EMAEngine   { return h.emaEngine }
func (h *Hub) BBEngine() *analytics.BBEngine     { return h.bbEngine }
func (h *Hub) SubManager() *SubscriptionManager  { return h.subManager }
