package main

import (
	"encoding/base64"
	"encoding/json"
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
		//log.Printf("EVENT RECEIVED (%s): %+v\n", input, msg)

		msgType, _ := msg["type"].(string)

if msgType == "sma_update" {

	encoded, ok := msg["data"].(string)
	if !ok {
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		log.Println("decode error:", err)
		return
	}

	var sma struct {
		InstrumentID int     `json:"instrument_id"`
		Value        float64 `json:"value"`
		Timestamp    int64   `json:"timestamp"`
	}

	err = json.Unmarshal(decoded, &sma)
	if err != nil {
		log.Println("json decode error:", err)
		return
	}

	log.Printf("📊 SMA(%d) = %.2f\n", sma.InstrumentID, sma.Value)

} else {

	dataBytes, _ := json.Marshal(msg["data"])

	var price struct {
		Instrument string  `json:"instrument"`
		Price      float64 `json:"price"`
	}

	json.Unmarshal(dataBytes, &price)

	log.Printf("💰 PRICE %s = %.2f\n", price.Instrument, price.Price)
}
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
