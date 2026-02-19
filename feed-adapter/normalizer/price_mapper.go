package normalizer

import "github.com/riyansh/chat-backend/feed-adapter/exchange"

type PriceUpdateEvent struct {
	Type       string  `json:"type"`
	Instrument string  `json:"instrument"`
	Price      float64 `json:"price"`
	Timestamp  int64   `json:"ts"`
}

func MapToDomain(raw exchange.RawPrice) PriceUpdateEvent {
	return PriceUpdateEvent{
		Type:       "price_update",
		Instrument: raw.Instrument,
		Price:      raw.Price,
		Timestamp:  raw.Timestamp,
	}
}

func PriceUpdateFromNormalized(n exchange.NormalizedPriceEvent) interface{} {
	return PriceUpdateEvent{
		Type:       "price_update",
		Instrument: n.Instrument,
		Price:      n.Price,
		Timestamp:  n.Timestamp,
	}
}
