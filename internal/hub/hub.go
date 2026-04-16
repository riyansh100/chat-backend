// internal/hub/hub.go
package hub

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
	"github.com/riyansh/chat-backend/internal/analytics"
	"github.com/riyansh/chat-backend/internal/cache"
	"github.com/riyansh/chat-backend/internal/metrics"
	internalnats "github.com/riyansh/chat-backend/internal/nats"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

type Hub struct {
	Rooms       map[string]*Room
	RedisClient *goredis.Client              // write client — least-loaded primary (from lb)
	NatsConn    *nats.Conn                   // NATS connection for analytics pub/sub
	natsPub     *internalnats.Publisher      // JetStream publisher (dataserver only)
	lb          *chatredis.RedisLoadBalancer // load balancer — owns all 4 Redis clients
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
	rsiEngine  *analytics.RSIEngine
	rsiStore   *analytics.RSIStore
	macdEngine *analytics.MACDEngine
	macdStore  *analytics.MACDStore
}

func (h *Hub) Registry() *analytics.Registry     { return h.registry }
func (h *Hub) SMAEngine() *analytics.Engine      { return h.smaEngine }
func (h *Hub) OHLCEngine() *analytics.OHLCEngine { return h.ohlcEngine }
func (h *Hub) EMAEngine() *analytics.EMAEngine   { return h.emaEngine }
func (h *Hub) BBEngine() *analytics.BBEngine     { return h.bbEngine }
func (h *Hub) RSIEngine() *analytics.RSIEngine   { return h.rsiEngine }
func (h *Hub) MACDEngine() *analytics.MACDEngine { return h.macdEngine }
func (h *Hub) SubManager() *SubscriptionManager  { return h.subManager }
