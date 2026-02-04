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

	log.Println("Consumer connecting...")
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	switch mode {
	case "btc":
		join(conn, "BTC_USDT")
		log.Println("Subscribed to BTC_USDT")

	case "eth":
		join(conn, "ETH_USDT")
		log.Println("Subscribed to ETH_USDT")

	case "both":
		join(conn, "BTC_USDT")
		join(conn, "ETH_USDT")
		log.Println("Subscribed to BTC_USDT and ETH_USDT")

	default:
		log.Fatal("Invalid option. Use btc | eth | both")
	}

	log.Println("Waiting for data...")

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
