package trading

var PriceUpdateChan = make(chan PriceUpdateEvent, 1024)
