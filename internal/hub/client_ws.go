package hub

import (
	"context"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/riyansh/chat-backend/internal/domain/chat"
	"github.com/riyansh/chat-backend/internal/domain/common"
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
		// push path — room broadcasts
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				return
			}
			if err := c.Conn.WriteJSON(msg); err != nil {
				return
			}

		// pull path — explicit indicator subscriptions
		case msg, ok := <-c.IndicatorFeed:
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
		if err := c.Conn.ReadJSON(&raw); err != nil {
			break
		}

		msgType, ok := raw["type"].(string)
		if !ok {
			continue
		}
		delete(raw, "type")

		// ---- pull model: subscribe / unsubscribe ----
		if msgType == "subscribe" {
			topic, ok := raw["topic"].(string)
			if !ok || topic == "" {
				continue
			}
			c.Hub.Subscribe <- SubscribeEvent{Client: c, Topic: topic}
			continue
		}

		if msgType == "unsubscribe" {
			topic, ok := raw["topic"].(string)
			if !ok || topic == "" {
				continue
			}
			c.Hub.Unsubscribe <- UnsubscribeEvent{Client: c, Topic: topic}
			continue
		}

		// ---- existing push model: join / leave / message ----
		env := common.Envelope{Type: msgType, Body: raw}

		switch c.Role {
		case string(trading.RoleConsumer):
			chatEvents, err := chat.ValidateAndTranslate(env, c.Rooms)
			if err != nil {
				continue
			}
			for _, e := range chatEvents {
				switch ev := e.(type) {
				case chat.JoinEvent:
					c.Hub.JoinRoom <- JoinRoomEvent{Client: c, Room: ev.Room}
				case chat.LeaveEvent:
					c.Hub.LeaveRoom <- LeaveRoomEvent{Client: c, Room: ev.Room}
				case chat.MessageEvent:
					c.Hub.Broadcast <- BroadcastEvent{
						Room:    ev.Room,
						Origin:  c.Hub.InstanceID,
						Message: Message{Room: ev.Room, Data: ev.Data},
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
					ev.IngestedAt = time.Now().UnixNano()
					c.Hub.Metrics.EventsIngested.Add(1)

					if c.Hub.redisCache != nil {
						ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
						_ = c.Hub.redisCache.SetLastPrice(
							ctx,
							strconv.Itoa(ev.InstrumentID),
							map[string]interface{}{
								"type":       "price_update",
								"price":      ev.Price,
								"ts":         ev.Timestamp,
								"instrument": ev.Instrument,
							},
						)
						cancel()
					}

					roomID := strconv.Itoa(ev.InstrumentID)
					c.Hub.Broadcast <- BroadcastEvent{
						Room:   roomID,
						Origin: c.Hub.InstanceID,
						Message: Message{
							Room: roomID,
							Data: map[string]interface{}{
								"type":        "price_update",
								"price":       ev.Price,
								"ts":          ev.Timestamp,
								"instrument":  ev.Instrument,
								"ingested_at": ev.IngestedAt,
							},
						},
					}
				}
			}
		}
	}
}
