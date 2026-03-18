package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/riyansh/chat-backend/internal/analytics"
	"github.com/riyansh/chat-backend/internal/domain/trading"
)

const maxDroppedMessages = 5

func (h *Hub) Run() {
	for {
		select {

		// ---------------- REGISTER ----------------
		case client := <-h.Register:
			h.Metrics.ActiveClients.Add(1)
			fmt.Println("Register client:", client.ID)

		// ---------------- UNREGISTER ----------------
		case client := <-h.Unregister:
			h.Metrics.ActiveClients.Add(-1)
			fmt.Println("Unregistering client:", client.ID)
			for roomName := range client.Rooms {
				if room, ok := h.Rooms[roomName]; ok {
					delete(room.Clients, client)
					h.Metrics.ActiveRooms.Add(-1)
				}
			}
			h.subManager.UnsubscribeAll(client.ID)

		// ---------------- SUBSCRIBE (pull model) ----------------
		case event := <-h.Subscribe:
			h.subManager.Subscribe(event.Client.ID, event.Topic, event.Client.IndicatorFeed)
			event.Client.IndicatorFeed <- Message{
				Type: "subscribed",
				Data: map[string]interface{}{"topic": event.Topic},
			}

		// ---------------- UNSUBSCRIBE (pull model) ----------------
		case event := <-h.Unsubscribe:
			h.subManager.Unsubscribe(event.Client.ID, event.Topic)
			event.Client.IndicatorFeed <- Message{
				Type: "unsubscribed",
				Data: map[string]interface{}{"topic": event.Topic},
			}

		// ---------------- JOIN ROOM ----------------
		case event := <-h.JoinRoom:
			roomName := event.Room
			if id, ok := trading.SymbolToID[roomName]; ok {
				roomName = strconv.Itoa(id)
			}

			room, ok := h.Rooms[roomName]
			if !ok {
				room = &Room{
					Name:    roomName,
					Clients: make(map[*Client]bool),
				}
				h.Rooms[roomName] = room
				h.Metrics.ActiveRooms.Add(1)
			}

			room.Clients[event.Client] = true
			event.Client.Rooms[roomName] = true
			fmt.Println(event.Client.ID, "joined", roomName)

			// ---------------- SMA HISTORY ----------------
			if h.smaStore != nil {
				instrumentID, err := strconv.Atoi(roomName)
				if err == nil {
					go func(client *Client, store *analytics.SMAStore, id int) {
						for _, res := range []struct {
							resolution string
							n          int
						}{
							{"1s", 1800},
							{"1m", 60},
						} {
							entries, err := store.GetLast(context.Background(), id, res.n, res.resolution)
							if err != nil || len(entries) == 0 {
								continue
							}
							points := make([]map[string]interface{}, 0, len(entries))
							for _, z := range entries {
								points = append(points, map[string]interface{}{
									"ts": int64(z.Score), "value": z.Member,
								})
							}
							select {
							case client.Send <- Message{
								Type: "sma_history",
								Data: map[string]interface{}{
									"instrument_id": id,
									"window":        20,
									"resolution":    res.resolution,
									"points":        points,
								},
							}:
							default:
							}
						}
					}(event.Client, h.smaStore, instrumentID)
				}
			}

			// ---------------- OHLC HISTORY ----------------
			if h.ohlcStore != nil {
				instrumentID, err := strconv.Atoi(roomName)
				if err == nil {
					go func(client *Client, store *analytics.OHLCStore, id int) {
						entries, err := store.GetLast(context.Background(), id, 60)
						if err != nil || len(entries) == 0 {
							return
						}
						candles := make([]map[string]interface{}, 0, len(entries))
						for _, z := range entries {
							candles = append(candles, map[string]interface{}{
								"ts": int64(z.Score), "candle": z.Member,
							})
						}
						select {
						case client.Send <- Message{
							Type: "ohlc_history",
							Data: map[string]interface{}{
								"instrument_id": id,
								"resolution":    "1m",
								"candles":       candles,
							},
						}:
						default:
						}
					}(event.Client, h.ohlcStore, instrumentID)
				}
			}

			// ---------------- EMA HISTORY ----------------
			if h.emaStore != nil {
				instrumentID, err := strconv.Atoi(roomName)
				if err == nil {
					go func(client *Client, store *analytics.EMAStore, id int) {
						for _, res := range []struct {
							resolution string
							n          int
						}{
							{"1s", 1800},
							{"1m", 60},
						} {
							entries, err := store.GetLast(context.Background(), id, res.n, res.resolution)
							if err != nil || len(entries) == 0 {
								continue
							}
							points := make([]map[string]interface{}, 0, len(entries))
							for _, z := range entries {
								points = append(points, map[string]interface{}{
									"ts": int64(z.Score), "value": z.Member,
								})
							}
							select {
							case client.Send <- Message{
								Type: "ema_history",
								Data: map[string]interface{}{
									"instrument_id": id,
									"window":        20,
									"resolution":    res.resolution,
									"points":        points,
								},
							}:
							default:
							}
						}
					}(event.Client, h.emaStore, instrumentID)
				}
			}

			// ---------------- BB HISTORY ----------------
			if h.bbStore != nil {
				instrumentID, err := strconv.Atoi(roomName)
				if err == nil {
					go func(client *Client, store *analytics.BBStore, id int) {
						entries, err := store.GetLast(context.Background(), id, 60)
						if err != nil || len(entries) == 0 {
							return
						}
						candles := make([]map[string]interface{}, 0, len(entries))
						for _, z := range entries {
							candles = append(candles, map[string]interface{}{
								"ts": int64(z.Score), "band": z.Member,
							})
						}
						select {
						case client.Send <- Message{
							Type: "bb_history",
							Data: map[string]interface{}{
								"instrument_id": id,
								"resolution":    "1m",
								"window":        20,
								"k":             2.0,
								"bands":         candles,
							},
						}:
						default:
						}
					}(event.Client, h.bbStore, instrumentID)
				}
			}

			// ---------------- L1 CACHE CHECK ----------------
			key := fmt.Sprintf("instrument:%s:last", roomName)
			if val, ok := h.l1.Get(key); ok {
				if msg, ok := val.(Message); ok {
					fmt.Println("L1 HIT:", roomName)
					select {
					case event.Client.Send <- msg:
						event.Client.Dropped = 0
					default:
						event.Client.Dropped++
						if event.Client.Dropped > maxDroppedMessages {
							fmt.Println("Disconnecting slow client:", event.Client.ID)
							h.Unregister <- event.Client
						}
					}
					continue
				}
			}

			// ---------------- REDIS FALLBACK ----------------
			if h.redisCache != nil {
				data, err := h.redisCache.GetLastPrice(context.Background(), roomName)
				if err == nil {
					msg := Message{Type: roomName, Data: json.RawMessage(data)}
					h.l1.Set(key, msg)
					select {
					case event.Client.Send <- msg:
						event.Client.Dropped = 0
					default:
						event.Client.Dropped++
						if event.Client.Dropped > maxDroppedMessages {
							fmt.Println("Disconnecting slow client:", event.Client.ID)
							h.Unregister <- event.Client
						}
					}
				}
			}

		// ---------------- LEAVE ROOM ----------------
		case event := <-h.LeaveRoom:
			roomName := event.Room
			if id, ok := trading.SymbolToID[roomName]; ok {
				roomName = strconv.Itoa(id)
			}
			if room, ok := h.Rooms[roomName]; ok {
				delete(room.Clients, event.Client)
				fmt.Println(event.Client.ID, "left", roomName)
			}
			delete(event.Client.Rooms, roomName)
			h.Metrics.ActiveRooms.Add(-1)

		// ---------------- BROADCAST ----------------
		case event := <-h.Broadcast:
			h.Metrics.EventsBroadcasted.Add(1)

			key := fmt.Sprintf("instrument:%s:last", event.Room)
			h.l1.Set(key, event.Message)

			room, ok := h.Rooms[event.Room]
			if !ok {
				continue
			}

			h.Metrics.MessagesDelivered.Add(1)
			for client := range room.Clients {
				select {
				case client.Send <- event.Message:
					client.Dropped = 0
				default:
					h.Metrics.MessagesDropped.Add(1)
					client.Dropped++
					if client.Dropped > maxDroppedMessages {
						fmt.Println("Disconnecting slow client:", client.ID)
						h.Unregister <- client
					}
				}
			}

			// Redis pub/sub — only locally-originated events
			if event.Origin == h.InstanceID {
				rm := RedisMessage{
					Room:   event.Room,
					Type:   event.Message.Type,
					Data:   event.Message.Data,
					Origin: h.InstanceID,
				}
				payload, _ := json.Marshal(rm)
				go h.RedisClient.Publish(context.Background(), "chat:events", payload)
			}

			// ---------------- ANALYTICS LAYER ----------------
			instrumentID, err := strconv.Atoi(event.Room)
			if err != nil {
				break
			}
			dataMap, ok := event.Message.Data.(map[string]interface{})
			if !ok {
				break
			}
			priceVal, ok := dataMap["price"]
			if !ok {
				break
			}
			priceFloat, ok := priceVal.(float64)
			if !ok {
				break
			}

			tick := analytics.PriceUpdateEvent{
				InstrumentID: instrumentID,
				Price:        priceFloat,
				Timestamp:    time.Now().UnixNano(),
			}
			select {
			case h.smaEngine.Input() <- tick:
			default:
			}
			select {
			case h.ohlcEngine.Input() <- tick:
			default:
			}
			select {
			case h.emaEngine.Input() <- tick:
			default:
			}
			select {
			case h.bbEngine.Input() <- tick:
			default:
			}
		}
	}
}
