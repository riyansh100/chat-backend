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
	Origin  string
}

type SubscribeEvent struct {
	Client *Client
	Topic  string // e.g. "sma:101", "ema:102", "ohlc:101"
}

type UnsubscribeEvent struct {
	Client *Client
	Topic  string
}
