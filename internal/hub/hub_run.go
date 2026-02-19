package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/riyansh/chat-backend/internal/domain/trading"
)

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

			// 🔥 Stage B1 — accept symbol OR numeric ID
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

			// 🔥 Warm-start from Redis (already numeric-safe)
			if h.redisCache != nil {
				data, err := h.redisCache.GetLastPrice(
					context.Background(),
					roomName,
				)

				if err == nil {
					select {
					case event.Client.Send <- Message{
						Type: roomName,
						Data: json.RawMessage(data),
					}:
					default:
						h.Unregister <- event.Client
					}
				}
			}

		// ---------------- LEAVE ROOM ----------------
		case event := <-h.LeaveRoom:

			roomName := event.Room

			// 🔥 same dual-accept conversion for leave
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

			// DEBUG logs (safe to keep for now)
			fmt.Println(
				"DEBUG ORIGIN CHECK:",
				"event.Origin =", event.Origin,
				"hub.InstanceID =", h.InstanceID,
			)
			fmt.Println("DEBUG redisCache nil?", h.redisCache == nil)
			fmt.Println("DEBUG event origin:", event.Origin)

			// 🔁 Local fan-out (unchanged — still string room for Stage A compatibility)
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
