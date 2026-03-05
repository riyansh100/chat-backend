package main

import (
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

			dataBytes, _ := json.Marshal(msg["data"])

			var sma struct {
				InstrumentID int     `json:"instrument_id"`
				Value        float64 `json:"value"`
				Timestamp    int64   `json:"timestamp"`
			}

			if err := json.Unmarshal(dataBytes, &sma); err != nil {
				log.Println("sma_update decode error:", err)
				continue
			}

			log.Printf("📊 SMA(%d) = %.2f\n", sma.InstrumentID, sma.Value)

		} else if msgType == "sma_history" {

			dataBytes, _ := json.Marshal(msg["data"])

			var history struct {
				InstrumentID int `json:"instrument_id"`
				Points       []struct {
					Ts    int64  `json:"ts"`
					Value string `json:"value"`
				} `json:"points"`
			}

			if err := json.Unmarshal(dataBytes, &history); err != nil {
				log.Println("sma_history decode error:", err)
				continue
			}

			log.Printf("📈 SMA HISTORY instrument=%d points=%d (oldest → newest)\n",
				history.InstrumentID, len(history.Points))

			if len(history.Points) > 0 {
				first := history.Points[0]
				last := history.Points[len(history.Points)-1]
				log.Printf("   first: ts=%d value=%s\n", first.Ts, first.Value)
				log.Printf("   last:  ts=%d value=%s\n", last.Ts, last.Value)
			}

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
