package hub

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"
)

func StartRedisSubscriber(
	ctx context.Context,
	rdb *redis.Client,
	h *Hub,
) {
	sub := rdb.Subscribe(ctx, "chat:events")

	go func() {
		for msg := range sub.Channel() {
			var rm RedisMessage

			if err := json.Unmarshal([]byte(msg.Payload), &rm); err != nil {
				continue
			}

			// 🚨 critical: prevent echo
			if rm.Origin == h.InstanceID {
				continue
			}

			h.Broadcast <- BroadcastEvent{
				Room:   rm.Room,
				Origin: rm.Origin,
				Message: Message{
					Type: rm.Type,
					Data: rm.Data,
					Room: rm.Room,
				},
			}
		}
	}()
}
