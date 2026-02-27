package hub

import (
	goredis "github.com/redis/go-redis/v9"
	"github.com/riyansh/chat-backend/internal/cache"
	"github.com/riyansh/chat-backend/internal/metrics"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

type Hub struct {
	Rooms       map[string]*Room
	RedisClient *goredis.Client

	Register   chan *Client
	Unregister chan *Client

	JoinRoom  chan JoinRoomEvent
	LeaveRoom chan LeaveRoomEvent

	Broadcast chan BroadcastEvent

	InstanceID string

	redisCache chatredis.Cache

	l1      *cache.L1Cache
	Metrics *metrics.HubMetrics
}
