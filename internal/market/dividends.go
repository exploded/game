package market

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/exploded/game/internal/db"
)

// dividendsCalendarCost is what one US dividends calendar request costs,
// whatever its outputsize (measured with /api_usage on 2 November 2026).
const dividendsCalendarCost = 40

// dividendsCalendarRows is the outputsize asked for. Without it the calendar
// stops at 100 rows; a week of US dividends is about 1,500, most of them
// funds and OTC listings.
const dividendsCalendarRows = 5000

// calendarDividend is one row of Twelve Data's /dividends_calendar.
type calendarDividend struct {
	Symbol   string  `json:"symbol"`
	Exchange string  `json:"exchange"`
	ExDate   string  `json:"ex_date"`
	Amount   float64 `json:"amount"` // a share, in the stock's currency
}

// errNoRate is returned when a dividend needs converting and no exchange
// rate is stored. The dividend is left unpaid, to be paid on the next check.
var errNoRate = errors.New("no exchange rate stored")

// CheckDividends pays every dividend in the last week of Twelve Data's US
// dividends calendar, up to today (a New York date), to players holding the
// stock at the open on its ex-date. A short pays the dividend instead. Each
// is paid once (stock_dividends). It returns how many it paid; a dividend
// that couldn't be paid makes it return an error too, so it's tried again.
//
// It's paid on the ex-date, when the price drops by about the dividend,
// rather than the payment date, which can fall after the game ends. Run
// before the open, it pays whoever holds the stock; run late, shares
// traded since the open don't count. It looks back a week, like
// CheckSplits, so a missed morning is made up.
func (f *Fetcher) CheckDividends(ctx context.Context, q *db.Queries, today string) (int, error) {
	day, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return 0, err
	}
	if err := takeCredits(ctx, dividendsCalendarCost); err != nil {
		return 0, err
	}
	u := fmt.Sprintf("https://api.twelvedata.com/dividends_calendar?country=%s&start_date=%s&end_date=%s&outputsize=%d",
		url.QueryEscape("United States"), day.Add(-splitsLookback).Format(time.DateOnly), today, dividendsCalendarRows)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return 0, err
	}
	var divs []calendarDividend
	if err := json.Unmarshal(raw, &divs); err != nil {
		// An error comes back as an object, not a list.
		var body map[string]json.RawMessage
		if json.Unmarshal(raw, &body) == nil {
			if err := requestError(body); err != nil {
				return 0, err
			}
		}
		return 0, fmt.Errorf("dividends calendar: %w", err)
	}
	if len(divs) >= dividendsCalendarRows {
		slog.Warn("dividends calendar full, some dividends may be missing", "rows", len(divs))
	}

	paid, failed := 0, 0
	for _, d := range divs {
		// OTC listings can share a symbol with nothing of ours, but skip
		// them in case one does.
		if d.ExDate > today || d.Amount <= 0 || d.Exchange == "OTC" {
			continue
		}
		stock, err := q.GetStockBySymbol(ctx, db.GetStockBySymbolParams{Symbol: d.Symbol, Market: "sp500"})
		if err != nil {
			continue // not one of ours
		}
		ok, err := f.payDividend(ctx, q, stock, d)
		if err != nil {
			slog.Error("paying dividend", "symbol", d.Symbol, "ex_date", d.ExDate, "error", err)
			failed++
			continue
		}
		if ok {
			paid++
			slog.Info("dividend paid", "symbol", d.Symbol, "ex_date", d.ExDate, "amount", d.Amount)
		}
	}
	if failed > 0 {
		return paid, fmt.Errorf("%d dividends not paid", failed)
	}
	return paid, nil
}

// dividendCents is what shares earn at perShare (× 1,000,000), in cents,
// rounded to the nearest cent. It's negative for a short.
func dividendCents(shares, perShare int64) int64 {
	return int64(math.Round(float64(shares) * float64(perShare) / 10000))
}

// FmtPerShare formats a dividend a share (× 1,000,000) as dollars, with at
// least 2 decimal places and up to 4: 176000 is "$0.176", 1150000 "$1.15".
func FmtPerShare(perShare int64) string {
	s := fmt.Sprintf("%.4f", float64(perShare)/1e6)
	for len(s) > 0 && s[len(s)-1] == '0' && s[len(s)-3] != '.' {
		s = s[:len(s)-1]
	}
	return "$" + s
}

// payDividend pays one dividend in a transaction, unless it was paid
// before. It reports whether it paid it.
func (f *Fetcher) payDividend(ctx context.Context, q *db.Queries, stock db.Stock, d calendarDividend) (bool, error) {
	tx, err := f.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	perShare := int64(math.Round(d.Amount * 1e6))
	n, err := qtx.RecordStockDividend(ctx, db.RecordStockDividendParams{
		StockID: stock.ID, ExDate: d.ExDate, PerShare: perShare,
	})
	if err != nil || n == 0 {
		return false, err
	}
	open := splitOpen(d.ExDate)

	var rate int64
	if r, err := qtx.GetLatestExchangeRate(ctx); err == nil {
		rate = r.RateAudUsd
	}

	holdings, err := qtx.ListRunningHoldings(ctx, stock.ID)
	if err != nil {
		return false, err
	}
	for _, h := range holdings {
		since, err := qtx.NetSharesTradedSince(ctx, db.NetSharesTradedSinceParams{
			ParticipantID: h.ParticipantID, StockID: stock.ID, CreatedAt: open,
		})
		if err != nil {
			return false, err
		}
		shares := h.Quantity - since // held at the open
		if shares == 0 {
			continue
		}
		cash := dividendCents(shares, perShare)
		if cash == 0 {
			continue
		}
		if stock.Currency != h.BaseCurrency && rate == 0 {
			return false, errNoRate
		}
		amount := convertCurrency(cash, stock.Currency, h.BaseCurrency, rate)
		if err := qtx.InsertDividendPayment(ctx, db.InsertDividendPaymentParams{
			ParticipantID: h.ParticipantID, StockID: stock.ID, ExDate: d.ExDate,
			Shares: shares, PerShare: perShare, Amount: amount,
		}); err != nil {
			return false, err
		}
		if err := qtx.CreditCash(ctx, db.CreditCashParams{Amount: amount, ID: h.ParticipantID}); err != nil {
			return false, err
		}

		each, total := FmtPerShare(perShare), fmtCentsSimple(abs(amount))
		if stock.Currency != h.BaseCurrency {
			each, total = each+" "+stock.Currency, total+" "+h.BaseCurrency
		}
		label := StockLabel(stock.Symbol, stock.Name)
		title := label + " paid a dividend"
		msg := fmt.Sprintf("%s paid a dividend of %s a share. You held %d shares, so %s was added to your cash.",
			label, each, shares, total)
		if shares < 0 {
			title = "You paid " + label + "'s dividend"
			msg = fmt.Sprintf("%s paid a dividend of %s a share. You were short %d shares, so you paid it: %s was taken from your cash.",
				label, each, -shares, total)
		}
		if err := qtx.CreateNotification(ctx, db.CreateNotificationParams{
			UserID: h.UserID, GameID: sql.NullInt64{Int64: h.GameID, Valid: true},
			Type: "dividend", Title: title, Message: msg,
		}); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}
