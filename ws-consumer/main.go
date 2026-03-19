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

	lastPrinted := make(map[string]time.Time)

	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			log.Println("read error:", err)
			return
		}

		msgType, _ := msg["type"].(string)
		dataBytes, _ := json.Marshal(msg["data"])

		switch msgType {

		case "sma_update":
			var d struct {
				InstrumentID int     `json:"instrument_id"`
				Value        float64 `json:"value"`
				Timestamp    int64   `json:"timestamp"`
				Resolution   string  `json:"resolution"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("📊 SMA [%s] (%d) = %.5f", d.Resolution, d.InstrumentID, d.Value)

		case "sma_history":
			var d struct {
				InstrumentID int    `json:"instrument_id"`
				Resolution   string `json:"resolution"`
				Points       []struct {
					Ts    int64  `json:"ts"`
					Value string `json:"value"`
				} `json:"points"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("📈 SMA HISTORY [%s] instrument=%d points=%d", d.Resolution, d.InstrumentID, len(d.Points))
			if len(d.Points) > 0 {
				log.Printf("   first: ts=%d value=%s", d.Points[0].Ts, d.Points[0].Value)
				log.Printf("   last:  ts=%d value=%s", d.Points[len(d.Points)-1].Ts, d.Points[len(d.Points)-1].Value)
			}

		case "ema_update":
			var d struct {
				InstrumentID int     `json:"instrument_id"`
				Value        float64 `json:"value"`
				Timestamp    int64   `json:"timestamp"`
				Resolution   string  `json:"resolution"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("📉 EMA [%s] (%d) = %.5f", d.Resolution, d.InstrumentID, d.Value)

		case "ema_history":
			var d struct {
				InstrumentID int    `json:"instrument_id"`
				Resolution   string `json:"resolution"`
				Points       []struct {
					Ts    int64  `json:"ts"`
					Value string `json:"value"`
				} `json:"points"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("📉 EMA HISTORY [%s] instrument=%d points=%d", d.Resolution, d.InstrumentID, len(d.Points))
			if len(d.Points) > 0 {
				log.Printf("   first: ts=%d value=%s", d.Points[0].Ts, d.Points[0].Value)
				log.Printf("   last:  ts=%d value=%s", d.Points[len(d.Points)-1].Ts, d.Points[len(d.Points)-1].Value)
			}

		case "ohlc_update":
			var d struct {
				InstrumentID int     `json:"instrument_id"`
				Resolution   string  `json:"resolution"`
				Open         float64 `json:"open"`
				High         float64 `json:"high"`
				Low          float64 `json:"low"`
				Close        float64 `json:"close"`
				Timestamp    int64   `json:"timestamp"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("🕯️  OHLC [%s] (%d) O=%.2f H=%.2f L=%.2f C=%.2f",
				d.Resolution, d.InstrumentID, d.Open, d.High, d.Low, d.Close)

		case "ohlc_history":
			var d struct {
				InstrumentID int    `json:"instrument_id"`
				Resolution   string `json:"resolution"`
				Candles      []struct {
					Ts     int64  `json:"ts"`
					Candle string `json:"candle"`
				} `json:"candles"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("🕯️  OHLC HISTORY [%s] instrument=%d candles=%d", d.Resolution, d.InstrumentID, len(d.Candles))
			if len(d.Candles) > 0 {
				log.Printf("   first: ts=%d", d.Candles[0].Ts)
				log.Printf("   last:  ts=%d", d.Candles[len(d.Candles)-1].Ts)
			}

		case "bb_update":
			var d struct {
				InstrumentID int     `json:"instrument_id"`
				Upper        float64 `json:"upper"`
				Middle       float64 `json:"middle"`
				Lower        float64 `json:"lower"`
				Resolution   string  `json:"resolution"`
				Timestamp    int64   `json:"timestamp"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("📐 BB [%s] (%d) U=%.5f M=%.5f L=%.5f", d.Resolution, d.InstrumentID, d.Upper, d.Middle, d.Lower)

		case "bb_history":
			var d struct {
				InstrumentID int    `json:"instrument_id"`
				Resolution   string `json:"resolution"`
				Bands        []struct {
					Ts   int64  `json:"ts"`
					Band string `json:"band"`
				} `json:"bands"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("📐 BB HISTORY [%s] instrument=%d bands=%d", d.Resolution, d.InstrumentID, len(d.Bands))
			if len(d.Bands) > 0 {
				log.Printf("   first: ts=%d", d.Bands[0].Ts)
				log.Printf("   last:  ts=%d", d.Bands[len(d.Bands)-1].Ts)
			}

		case "rsi_update":
			var d struct {
				InstrumentID int     `json:"instrument_id"`
				Value        float64 `json:"value"`
				Resolution   string  `json:"resolution"`
				Timestamp    int64   `json:"timestamp"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("💪 RSI [%s] (%d) = %.2f", d.Resolution, d.InstrumentID, d.Value)

		case "rsi_history":
			var d struct {
				InstrumentID int    `json:"instrument_id"`
				Resolution   string `json:"resolution"`
				Points       []struct {
					Ts    int64  `json:"ts"`
					Value string `json:"value"`
				} `json:"points"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("💪 RSI HISTORY [%s] instrument=%d points=%d", d.Resolution, d.InstrumentID, len(d.Points))
			if len(d.Points) > 0 {
				log.Printf("   first: ts=%d value=%s", d.Points[0].Ts, d.Points[0].Value)
				log.Printf("   last:  ts=%d value=%s", d.Points[len(d.Points)-1].Ts, d.Points[len(d.Points)-1].Value)
			}

		case "macd_update":
			var d struct {
				InstrumentID int     `json:"instrument_id"`
				MACDLine     float64 `json:"macd_line"`
				SignalLine   float64 `json:"signal_line"`
				Histogram    float64 `json:"histogram"`
				Resolution   string  `json:"resolution"`
				Timestamp    int64   `json:"timestamp"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("📶 MACD [%s] (%d) line=%.5f signal=%.5f hist=%.5f",
				d.Resolution, d.InstrumentID, d.MACDLine, d.SignalLine, d.Histogram)

		case "macd_history":
			var d struct {
				InstrumentID int    `json:"instrument_id"`
				Resolution   string `json:"resolution"`
				Points       []struct {
					Ts   int64  `json:"ts"`
					Data string `json:"data"`
				} `json:"points"`
			}
			if err := json.Unmarshal(dataBytes, &d); err != nil {
				continue
			}
			log.Printf("📶 MACD HISTORY [%s] instrument=%d points=%d", d.Resolution, d.InstrumentID, len(d.Points))
			if len(d.Points) > 0 {
				log.Printf("   first: ts=%d", d.Points[0].Ts)
				log.Printf("   last:  ts=%d", d.Points[len(d.Points)-1].Ts)
			}

		default:
			var price struct {
				Instrument string  `json:"instrument"`
				Price      float64 `json:"price"`
			}
			json.Unmarshal(dataBytes, &price)
			if time.Since(lastPrinted[price.Instrument]) >= time.Second {
				log.Printf("💰 PRICE %s = %.2f", price.Instrument, price.Price)
				lastPrinted[price.Instrument] = time.Now()
			}
		}
	}
}

func join(conn *websocket.Conn, room string) {
	if err := conn.WriteJSON(map[string]string{"type": "join", "room": room}); err != nil {
		log.Fatal("join error:", err)
	}
}
