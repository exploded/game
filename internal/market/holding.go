package market

// Fill is a holding after a trade of n shares at price. qty and cost are the
// holding before it: cost is the total paid for the shares held, in the
// stock's own currency, and is negative for a short (what the sale raised).
//
// Buying adds to a long holding's cost and selling takes away its share of
// it, so the average price paid never changes on a sale. A short works the
// same way the other way round. A trade that crosses zero, long to short or
// short to long, starts the new position at price.
func Fill(qty, cost int64, side string, n, price int64) (newQty, newCost int64) {
	if side == "sell" {
		n = -n
	}
	newQty = qty + n
	switch {
	case newQty == 0:
		return 0, 0
	case qty == 0 || (qty > 0) == (n > 0):
		// Opening or adding to a position.
		return newQty, cost + n*price
	case (qty > 0) == (newQty > 0):
		// Closing part of it.
		return newQty, divRound(cost*newQty, qty)
	}
	// Closed it and opened the other way.
	return newQty, newQty * price
}

// AvgCost is the average price per share of a holding, rounded to the
// nearest cent: positive for a short too.
func AvgCost(qty, cost int64) int64 {
	if qty == 0 {
		return 0
	}
	return divRound(cost, qty)
}

// divRound is a/b rounded to the nearest whole number, halves away from zero.
func divRound(a, b int64) int64 {
	if b < 0 {
		a, b = -a, -b
	}
	if a < 0 {
		return -((-a + b/2) / b)
	}
	return (a + b/2) / b
}
