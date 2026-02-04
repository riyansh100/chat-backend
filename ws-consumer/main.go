package main

import (
	"log"
	"net/url"
	"os"

	"github.com/gorilla/websocket"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run main.go [btc|eth|both]")
	}

	mode := os.Args[1]

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

	// Subscribe based on mode
	switch mode {
	case "btc":
		join(conn, "BTC_USDT")
	case "eth":
		join(conn, "ETH_USDT")
	case "both":
		join(conn, "BTC_USDT")
		join(conn, "ETH_USDT")
	default:
		log.Fatal("Invalid option. Use btc | eth | both")
	}

	log.Printf("Subscribed mode: %s\n", mode)

	// Read events
	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			log.Println("read error:", err)
			return
		}
		log.Printf("EVENT RECEIVED (%s): %+v\n", mode, msg)
	}
}

func join(conn *websocket.Conn, room string) {
	err := conn.WriteJSON(map[string]string{
		"type": "join",
		"room": room,
	})
	if err != nil {
		log.Fatal("join error:", err)
	}
}
