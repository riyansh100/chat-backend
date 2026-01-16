package hub

import "github.com/gorilla/websocket"

type Client struct {
	ID    string
	Conn  *websocket.Conn
	Hub   *Hub
	Send  chan Message
	Rooms map[string]bool
}
