package market

import (
	"context"

	"github.com/exploded/game/internal/db"
)

// ShortAllowed reports whether a player may hold newQty (negative: a short)
// of stock at price. A short puts its sale's proceeds in cash, so without a
// cap a player in a short-selling game could short without limit and buy
// with the cash. The cap is their portfolio value: every short they'd hold,
// valued at the last price in the game's currency, may add up to at most
// what they're worth. Only games with allow_short on reach it.
func ShortAllowed(ctx context.Context, q *db.Queries, p db.Participant, baseCurrency string, stock db.Stock, newQty, price int64) (bool, error) {
	if newQty >= 0 {
		return true, nil
	}
	var rate int64
	if r, err := q.GetLatestExchangeRate(ctx); err == nil {
		rate = r.RateAudUsd
	}
	conv := func(amount int64, currency string) (int64, bool) {
		if currency == baseCurrency {
			return amount, true
		}
		if rate == 0 {
			return 0, false
		}
		return convertCurrency(amount, currency, baseCurrency, rate), true
	}

	value, ok := TradeValue(-newQty, price)
	if !ok {
		return false, nil
	}
	exposure, ok := conv(value, stock.Currency)
	if !ok {
		return false, nil
	}
	holdings, err := q.ListHoldings(ctx, p.ID)
	if err != nil {
		return false, err
	}
	for _, h := range holdings {
		if h.StockID == stock.ID || h.Quantity >= 0 {
			continue
		}
		v, ok := conv(-h.CurrentValue, h.Currency)
		if !ok {
			return false, nil
		}
		exposure += v
	}
	return exposure <= max(p.PortfolioValue, 0), nil
}
