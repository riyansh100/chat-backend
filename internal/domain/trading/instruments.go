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
	// added for stress testing
	{ID: 111, Symbol: "AVAX_USDT"},
	{ID: 112, Symbol: "LINK_USDT"},
	{ID: 113, Symbol: "UNI_USDT"},
	{ID: 114, Symbol: "ATOM_USDT"},
	{ID: 115, Symbol: "TRX_USDT"},
	{ID: 116, Symbol: "ETC_USDT"},
	{ID: 117, Symbol: "FIL_USDT"},
	{ID: 118, Symbol: "ICP_USDT"},
	{ID: 119, Symbol: "APT_USDT"},
	{ID: 120, Symbol: "ARB_USDT"},
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
