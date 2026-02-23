package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/riyansh/chat-backend/internal/domain/trading"
)

const maxDroppedMessages = 5

func (h *Hub) Run() {
	for {
		select {

		// ---------------- REGISTER ----------------
		case client := <-h.Register:
			fmt.Println("Register client:", client.ID)

		// ---------------- UNREGISTER ----------------
		case client := <-h.Unregister:
			fmt.Println("Unregistering client:", client.ID)
			for roomName := range client.Rooms {
				if room, ok := h.Rooms[roomName]; ok {
					delete(room.Clients, client)
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
			}

			room.Clients[event.Client] = true
			event.Client.Rooms[roomName] = true

			fmt.Println(event.Client.ID, "joined", roomName)

			// Warm-start from Redis
			if h.redisCache != nil {
				data, err := h.redisCache.GetLastPrice(
					context.Background(),
					roomName,
				)

				if err == nil {
					select {

					// Successful warm-start delivery
					case event.Client.Send <- Message{
						Type: roomName,
						Data: json.RawMessage(data),
					}:
						event.Client.Dropped = 0

					// Warm-start drop
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

		// ---------------- BROADCAST ----------------
		case event := <-h.Broadcast:

			room, ok := h.Rooms[event.Room]
			if !ok {
				continue
			}

			for client := range room.Clients {
				select {

				// ✅ Successful delivery → reset drop counter
				case client.Send <- event.Message:
					client.Dropped = 0

				// ❌ Client too slow → count drop
				default:
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
		}
	}
}
