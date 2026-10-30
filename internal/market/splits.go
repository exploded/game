package market

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/exploded/game/internal/db"
)

// splitsCalendarCost is what one US splits calendar request costs (measured
// with /api_usage on 30 October 2026). /splits for one symbol costs 20, so
// one calendar call a day is far cheaper than asking per stock.
const splitsCalendarCost = 40

// splitsLookback is how far back each check looks, so a split is still
// applied if the server missed the morning it happened.
const splitsLookback = 7 * 24 * time.Hour

// calendarSplit is one row of Twelve Data's /splits_calendar.
type calendarSplit struct {
	Date        string  `json:"date"`
	Symbol      string  `json:"symbol"`
	Exchange    string  `json:"exchange"`
	Description string  `json:"description"`
	FromFactor  float64 `json:"from_factor"` // "10-for-1" is from 10, to 1
	ToFactor    float64 `json:"to_factor"`
}

// CheckSplits applies every split in the last week of Twelve Data's US
// splits calendar to the stocks in the game, up to today (a New York date).
// Each is applied once (stock_splits). It returns how many it applied.
//
// A split for date D takes effect at that day's open. Run it before then
// and holdings, open orders and stored prices all change together, so
// nothing jumps. Run it late and only what came before the open is
// scaled: Twelve Data's later fetches, quotes and trades are already split.
func (f *Fetcher) CheckSplits(ctx context.Context, q *db.Queries, today string) (int, error) {
	day, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return 0, err
	}
	if err := takeCredits(ctx, splitsCalendarCost); err != nil {
		return 0, err
	}
	u := fmt.Sprintf("https://api.twelvedata.com/splits_calendar?country=%s&start_date=%s&end_date=%s&apikey=%s",
		url.QueryEscape("United States"), day.Add(-splitsLookback).Format(time.DateOnly), today, f.apiKey)
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
	var splits []calendarSplit
	if err := json.Unmarshal(raw, &splits); err != nil {
		// An error comes back as an object, not a list.
		var body map[string]json.RawMessage
		if json.Unmarshal(raw, &body) == nil {
			if err := requestError(body); err != nil {
				return 0, err
			}
		}
		return 0, fmt.Errorf("splits calendar: %w", err)
	}
	if len(splits) >= 100 {
		// The calendar stops at 100 rows; a week of US splits is usually
		// 40 or so. Some may be missing.
		slog.Warn("splits calendar full, some splits may be missing", "rows", len(splits))
	}

	applied := 0
	for _, s := range splits {
		if s.Date > today || s.FromFactor <= 0 || s.ToFactor <= 0 {
			continue
		}
		stock, err := q.GetStockBySymbol(ctx, db.GetStockBySymbolParams{Symbol: s.Symbol, Market: "sp500"})
		if err != nil {
			continue // not one of ours
		}
		ok, err := f.applySplit(ctx, q, stock, s)
		if err != nil {
			slog.Error("applying split", "symbol", s.Symbol, "date", s.Date, "error", err)
			continue
		}
		if ok {
			applied++
			slog.Info("stock split applied", "symbol", s.Symbol, "date", s.Date, "split", s.Description)
		}
	}
	return applied, nil
}

// splitOpen is when a split for date (YYYY-MM-DD) takes effect: 9:30 am in
// New York, as a stored UTC time ("2006-01-02 15:04:05").
func splitOpen(date string) string {
	loc := NewYork
	if loc == nil {
		loc = time.FixedZone("EST", -5*3600)
	}
	d, _ := time.ParseInLocation(time.DateOnly, date, loc)
	return d.Add(9*time.Hour + 30*time.Minute).UTC().Format(time.DateTime)
}

