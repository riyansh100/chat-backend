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

		case event := <-h.LeaveRoom:
			if room, ok := h.Rooms[event.Room]; ok {
				delete(room.Clients, event.Client)
				fmt.Println(event.Client.ID, "left", event.Room)
			}
			delete(event.Client.Rooms, event.Room)

		case event := <-h.Broadcast:

			room := h.Rooms[event.Room]
			for client := range room.Clients {
				select {
				case client.Send <- event.Message:
				default:
					h.Unregister <- client
				}
			}

			// 🔹 publish to Redis (fire-and-forget)
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
