package hub

import (
	"context"
	"time"

	"github.com/riyansh/chat-backend/internal/domain/chat"
	"github.com/riyansh/chat-backend/internal/domain/common"

	"github.com/gorilla/websocket"
	"github.com/riyansh/chat-backend/internal/domain/trading"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 50 * time.Second
)

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				return
			}
			if err := c.Conn.WriteJSON(msg); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		var raw map[string]interface{}

		// Read raw JSON (transport responsibility only)
		if err := c.Conn.ReadJSON(&raw); err != nil {
			break
		}

		// Extract type
		msgType, ok := raw["type"].(string)
		if !ok {
			continue
		}
		delete(raw, "type")

		// Build domain envelope
		env := common.Envelope{
			Type: msgType,
			Body: raw,
		}

		switch c.Role {

		case string(trading.RoleConsumer):

			chatEvents, err := chat.ValidateAndTranslate(env, c.Rooms)
			if err != nil {
				continue
			}

			for _, e := range chatEvents {
				switch ev := e.(type) {

				case chat.JoinEvent:
					c.Hub.JoinRoom <- JoinRoomEvent{
						Client: c,
						Room:   ev.Room,
					}

				case chat.LeaveEvent:
					c.Hub.LeaveRoom <- LeaveRoomEvent{
						Client: c,
						Room:   ev.Room,
					}

				case chat.MessageEvent:
					c.Hub.Broadcast <- BroadcastEvent{
						Room:   ev.Room,
						Origin: c.Hub.InstanceID,
						Message: Message{
							Room: ev.Room,
							Data: ev.Data,
						},
					}
				}
			}

		case string(trading.RoleIngestor):

			tradingEvents, err := trading.ValidateAndTranslate(env, trading.RoleIngestor)
			if err != nil {
				continue
			}

			for _, e := range tradingEvents {
				switch ev := e.(type) {

				case trading.PriceUpdateEvent:
					// c.Hub.Broadcast <- BroadcastEvent{
					// 	Room:   ev.Instrument,
					// 	Origin: c.Hub.InstanceID,
					// 	Message: Message{
					// 		Room: ev.Instrument,
					// 		Data: map[string]interface{}{
					// 			"type":       "price_update",
					// 			"price":      ev.Price,
					// 			"ts":         ev.Timestamp,
					// 			"instrument": ev.Instrument,
					// 		},
					// 	},
					// }

					if c.Hub.redisCache != nil {
						_ = c.Hub.redisCache.SetLastPrice(
							context.Background(),
							ev.Instrument,
							map[string]interface{}{
								"type":       "price_update",
								"price":      ev.Price,
								"ts":         ev.Timestamp,
								"instrument": ev.Instrument,
							},
						)
					}

					c.Hub.Broadcast <- BroadcastEvent{
						Room:   ev.Instrument,
						Origin: c.Hub.InstanceID,
						Message: Message{
							Room: ev.Instrument,
							Data: map[string]interface{}{
								"type":       "price_update",
								"price":      ev.Price,
								"ts":         ev.Timestamp,
								"instrument": ev.Instrument,
							},
						},
					}

				}
			}
		}
	}
}
