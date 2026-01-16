package hub

import "github.com/redis/go-redis/v9"

type Hub struct {
	Rooms       map[string]*Room
	RedisClient *redis.Client
	Register    chan *Client
	Unregister  chan *Client

	JoinRoom  chan JoinRoomEvent
	LeaveRoom chan LeaveRoomEvent

	Broadcast chan BroadcastEvent

	InstanceID string
}
