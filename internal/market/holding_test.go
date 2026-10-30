package market

import "testing"

func TestFill(t *testing.T) {
	for _, c := range []struct {
		name              string
		qty, cost         int64
		side              string
		n, price          int64
		wantQty, wantCost int64
		wantAvg           int64
	}{
		{"first buy", 0, 0, "buy", 10, 4567, 10, 45670, 4567},
		// 15.5c a share: an average kept in whole cents would lose $1,000.
		{"two buys at different prices", 100000, 1500000, "buy", 100000, 16, 200000, 3100000, 16},
		{"partial sale keeps the average", 200000, 3100000, "sell", 50000, 20, 150000, 2325000, 16},
		{"sell everything", 10, 45670, "sell", 10, 5000, 0, 0, 0},
		{"long to short", 5, 5000, "sell", 8, 900, -3, -2700, 900},
		{"open a short", 0, 0, "sell", 10, 1000, -10, -10000, 1000},
		{"add to a short", -10, -10000, "sell", 10, 1100, -20, -21000, 1050},
		{"cover part of a short", -10, -10000, "buy", 4, 900, -6, -6000, 1000},
		{"short to long", -10, -10000, "buy", 15, 900, 5, 4500, 900},
	} {
		t.Run(c.name, func(t *testing.T) {
			qty, cost := Fill(c.qty, c.cost, c.side, c.n, c.price)
			if qty != c.wantQty || cost != c.wantCost {
				t.Errorf("Fill = %d shares costing %d, want %d costing %d", qty, cost, c.wantQty, c.wantCost)
			}
			if avg := AvgCost(qty, cost); avg != c.wantAvg {
				t.Errorf("AvgCost = %d, want %d", avg, c.wantAvg)
			}
		})
	}
}
