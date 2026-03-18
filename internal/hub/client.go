package hub

import "github.com/gorilla/websocket"

type Client struct {
	ID   string
	Conn *websocket.Conn

	// Push path — hub broadcasts everything the client's rooms receive
	Send chan Message

	// Pull path — only topics the client explicitly subscribed to
	IndicatorFeed chan Message

	Rooms map[string]bool
	Hub   *Hub

	Dropped int
	Role    string
}
