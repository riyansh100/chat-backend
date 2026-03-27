package trading

type Instrument struct {
	ID     int
	Symbol string
}

var Instruments = []Instrument{
	// ---- original 20 (101-120) ----
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

	// ---- extended 80 (121-200) ----
	{ID: 121, Symbol: "OP_USDT"},
	{ID: 122, Symbol: "INJ_USDT"},
	{ID: 123, Symbol: "SUI_USDT"},
	{ID: 124, Symbol: "STX_USDT"},
	{ID: 125, Symbol: "IMX_USDT"},
	{ID: 126, Symbol: "GRT_USDT"},
	{ID: 127, Symbol: "AAVE_USDT"},
	{ID: 128, Symbol: "SNX_USDT"},
	{ID: 129, Symbol: "MKR_USDT"},
	{ID: 130, Symbol: "COMP_USDT"},
	{ID: 131, Symbol: "LDO_USDT"},
	{ID: 132, Symbol: "RPL_USDT"},
	{ID: 133, Symbol: "FTM_USDT"},
	{ID: 134, Symbol: "NEAR_USDT"},
	{ID: 135, Symbol: "ALGO_USDT"},
	{ID: 136, Symbol: "VET_USDT"},
	{ID: 137, Symbol: "HBAR_USDT"},
	{ID: 138, Symbol: "EGLD_USDT"},
	{ID: 139, Symbol: "XTZ_USDT"},
	{ID: 140, Symbol: "THETA_USDT"},
	{ID: 141, Symbol: "AXS_USDT"},
	{ID: 142, Symbol: "SAND_USDT"},
	{ID: 143, Symbol: "MANA_USDT"},
	{ID: 144, Symbol: "ENJ_USDT"},
	{ID: 145, Symbol: "CHZ_USDT"},
	{ID: 146, Symbol: "ONE_USDT"},
	{ID: 147, Symbol: "ZIL_USDT"},
	{ID: 148, Symbol: "BAT_USDT"},
	{ID: 149, Symbol: "ZRX_USDT"},
	{ID: 150, Symbol: "CRV_USDT"},
	{ID: 151, Symbol: "1INCH_USDT"},
	{ID: 152, Symbol: "DYDX_USDT"},
	{ID: 153, Symbol: "PERP_USDT"},
	{ID: 154, Symbol: "SUSHI_USDT"},
	{ID: 155, Symbol: "YFI_USDT"},
	{ID: 156, Symbol: "BAL_USDT"},
	{ID: 157, Symbol: "REN_USDT"},
	{ID: 158, Symbol: "BNT_USDT"},
	{ID: 159, Symbol: "KNC_USDT"},
	{ID: 160, Symbol: "OCEAN_USDT"},
	{ID: 161, Symbol: "ROSE_USDT"},
	{ID: 162, Symbol: "CELO_USDT"},
	{ID: 163, Symbol: "CFX_USDT"},
	{ID: 164, Symbol: "KAVA_USDT"},
	{ID: 165, Symbol: "BAND_USDT"},
	{ID: 166, Symbol: "SKL_USDT"},
	{ID: 167, Symbol: "CKB_USDT"},
	{ID: 168, Symbol: "SC_USDT"},
	{ID: 169, Symbol: "ZEN_USDT"},
	{ID: 170, Symbol: "DASH_USDT"},
	{ID: 171, Symbol: "XMR_USDT"},
	{ID: 172, Symbol: "DCR_USDT"},
	{ID: 173, Symbol: "ZEC_USDT"},
	{ID: 174, Symbol: "QTUM_USDT"},
	{ID: 175, Symbol: "ONT_USDT"},
	{ID: 176, Symbol: "ICX_USDT"},
	{ID: 177, Symbol: "WAN_USDT"},
	{ID: 178, Symbol: "STEEM_USDT"},
	{ID: 179, Symbol: "LSK_USDT"},
	{ID: 180, Symbol: "ARK_USDT"},
	{ID: 181, Symbol: "WLD_USDT"},
	{ID: 182, Symbol: "TIA_USDT"},
	{ID: 183, Symbol: "SEI_USDT"},
	{ID: 184, Symbol: "JTO_USDT"},
	{ID: 185, Symbol: "PYTH_USDT"},
	{ID: 186, Symbol: "JUP_USDT"},
	{ID: 187, Symbol: "STRK_USDT"},
	{ID: 188, Symbol: "MANTA_USDT"},
	{ID: 189, Symbol: "ALT_USDT"},
	{ID: 190, Symbol: "PIXEL_USDT"},
	{ID: 191, Symbol: "PORTAL_USDT"},
	{ID: 192, Symbol: "DYM_USDT"},
	{ID: 193, Symbol: "ETHFI_USDT"},
	{ID: 194, Symbol: "ENA_USDT"},
	{ID: 195, Symbol: "W_USDT"},
	{ID: 196, Symbol: "IO_USDT"},
	{ID: 197, Symbol: "ZK_USDT"},
	{ID: 198, Symbol: "LISTA_USDT"},
	{ID: 199, Symbol: "RENDER_USDT"},
	{ID: 200, Symbol: "FET_USDT"},
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
