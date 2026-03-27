package background

import (
	"context"
	"log"
	"math/rand"
	"time"

	"github.com/riyansh/chat-backend/feed-adapter/exchange"
	"github.com/riyansh/chat-backend/internal/analytics"
	"github.com/riyansh/chat-backend/internal/domain/trading"
)

const binanceEndpoint = "wss://stream.binance.com:9443/stream?streams=" +
	// original 20
	"btcusdt@trade/ethusdt@trade/bnbusdt@trade/xrpusdt@trade/solusdt@trade/" +
	"adausdt@trade/dogeusdt@trade/maticusdt@trade/ltcusdt@trade/dotusdt@trade/" +
	"avaxusdt@trade/linkusdt@trade/uniusdt@trade/atomusdt@trade/trxusdt@trade/" +
	"etcusdt@trade/filusdt@trade/icpusdt@trade/aptusdt@trade/arbusdt@trade/" +
	// extended 80
	"opusdt@trade/injusdt@trade/suiusdt@trade/stxusdt@trade/imxusdt@trade/" +
	"grtusdt@trade/aaveusdt@trade/snxusdt@trade/mkrusdt@trade/compusdt@trade/" +
	"ldousdt@trade/rplusdt@trade/ftmusdt@trade/nearusdt@trade/algousdt@trade/" +
	"vetusdt@trade/hbarusdt@trade/egldusdt@trade/xtzusdt@trade/thetausdt@trade/" +
	"axsusdt@trade/sandusdt@trade/manausdt@trade/enjusdt@trade/chzusdt@trade/" +
	"oneusdt@trade/zilusdt@trade/batusdt@trade/zrxusdt@trade/crvusdt@trade/" +
	"1inchusdt@trade/dydxusdt@trade/perpusdt@trade/sushiusdt@trade/yfiusdt@trade/" +
	"balusdt@trade/renusdt@trade/bntusdt@trade/kncusdt@trade/oceanusdt@trade/" +
	"roseusdt@trade/celousdt@trade/cfxusdt@trade/kavausdt@trade/bandusdt@trade/" +
	"sklusdt@trade/ckbusdt@trade/scusdt@trade/zenusdt@trade/dashusdt@trade/" +
	"xmrusdt@trade/dcrusdt@trade/zecusdt@trade/qtumusdt@trade/ontusdt@trade/" +
	"icxusdt@trade/wanusdt@trade/steemusdt@trade/lskusdt@trade/arkusdt@trade/" +
	"wldusdt@trade/tiausdt@trade/seiusdt@trade/jtousdt@trade/pythusdt@trade/" +
	"jupusdt@trade/strkusdt@trade/mantausdt@trade/altusdt@trade/pixelusdt@trade/" +
	"portalusdt@trade/dymusdt@trade/ethfiusdt@trade/enausdt@trade/wusdt@trade/" +
	"iousdt@trade/zkusdt@trade/listausdt@trade/renderusdt@trade/fetusdt@trade"

type Worker struct {
	registry *analytics.Registry
	source   string
}

func NewWorker(source string, registry *analytics.Registry) *Worker {
	return &Worker{registry: registry, source: source}
}

func (w *Worker) Start(ctx context.Context) {
	log.Printf("[BG Worker] starting with source=%s", w.source)
	switch w.source {
	case "binance":
		w.runBinance(ctx)
	default:
		w.runMock(ctx)
	}
}

func (w *Worker) runBinance(ctx context.Context) {
	out := make(chan exchange.NormalizedPriceEvent, 2048)
	adapter := &exchange.BinanceAdapter{
		Endpoint: binanceEndpoint,
		Out:      out,
	}
	go adapter.Start(ctx)
	log.Println("[BG Worker] Binance feed connected (100 instruments), feeding analytics engines...")

	for {
		select {
		case <-ctx.Done():
			log.Println("[BG Worker] shutting down")
			return
		case evt := <-out:
			id, ok := trading.SymbolToID[evt.Instrument]
			if !ok {
				continue
			}
			w.registry.Feed(analytics.PriceUpdateEvent{
				InstrumentID: id,
				Price:        evt.Price,
				Timestamp:    time.Now().UnixNano(),
			})
		}
	}
}

func (w *Worker) runMock(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	log.Println("[BG Worker] mock feed running (100 instruments), feeding analytics engines...")

	basePrices := map[int]float64{
		// original 20
		101: 60000, 102: 3000, 103: 300, 104: 0.5,
		105: 150, 106: 0.4, 107: 0.08, 108: 0.7,
		109: 80, 110: 7, 111: 35, 112: 15,
		113: 8, 114: 9, 115: 0.12, 116: 25,
		117: 5, 118: 10, 119: 12, 120: 1.5,
		// extended 80
		121: 3.5, 122: 25, 123: 1.2, 124: 2.1, 125: 2.0,
		126: 0.18, 127: 95, 128: 3.2, 129: 2800, 130: 55,
		131: 2.1, 132: 28, 133: 0.55, 134: 6.5, 135: 0.18,
		136: 0.04, 137: 0.09, 138: 45, 139: 0.9, 140: 1.8,
		141: 7.5, 142: 0.45, 143: 0.4, 144: 0.25, 145: 0.08,
		146: 0.015, 147: 0.02, 148: 0.22, 149: 0.35, 150: 0.6,
		151: 0.38, 152: 2.1, 153: 0.9, 154: 1.1, 155: 6500,
		156: 4.5, 157: 0.06, 158: 0.55, 159: 0.7, 160: 0.9,
		161: 0.09, 162: 0.75, 163: 0.28, 164: 0.65, 165: 1.4,
		166: 0.05, 167: 0.012, 168: 0.008, 169: 8.5, 170: 32,
		171: 155, 172: 18, 173: 28, 174: 3.2, 175: 0.3,
		176: 0.22, 177: 0.18, 178: 0.22, 179: 1.1, 180: 0.6,
		181: 5.5, 182: 8.5, 183: 0.45, 184: 3.2, 185: 0.38,
		186: 0.85, 187: 0.55, 188: 1.8, 189: 0.12, 190: 0.35,
		191: 0.9, 192: 3.5, 193: 3.8, 194: 0.75, 195: 0.55,
		196: 4.2, 197: 0.18, 198: 0.022, 199: 7.5, 200: 1.6,
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("[BG Worker] shutting down")
			return
		case <-ticker.C:
			for id, base := range basePrices {
				w.registry.Feed(analytics.PriceUpdateEvent{
					InstrumentID: id,
					Price:        base + rand.Float64()*base*0.01,
					Timestamp:    time.Now().UnixNano(),
				})
			}
		}
	}
}
