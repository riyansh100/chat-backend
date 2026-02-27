package ws

import (
	"net/http"
"time
"
	"github.com/gorilla/websocket"

	"github.com/riyansh/chat-backend/internal/domain/trading"
	"github.com/riyansh/chat-backend/internal/hub"
)

var ingestUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func IngestHandler(h *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		conn, err := ingestUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		client := &hub.Client{
			ID:    r.RemoteAddr,
			Conn:  conn,
			Send:  make(chan hub.Message, 256),
			Rooms: make(map[string]bool),
			Hub:   h,
			Role:  string(trading.RoleIngestor),
		}

		h.Register <- client

		go client.WritePump()
		go client.ReadPump()
	}
}
