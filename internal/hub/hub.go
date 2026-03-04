package hub

import (
	"encoding/json"
	"strconv"

	goredis "github.com/redis/go-redis/v9"
	"github.com/riyansh/chat-backend/internal/analytics"
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

	smaEngine *analytics.Engine
}
func (h *Hub) broadcastSMA(sma analytics.SMAUpdateEvent) {

	roomName := strconv.Itoa(sma.InstrumentID)

	room, ok := h.Rooms[roomName]
	if !ok {
		return
	}

	payload := map[string]interface{}{
		"type":          "sma_update",
		"instrument_id": sma.InstrumentID,
		"value":         sma.Value,
		"timestamp":     sma.Timestamp,
	}

	data, _ := json.Marshal(payload)

	msg := Message{
		Type: "sma_update",
		Data: data,
	}

	for client := range room.Clients {
		select {
		case client.Send <- msg:
			client.Dropped = 0
		default:
			client.Dropped++
			if client.Dropped > maxDroppedMessages {
				h.Unregister <- client
			}
		}
	}
}