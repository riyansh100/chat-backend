package hub

import (
	"context"
	"encoding/json"
	"fmt"
)

func (h *Hub) Run() {
	for {
		select {

		case client := <-h.Register:
			fmt.Println("Register client:", client.ID)

		case client := <-h.Unregister:
			fmt.Println("Unregistering client:", client.ID)
			for roomName := range client.Rooms {
				if room, ok := h.Rooms[roomName]; ok {
					delete(room.Clients, client)
				}
			}

		case event := <-h.JoinRoom:
			room, ok := h.Rooms[event.Room]
			if !ok {
				room = &Room{
					Name:    event.Room,
					Clients: make(map[*Client]bool),
				}
				h.Rooms[event.Room] = room
			}

			room.Clients[event.Client] = true
			event.Client.Rooms[event.Room] = true

			fmt.Println(event.Client.ID, "joined", event.Room)

			// 🔥 STEP 5 — Warm-start from Redis (OPTIONAL, SAFE)
			if h.redisCache != nil {
				data, err := h.redisCache.GetLastPrice(
					context.Background(),
					event.Room,
				)

				if err == nil {
					select {
					case event.Client.Send <- Message{
						Type: event.Room,
						Data: json.RawMessage(data),
					}:
					default:
						// slow client — system health > client
						h.Unregister <- event.Client
					}
				}
			}

		case event := <-h.LeaveRoom:
			if room, ok := h.Rooms[event.Room]; ok {
				delete(room.Clients, event.Client)
				fmt.Println(event.Client.ID, "left", event.Room)
			}
			delete(event.Client.Rooms, event.Room)

		case event := <-h.Broadcast:

			// DEBUG (temporary – you can remove later)
			fmt.Println(
				"DEBUG ORIGIN CHECK:",
				"event.Origin =", event.Origin,
				"hub.InstanceID =", h.InstanceID,
			)
			fmt.Println("DEBUG redisCache nil?", h.redisCache == nil)
			fmt.Println("DEBUG event origin:", event.Origin)

			// 🧠 Redis KV write — ONLY for locally-originated events
			// if h.redisCache != nil && event.Origin == h.InstanceID {
			// 	fmt.Println("DEBUG writing KV for room:", event.Room)
			// 	_ = h.redisCache.SetLastPrice(
			// 		context.Background(),
			// 		event.Room,
			// 		event.Message.Data,
			// 	)
			// }

			// 🔁 Local fan-out (always happens)
			room, ok := h.Rooms[event.Room]
			if !ok {
				continue
			}

			for client := range room.Clients {
				select {
				case client.Send <- event.Message:
				default:
					h.Unregister <- client
				}
			}

			// 🔹 Redis bus publish — ONLY for locally-originated events
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
