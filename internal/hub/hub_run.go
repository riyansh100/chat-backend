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

		// ---------------- JOIN ROOM ----------------
		case event := <-h.JoinRoom:

			roomName := event.Room

			// Accept symbol OR numeric ID
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
			// Must be before L1/Redis warm-start (those use continue/return)
			if h.smaStore != nil {
				instrumentID, err := strconv.Atoi(roomName)
				if err == nil {
					go func(client *Client, id int) {
						entries, err := h.smaStore.GetLast(context.Background(), id, 1800)
						fmt.Printf("[SMA HISTORY] instrument=%d entries=%d err=%v\n", id, len(entries), err)
						if err != nil || len(entries) == 0 {
							return
						}

						points := make([]map[string]interface{}, 0, len(entries))
						for _, z := range entries {
							points = append(points, map[string]interface{}{
								"ts":    int64(z.Score),
								"value": z.Member,
							})
						}

						msg := Message{
							Type: "sma_history",
							Data: map[string]interface{}{
								"instrument_id": id,
								"window":        20,
								"resolution":    "1s",
								"points":        points,
							},
						}

						select {
						case client.Send <- msg:
							fmt.Printf("[SMA HISTORY] sent to client %s\n", client.ID)
						default:
							fmt.Printf("[SMA HISTORY] client send channel full — dropped\n")
						}
					}(event.Client, instrumentID)
				}
			} else {
				fmt.Println("[SMA HISTORY] smaStore is nil — skipping")
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
							fmt.Println("Disconnecting slow client:", event.Client.ID, "drops:", event.Client.Dropped)
							h.Unregister <- event.Client
						}

					}
					continue // Skip Redis if L1 hit
				}
			}

			// ---------------- REDIS FALLBACK ----------------
			if h.redisCache != nil {
				data, err := h.redisCache.GetLastPrice(
					context.Background(),
					roomName,
				)

				if err == nil {

					msg := Message{
						Type: roomName,
						Data: json.RawMessage(data),
					}

					// Populate L1
					h.l1.Set(key, msg)

					select {
					case event.Client.Send <- msg:
						event.Client.Dropped = 0
					default:
						event.Client.Dropped++
						if event.Client.Dropped > maxDroppedMessages {
							fmt.Println("Disconnecting slow client:", event.Client.ID, "drops:", event.Client.Dropped)
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

			// -------- L1 UPDATE --------
			key := fmt.Sprintf("instrument:%s:last", event.Room)
			h.l1.Set(key, event.Message)

			room, ok := h.Rooms[event.Room]
			if !ok {
				continue
			}
			h.Metrics.MessagesDelivered.Add(1)
			for client := range room.Clients {
				select {

				// ✅ Successful delivery → reset drop counter
				case client.Send <- event.Message:
					client.Dropped = 0

				// ❌ Client too slow → count drop
				default:
					h.Metrics.MessagesDropped.Add(1)
					client.Dropped++

					if client.Dropped > maxDroppedMessages {
						fmt.Println("Disconnecting slow client:", client.ID, "drops:", client.Dropped)
						h.Unregister <- client
					}
				}
			}

			// Redis bus publish — ONLY for locally-originated events
			if event.Origin == h.InstanceID {
				rm := RedisMessage{
					Room:   event.Room,
					Type:   event.Message.Type,
					Data:   event.Message.Data,
					Origin: h.InstanceID,
				}
				payload, _ := json.Marshal(rm)
				go h.RedisClient.Publish(
					context.Background(),
					"chat:events",
					payload,
				)
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

			select {
			case h.smaEngine.Input() <- analytics.PriceUpdateEvent{
				InstrumentID: instrumentID,
				Price:        priceFloat,
				Timestamp:    time.Now().UnixNano(),
			}:
			default:
				// analytics overloaded — safe drop
			}
		}
	}
}
