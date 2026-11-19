package market

// MaxTradeValue is the most one order can be worth, in cents of the stock's
// currency: $10 billion, ten times the largest starting balance an admin can
// set. Converting between currencies multiplies by 1,000,000, so without a cap
// a big enough order wraps int64 to a negative cost and adds cash instead of
// taking it (found in review, 19 November 2026). Under the cap that
// multiplication stays below 10^18.
const MaxTradeValue int64 = 1_000_000_000_000

// TradeValue is quantity × price in cents, and false when either isn't
// positive or the product is over MaxTradeValue. Every order and fill path
// checks it before converting currency or charging fees.
func TradeValue(quantity, price int64) (int64, bool) {
	if quantity <= 0 || price <= 0 || quantity > MaxTradeValue/price {
		return 0, false
	}
	return quantity * price, true
}
