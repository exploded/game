package market

import "github.com/exploded/game/internal/db"

// TradeFees is what a trade costs on top of its value, in the game's base
// currency: the game's flat trade fee, plus its currency fee (fx_fee, basis
// points of the trade's value, rounded to the nearest cent) when the stock is
// priced in another currency. value is the trade's value already converted to
// the base currency.
func TradeFees(game db.Game, stockCurrency string, value int64) int64 {
	fee := game.TradeFee
	if stockCurrency != game.BaseCurrency && game.FxFee > 0 {
		fee += (value*game.FxFee + 5000) / 10000
	}
	return fee
}
