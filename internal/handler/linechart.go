package handler

import (
	"strconv"
	"time"

	"github.com/exploded/game/internal/db"
)

// The stock page draws a line chart on phones (static/js/stockchart.js) and
// candles on wider screens. The line's data is written into the page, one
// series per range chip; the candles fetch theirs from StockBars.

// lineRanges are the chart's range chips, in months back from the last
// price; 0 is the whole history.
var lineRanges = []int{1, 6, 12, 60, 0}

// lineMaxPoints is the most points a range's line keeps. A phone is about
// 360px wide, so more can't be told apart.
const lineMaxPoints = 260

// lineRange is one range of the line chart: dates and closes in cents.
type lineRange struct {
	D []string `json:"d"`
	C []int64  `json:"c"`
}

// lineSeries builds the line chart's ranges from a stock's daily prices, in
// ascending date order, keyed by months as text ("12"; "0" for all). A range
// that starts before the first price is left out, which hides its chip.
// Each range is thinned to lineMaxPoints by stride, keeping its first and
// last points. No prices give nil.
func lineSeries(prices []db.StockPrice) map[string]lineRange {
	if len(prices) == 0 {
		return nil
	}
	last, err := time.Parse("2006-01-02", prices[len(prices)-1].Date)
	if err != nil {
		return nil
	}
	first := prices[0].Date
	out := make(map[string]lineRange, len(lineRanges))
	for _, months := range lineRanges {
		from := first
		if months > 0 {
			// The same date as monthsBack in stockchart.js.
			from = last.AddDate(0, -months, 0).Format("2006-01-02")
			if from < first {
				continue
			}
		}
		start := len(prices) - 1
		for start > 0 && prices[start-1].Date >= from {
			start--
		}
		out[strconv.Itoa(months)] = thinLine(prices[start:])
	}
	return out
}

// thinLine keeps every nth price so at most lineMaxPoints remain, always
// keeping the first and last.
func thinLine(prices []db.StockPrice) lineRange {
	step := (len(prices) + lineMaxPoints - 2) / (lineMaxPoints - 1)
	step = max(step, 1)
	var r lineRange
	for i := 0; i < len(prices); i += step {
		r.D = append(r.D, prices[i].Date)
		r.C = append(r.C, prices[i].Close)
	}
	if n := len(prices) - 1; (n % step) != 0 {
		r.D = append(r.D, prices[n].Date)
		r.C = append(r.C, prices[n].Close)
	}
	return r
}

// candleBar is one bar of the wide-screen candle chart, prices in cents.
type candleBar struct {
	T string `json:"t"`
	O int64  `json:"o"`
	H int64  `json:"h"`
	L int64  `json:"l"`
	C int64  `json:"c"`
	V int64  `json:"v"`
}
