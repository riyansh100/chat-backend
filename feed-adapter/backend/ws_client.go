package backend

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type WSIngestor struct {
	url    string
	apiKey string
	conn   *websocket.Conn
}

// Create ingestor (does NOT connect yet)
func NewWSIngestor(url string, apiKey string) *WSIngestor {
	return &WSIngestor{
		url:    url,
		apiKey: apiKey,
	}
}

// Start runs forever: connect → send → reconnect on failure
func (w *WSIngestor) Start(events <-chan interface{}) {
	for {
		log.Println("Connecting to backend ingest WS...")

		headers := make(map[string][]string)
		headers["X-API-Key"] = []string{w.apiKey}

		conn, _, err := websocket.DefaultDialer.Dial(w.url, headers)
		if err != nil {
			log.Println("WS connect failed:", err)
			time.Sleep(1 * time.Second)
			continue
		}

		w.conn = conn
		log.Println("Connected to backend ingest WS")

		// stream events until a write fails
		if err := w.stream(events); err != nil {
			log.Println("ingest connection lost:", err)
			_ = conn.Close()
			time.Sleep(1 * time.Second)
		}
	}
}

// stream sends events until a write error occurs
func (w *WSIngestor) stream(events <-chan interface{}) error {
	for event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			log.Println("marshal error:", err)
			continue
		}

		if err := w.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return err // triggers reconnect
		}
	}
	return nil
}
