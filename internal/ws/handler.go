// internal/ws/handler.go
package ws

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/riyansh/chat-backend/internal/auth"
	"github.com/riyansh/chat-backend/internal/domain/trading"
	"github.com/riyansh/chat-backend/internal/hub"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ServeWS upgrades the HTTP connection to WebSocket.
// Requires ?token=<session-token> query parameter.
// Rejects with 401 if the token is missing or invalid.
func ServeWS(h *hub.Hub, ss *auth.SessionStore, w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	clientID, err := ss.ValidateSession(context.Background(), token)
	if err != nil {
		http.Error(w, "invalid or expired session", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &hub.Client{
		ID:            uuid.NewString(),
		ClientID:      clientID,
		Conn:          conn,
		Send:          make(chan hub.Message, 2048),
		IndicatorFeed: make(chan hub.Message, 256),
		Rooms:         make(map[string]bool),
		Hub:           h,
		Role:          string(trading.RoleConsumer),
	}

	h.Register <- client

	go client.WritePump()
	go client.ReadPump()
}