// applySplit applies one split in a transaction, unless it was applied
// before. It reports whether it applied it.
func (f *Fetcher) applySplit(ctx context.Context, q *db.Queries, stock db.Stock, s calendarSplit) (bool, error) {
	tx, err := f.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	n, err := qtx.RecordStockSplit(ctx, db.RecordStockSplitParams{
		StockID: stock.ID, Date: s.Date, FromFactor: s.FromFactor, ToFactor: s.ToFactor, Description: s.Description,
	})
	if err != nil || n == 0 {
		return false, err
	}
	shares := s.FromFactor / s.ToFactor // new shares per old share
	open := splitOpen(s.Date)

	if err := qtx.AdjustPricesForSplit(ctx, db.AdjustPricesForSplitParams{
		PriceFactor: 1 / shares, StockID: stock.ID, SplitDate: s.Date, TookEffect: open,
	}); err != nil {
		return false, err
	}

	// The price a fractional share is paid out at: the latest bar, now split.
	var price int64
	if latest, err := qtx.GetLatestPrice(ctx, stock.ID); err == nil {
		price = latest.Close
	}
	var rate int64
	if r, err := qtx.GetLatestExchangeRate(ctx); err == nil {
		rate = r.RateAudUsd
	}

	holdings, err := qtx.ListSplitHoldings(ctx, stock.ID)
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
		before := h.Quantity - since // held at the open, so split
		if before == 0 {
			continue
		}
		whole, frac := splitShares(before, shares)
		qty := whole + since
		cost := h.CostBasis
		var cash int64
		if frac != 0 {
			// A fraction left by a reverse split is paid out, like a sale:
			// its share of the cost goes with it.
			cash = int64(math.Round(frac * float64(price)))
			cost -= int64(math.Round(float64(h.CostBasis) * frac / (float64(whole) + frac + float64(since))))
		}
		if qty == 0 {
			err = qtx.DeleteHolding(ctx, db.DeleteHoldingParams{ParticipantID: h.ParticipantID, StockID: stock.ID})
		} else {
			err = qtx.UpsertHolding(ctx, db.UpsertHoldingParams{
				ParticipantID: h.ParticipantID, StockID: stock.ID, Quantity: qty,
				AvgCost: AvgCost(qty, cost), CostBasis: cost, CurrentValue: qty * price,
			})
		}
		if err != nil {
			return false, err
		}

		msg := fmt.Sprintf("%s had a %s on %s: your %d shares are now %d.", stock.Symbol, s.Description, s.Date, abs(before), abs(whole))
		if cash != 0 {
			if stock.Currency != h.BaseCurrency && rate == 0 {
				slog.Error("split cash unpaid: no exchange rate", "symbol", stock.Symbol, "participant", h.ParticipantID)
			} else {
				base := convertCurrency(cash, stock.Currency, h.BaseCurrency, rate)
				if err := qtx.CreditCash(ctx, db.CreditCashParams{Amount: base, ID: h.ParticipantID}); err != nil {
					return false, err
				}
				msg += " The part share left over was paid out as cash."
			}
		}
		if err := qtx.CreateNotification(ctx, db.CreateNotificationParams{
			UserID: h.UserID, GameID: sql.NullInt64{Int64: h.GameID, Valid: true},
			Type: "stock_split", Title: stock.Symbol + " split", Message: msg,
		}); err != nil {
			return false, err
		}
	}

	// Orders placed before the open are in old shares and prices.
	orders, err := qtx.ListOpenOrdersPlacedBefore(ctx, db.ListOpenOrdersPlacedBeforeParams{StockID: stock.ID, CreatedAt: open})
	if err != nil {
		return false, err
	}
	for _, o := range orders {
		qty, _ := splitShares(o.Quantity, shares)
		if qty == 0 {
			err = qtx.CancelLimitOrder(ctx, o.ID)
		} else {
			err = qtx.UpdateOrderForSplit(ctx, db.UpdateOrderForSplitParams{
				Quantity: qty, LimitPrice: int64(math.Round(float64(o.LimitPrice) / shares)), ID: o.ID,
			})
		}
		if err != nil {
			return false, err
		}
	}

	return true, tx.Commit()
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// splitShares turns n old shares into whole new ones and the fraction left
// over, both with n's sign (a short stays short).
func splitShares(n int64, shares float64) (whole int64, frac float64) {
	exact := float64(n) * shares
	if r := math.Round(exact); math.Abs(exact-r) < 1e-6 {
		return int64(r), 0
	}
	t := math.Trunc(exact)
	return int64(t), exact - t
}
