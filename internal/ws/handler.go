// 📌 This file does only one thing:
// HTTP → WebSocket → Client creation
package ws

import (
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/riyansh/chat-backend/internal/hub"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func ServeWS(h *hub.Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &hub.Client{
		ID:    conn.RemoteAddr().String(),
		Conn:  conn,
		Hub:   h,
		Send:  make(chan hub.Message, 256),
		Rooms: make(map[string]bool),
	}

	h.Register <- client

	go client.WritePump()
	go client.ReadPump()
}
