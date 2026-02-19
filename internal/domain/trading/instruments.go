package trading

type Instrument struct {
	ID     int
	Symbol string
}

var Instruments = []Instrument{
	{ID: 101, Symbol: "BTC_USDT"},
	{ID: 102, Symbol: "ETH_USDT"},
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
