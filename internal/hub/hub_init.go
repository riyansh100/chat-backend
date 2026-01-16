package hub

func NewHub(instanceID string) *Hub {
	return &Hub{
		InstanceID: instanceID,

		Rooms: make(map[string]*Room),

		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		JoinRoom:   make(chan JoinRoomEvent),
		LeaveRoom:  make(chan LeaveRoomEvent),
		Broadcast:  make(chan BroadcastEvent),
	}
}
