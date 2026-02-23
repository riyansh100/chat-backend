package main

import (
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/gorilla/websocket"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run main.go [instrument_id | id1,id2,...]")
	}

	// Accept numeric IDs (example: 101 or 101,102)
	input := os.Args[1]
	rooms := strings.Split(input, ",")

	u := url.URL{
		Scheme: "ws",
		Host:   "localhost:8080",
		Path:   "/ws",
	}

	log.Println("Connecting consumer...")
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// Join all requested numeric rooms
	for _, room := range rooms {
		room = strings.TrimSpace(room)
		join(conn, room)
		log.Println("Subscribed to instrument ID:", room)
	}

	// Read events continuously
	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			log.Println("read error:", err)
			return
		}
		log.Printf("EVENT RECEIVED (%s): %+v\n", input, msg)
	}
}

func join(conn *websocket.Conn, room string) {
	err := conn.WriteJSON(map[string]string{
		"type": "join",
		"room": room, // send numeric ID directly
	})
	if err != nil {
		log.Fatal("join error:", err)
	}
}
