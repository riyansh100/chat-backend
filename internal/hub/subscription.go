package hub

import (
	"sync"
)

// SubscriptionManager routes indicator updates to clients that explicitly
// subscribed to a specific topic (e.g. "sma:101", "ema:102", "ohlc:101").
//
// Each client gets one IndicatorFeed channel. The manager fans out matching
// topic messages into that channel, keeping WritePump simple (2 selects only).
type SubscriptionManager struct {
	mu   sync.RWMutex
	subs map[string]map[string]chan Message // topic → clientID → feed chan
}

func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		subs: make(map[string]map[string]chan Message),
	}
}

// Subscribe registers clientID for topic and returns the channel to write into.
// If already subscribed, returns the existing channel.
func (sm *SubscriptionManager) Subscribe(clientID, topic string, feed chan Message) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.subs[topic] == nil {
		sm.subs[topic] = make(map[string]chan Message)
	}
	sm.subs[topic][clientID] = feed
}

// Unsubscribe removes clientID from topic.
func (sm *SubscriptionManager) Unsubscribe(clientID, topic string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if clients, ok := sm.subs[topic]; ok {
		delete(clients, clientID)
		if len(clients) == 0 {
			delete(sm.subs, topic)
		}
	}
}

// UnsubscribeAll removes a client from every topic — call on disconnect.
func (sm *SubscriptionManager) UnsubscribeAll(clientID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for topic, clients := range sm.subs {
		delete(clients, clientID)
		if len(clients) == 0 {
			delete(sm.subs, topic)
		}
	}
}

// Fanout delivers msg to all clients subscribed to topic, non-blocking.
func (sm *SubscriptionManager) Fanout(topic string, msg Message) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	clients, ok := sm.subs[topic]
	if !ok {
		return
	}
	for _, feed := range clients {
		select {
		case feed <- msg:
		default:
			// client's IndicatorFeed is full — drop, never block fanout
		}
	}
}
