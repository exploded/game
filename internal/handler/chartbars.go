package handler

import (
	"time"

	"github.com/exploded/game/internal/db"
)

// chartBars thins a daily price history for the stock chart: bars dated on or
// after cutoff (YYYY-MM-DD) stay daily, and older ones become one bar per ISO
// week. Twenty years of daily bars is about 5,000 points of inline JS; weekly
// bars before the last two years cut that to about 1,300 without changing
// what's stored. prices must be in ascending date order.
//
// A weekly bar takes the date and open of its first day, the close of its last
// day, the highest high, the lowest low and the total volume.
func chartBars(prices []db.StockPrice, cutoff string) []db.StockPrice {
	out := make([]db.StockPrice, 0, len(prices))
	var week db.StockPrice
	weekKey := -1
	for _, p := range prices {
		if p.Date >= cutoff {
			if weekKey >= 0 {
				out = append(out, week)
				weekKey = -1
			}
			out = append(out, p)
			continue
		}
		d, err := time.Parse("2006-01-02", p.Date)
		if err != nil {
			continue
		}
		y, w := d.ISOWeek()
		key := y*100 + w
		if key != weekKey {
			if weekKey >= 0 {
				out = append(out, week)
			}
			week, weekKey = p, key
			continue
		}
		week.High = max(week.High, p.High)
		week.Low = min(week.Low, p.Low)
		week.Close = p.Close
		week.Volume += p.Volume
	}
	if weekKey >= 0 {
		out = append(out, week)
	}
	return out
}
