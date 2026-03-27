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

// 300 instruments — well within binance's 1024 stream/connection limit.
const binanceEndpoint = "wss://stream.binance.com:9443/stream?streams=" +
	// 101-120
	"btcusdt@trade/ethusdt@trade/bnbusdt@trade/xrpusdt@trade/solusdt@trade/" +
	"adausdt@trade/dogeusdt@trade/maticusdt@trade/ltcusdt@trade/dotusdt@trade/" +
	"avaxusdt@trade/linkusdt@trade/uniusdt@trade/atomusdt@trade/trxusdt@trade/" +
	"etcusdt@trade/filusdt@trade/icpusdt@trade/aptusdt@trade/arbusdt@trade/" +
	// 121-160
	"opusdt@trade/injusdt@trade/suiusdt@trade/stxusdt@trade/imxusdt@trade/" +
	"grtusdt@trade/aaveusdt@trade/snxusdt@trade/mkrusdt@trade/compusdt@trade/" +
	"ldousdt@trade/rplusdt@trade/ftmusdt@trade/nearusdt@trade/algousdt@trade/" +
	"vetusdt@trade/hbarusdt@trade/egldusdt@trade/xtzusdt@trade/thetausdt@trade/" +
	"axsusdt@trade/sandusdt@trade/manausdt@trade/enjusdt@trade/chzusdt@trade/" +
	"oneusdt@trade/zilusdt@trade/batusdt@trade/zrxusdt@trade/crvusdt@trade/" +
	"1inchusdt@trade/dydxusdt@trade/sushiusdt@trade/yfiusdt@trade/balusdt@trade/" +
	"renusdt@trade/kncusdt@trade/oceanusdt@trade/roseusdt@trade/celousdt@trade/" +
	// 161-200
	"cfxusdt@trade/kavausdt@trade/bandusdt@trade/sklusdt@trade/zenusdt@trade/" +
	"dashusdt@trade/xmrusdt@trade/zecusdt@trade/qtumusdt@trade/ontusdt@trade/" +
	"icxusdt@trade/wldusdt@trade/tiausdt@trade/seiusdt@trade/jtousdt@trade/" +
	"pythusdt@trade/jupusdt@trade/strkusdt@trade/mantausdt@trade/dymusdt@trade/" +
	"ethfiusdt@trade/enausdt@trade/wusdt@trade/iousdt@trade/zkusdt@trade/" +
	"listausdt@trade/renderusdt@trade/fetusdt@trade/agixusdt@trade/nmrusdt@trade/" +
	"magicusdt@trade/gmxusdt@trade/pendleusdt@trade/wifusdt@trade/bonkusdt@trade/" +
	"bomeusdt@trade/popcatusdt@trade/mewusdt@trade/radusdt@trade/cvpusdt@trade/" +
	// 201-250
	"notusdt@trade/dogsusdt@trade/hmstrusdt@trade/catiusdt@trade/eigenusdt@trade/" +
	"scrusdt@trade/neirousdt@trade/1000satsusdt@trade/ordiusdt@trade/luncusdt@trade/" +
	"ustcusdt@trade/blurusdt@trade/nfpusdt@trade/aiusdt@trade/xaiusdt@trade/" +
	"memeusdt@trade/aceusdt@trade/beamxusdt@trade/arkmust@trade/cyberusdt@trade/" +
	"hookusdt@trade/minausdt@trade/sfpusdt@trade/highusdt@trade/flowusdt@trade/" +
	"loomusdt@trade/iostusdt@trade/winusdt@trade/hotusdt@trade/celrusdt@trade/" +
	"rlcusdt@trade/ognusdt@trade/mtlusdt@trade/nknusdt@trade/tlmusdt@trade/" +
	"aliceusdt@trade/dentusdt@trade/ctsiusdt@trade/maskusdt@trade/slpusdt@trade/" +
	"mdtusdt@trade/polsusdt@trade/auctionusdt@trade/fluxusdt@trade/alphausdt@trade/" +
	"degousdt@trade/rareusdt@trade/linausdt@trade/idexusdt@trade/waxpusdt@trade/" +
	// 251-300
	"hardusdt@trade/belusdt@trade/cotiusdt@trade/funusdt@trade/gtcusdt@trade/" +
	"ilvusdt@trade/peopleusdt@trade/antusdt@trade/bakeusdt@trade/xvsusdt@trade/" +
	"chessusdt@trade/tkousdt@trade/phausdt@trade/dodousdt@trade/sxpusdt@trade/" +
	"unfiusdt@trade/ornusdt@trade/xvgusdt@trade/clvusdt@trade/qiusdt@trade/" +
	"portousdt@trade/santosusdt@trade/laziousdt@trade/cityusdt@trade/psgusdt@trade/" +
	"atmusdt@trade/ogusdt@trade/asrusdt@trade/acmusdt@trade/afausdt@trade"

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
	out := make(chan exchange.NormalizedPriceEvent, 4096)
	adapter := &exchange.BinanceAdapter{
		Endpoint: binanceEndpoint,
		Out:      out,
	}
	go adapter.Start(ctx)
	log.Println("[BG Worker] Binance feed connected (300 instruments)")

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
	log.Println("[BG Worker] mock feed running (300 instruments)")

	// build base prices from instruments list — unknown ones default to 1.0
	basePrices := map[int]float64{
		101: 60000, 102: 3000, 103: 300, 104: 0.5,
		105: 150, 106: 0.4, 107: 0.08, 108: 0.7,
		109: 80, 110: 7, 111: 35, 112: 15,
		113: 8, 114: 9, 115: 0.12, 116: 25,
		117: 5, 118: 10, 119: 12, 120: 1.5,
		121: 3.5, 122: 25, 123: 1.2, 124: 2.1, 125: 2.0,
		126: 0.18, 127: 95, 128: 3.2, 129: 2800, 130: 55,
		131: 2.1, 132: 28, 133: 0.55, 134: 6.5, 135: 0.18,
		136: 0.04, 137: 0.09, 138: 45, 139: 0.9, 140: 1.8,
		141: 7.5, 142: 0.45, 143: 0.4, 144: 0.25, 145: 0.08,
		146: 0.015, 147: 0.02, 148: 0.22, 149: 0.35, 150: 0.6,
		151: 0.38, 152: 2.1, 153: 1.1, 154: 6500, 155: 4.5,
		156: 0.06, 157: 0.7, 158: 0.9, 159: 0.09, 160: 0.75,
		161: 0.28, 162: 0.65, 163: 1.4, 164: 0.05, 165: 8.5,
		166: 32, 167: 155, 168: 28, 169: 3.2, 170: 0.3,
		171: 0.22, 172: 5.5, 173: 8.5, 174: 0.45, 175: 3.2,
		176: 0.38, 177: 0.85, 178: 0.55, 179: 1.8, 180: 3.5,
		181: 3.8, 182: 0.75, 183: 0.55, 184: 4.2, 185: 0.18,
		186: 0.022, 187: 7.5, 188: 1.6, 189: 0.8, 190: 0.9,
		191: 2.1, 192: 0.5, 193: 1.2, 194: 18, 195: 3.5,
		196: 2.8, 197: 0.00002, 198: 0.01, 199: 0.08, 200: 0.005,
	}
	// 201-300 default to 1.0 base — they'll get random walk
	for id := 201; id <= 300; id++ {
		if _, ok := basePrices[id]; !ok {
			basePrices[id] = 1.0
		}
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
