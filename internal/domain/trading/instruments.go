package trading

type Instrument struct {
	ID     int
	Symbol string
}

var Instruments = []Instrument{
	{ID: 101, Symbol: "BTC_USDT"},
	{ID: 102, Symbol: "ETH_USDT"},
	{ID: 103, Symbol: "BNB_USDT"},
	{ID: 104, Symbol: "XRP_USDT"},
	{ID: 105, Symbol: "SOL_USDT"},
	{ID: 106, Symbol: "ADA_USDT"},
	{ID: 107, Symbol: "DOGE_USDT"},
	{ID: 108, Symbol: "MATIC_USDT"},
	{ID: 109, Symbol: "LTC_USDT"},
	{ID: 110, Symbol: "DOT_USDT"},
}

var SymbolToID map[string]int
var IDToSymbol map[int]string

func init() {
	SymbolToID = make(map[string]int)
	IDToSymbol = make(map[int]string)

	for _, inst := range Instruments {
		SymbolToID[inst.Symbol] = inst.ID
		IDToSymbol[inst.ID] = inst.Symbol
	}
}
