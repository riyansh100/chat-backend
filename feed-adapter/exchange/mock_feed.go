package exchange

import (
	"math/rand"
	"time"
)

type RawPrice struct {
	Instrument string
	Price      float64
	Timestamp  int64
}

func StartMockFeed(out chan<- RawPrice) {
	ticker := time.NewTicker(500 * time.Millisecond)

	instruments := []string{"BTC_USDT", "ETH_USDT"}

	go func() {
		for range ticker.C {
			inst := instruments[rand.Intn(len(instruments))]

			price := 0.0
			if inst == "BTC_USDT" {
				price = 60000 + rand.Float64()*1000
			} else {
				price = 3000 + rand.Float64()*100
			}

			out <- RawPrice{
				Instrument: inst,
				Price:      price,
				Timestamp:  time.Now().Unix(),
			}
		}
	}()
}
