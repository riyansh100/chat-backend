package hub

import "github.com/riyansh/chat-backend/internal/redis"

func NewHub(instanceID string, redisCache redis.Cache) *Hub {
	return &Hub{
		InstanceID: instanceID,

		Rooms:      make(map[string]*Room),
		redisCache: redisCache,
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		JoinRoom:   make(chan JoinRoomEvent),
		LeaveRoom:  make(chan LeaveRoomEvent),
		Broadcast:  make(chan BroadcastEvent),
	}
}
