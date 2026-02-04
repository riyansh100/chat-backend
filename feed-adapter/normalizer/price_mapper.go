package normalizer

import "github.com/riyanshsachdev/feed-adapter/exchange"

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
