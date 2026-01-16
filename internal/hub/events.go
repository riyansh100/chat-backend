package hub

type JoinRoomEvent struct {
	Client *Client
	Room   string
}

type LeaveRoomEvent struct {
	Client *Client
	Room   string
}

type BroadcastEvent struct {
	Room    string
	Message Message
}
