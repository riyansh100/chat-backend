package hub

import "github.com/gorilla/websocket"

type Client struct {
	ID    string // NEW: unique client identifier
	Conn  *websocket.Conn
	Send  chan Message
	Rooms map[string]bool
	Hub   *Hub

	Role string // NEW: domain-agnostic role (default CONSUMER)
}
