package main

import (
	"encoding/json"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run main.go [instrument_id | id1,id2,...]")
	}

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

	for _, room := range rooms {
		room = strings.TrimSpace(room)
		join(conn, room)
		log.Println("Subscribed to instrument ID:", room)
	}

	// price bucket: track last printed time per instrument
	lastPrinted := make(map[string]time.Time)

	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			log.Println("read error:", err)
			return
		}

		msgType, _ := msg["type"].(string)

		if msgType == "sma_update" {

			dataBytes, _ := json.Marshal(msg["data"])

			var sma struct {
				InstrumentID int     `json:"instrument_id"`
				Value        float64 `json:"value"`
				Timestamp    int64   `json:"timestamp"`
				Resolution   string  `json:"resolution"`
			}

			if err := json.Unmarshal(dataBytes, &sma); err != nil {
				log.Println("sma_update decode error:", err)
				continue
			}

			log.Printf("📊 SMA [%s] (%d) = %.2f\n", sma.Resolution, sma.InstrumentID, sma.Value)

		} else if msgType == "sma_history" {

			dataBytes, _ := json.Marshal(msg["data"])

			var history struct {
				InstrumentID int    `json:"instrument_id"`
				Resolution   string `json:"resolution"`
				Points       []struct {
					Ts    int64  `json:"ts"`
					Value string `json:"value"`
				} `json:"points"`
			}

			if err := json.Unmarshal(dataBytes, &history); err != nil {
				log.Println("sma_history decode error:", err)
				continue
			}

			log.Printf("📈 SMA HISTORY [%s] instrument=%d points=%d\n",
				history.Resolution, history.InstrumentID, len(history.Points))

			if len(history.Points) > 0 {
				first := history.Points[0]
				last := history.Points[len(history.Points)-1]
				log.Printf("   first: ts=%d value=%s\n", first.Ts, first.Value)
				log.Printf("   last:  ts=%d value=%s\n", last.Ts, last.Value)
			}

		} else if msgType == "ohlc_update" {

			dataBytes, _ := json.Marshal(msg["data"])

			var ohlc struct {
				InstrumentID int     `json:"instrument_id"`
				Resolution   string  `json:"resolution"`
				Open         float64 `json:"open"`
				High         float64 `json:"high"`
				Low          float64 `json:"low"`
				Close        float64 `json:"close"`
				Timestamp    int64   `json:"timestamp"`
			}

			if err := json.Unmarshal(dataBytes, &ohlc); err != nil {
				log.Println("ohlc_update decode error:", err)
				continue
			}

			log.Printf("🕯️  OHLC [%s] (%d) O=%.2f H=%.2f L=%.2f C=%.2f\n",
				ohlc.Resolution, ohlc.InstrumentID,
				ohlc.Open, ohlc.High, ohlc.Low, ohlc.Close)

		} else if msgType == "ohlc_history" {

			dataBytes, _ := json.Marshal(msg["data"])

			var history struct {
				InstrumentID int    `json:"instrument_id"`
				Resolution   string `json:"resolution"`
				Candles      []struct {
					Ts     int64  `json:"ts"`
					Candle string `json:"candle"`
				} `json:"candles"`
			}

			if err := json.Unmarshal(dataBytes, &history); err != nil {
				log.Println("ohlc_history decode error:", err)
				continue
			}

			log.Printf("🕯️  OHLC HISTORY [%s] instrument=%d candles=%d\n",
				history.Resolution, history.InstrumentID, len(history.Candles))

			if len(history.Candles) > 0 {
				first := history.Candles[0]
				last := history.Candles[len(history.Candles)-1]
				log.Printf("   first: ts=%d\n", first.Ts)
				log.Printf("   last:  ts=%d\n", last.Ts)
			}

		} else {

			dataBytes, _ := json.Marshal(msg["data"])

			var price struct {
				Instrument string  `json:"instrument"`
				Price      float64 `json:"price"`
			}

			json.Unmarshal(dataBytes, &price)

			// only print once per second per instrument
			if time.Since(lastPrinted[price.Instrument]) >= time.Second {
				log.Printf("💰 PRICE %s = %.2f\n", price.Instrument, price.Price)
				lastPrinted[price.Instrument] = time.Now()
			}
		}
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
