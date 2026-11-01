package market

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/db"
)

// rateHistoryDays is how many daily AUD/USD closes FetchExchangeRates loads
// when the table is empty: about a year, so trades and charts from that far
// back convert at the day's real rate.
const rateHistoryDays = 400

// FetchExchangeRates saves the latest AUD/USD rate, or a year of daily closes
// if none are stored yet. Twelve Data's daily bar for today has the current
// rate as its close, so calling this through the day keeps today's row fresh.
// It costs 1 credit.
func (f *Fetcher) FetchExchangeRates(ctx context.Context, q *db.Queries) error {
	size := 1
	if _, err := q.GetLatestExchangeRate(ctx); err != nil {
		size = rateHistoryDays
	}
	if err := takeCredits(ctx, 1); err != nil {
		return err
	}
	url := fmt.Sprintf("https://api.twelvedata.com/time_series?symbol=AUD/USD&interval=1day&outputsize=%d", size)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var body struct {
		Values []struct {
			Datetime string `json:"datetime"`
			Close    string `json:"close"`
		} `json:"values"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decoding AUD/USD: %w", err)
	}
	if body.Code != 0 {
		return fmt.Errorf("twelve data error %d: %s", body.Code, body.Message)
	}

	rates := make([]db.UpsertExchangeRateParams, 0, len(body.Values))
	for _, v := range body.Values {
		rate, err := parseRate(v.Close)
		if err != nil {
			return fmt.Errorf("AUD/USD on %s: %w", v.Datetime, err)
		}
		date := v.Datetime
		if len(date) > 10 {
			date = date[:10]
		}
		rates = append(rates, db.UpsertExchangeRateParams{Date: date, RateAudUsd: rate})
	}
	if len(rates) == 0 {
		return fmt.Errorf("no AUD/USD rate returned")
	}

	tx, err := f.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	txq := q.WithTx(tx)
	for _, r := range rates {
		if err := txq.UpsertExchangeRate(ctx, r); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	slog.Info("AUD/USD rate updated", "date", rates[0].Date, "rate", rates[0].RateAudUsd, "days", len(rates))
	return nil
}

// parseRate turns a rate such as "0.70268" into rate * 1,000,000. It rejects
// anything outside 0.2 to 2, which AUD/USD has never been near, so a bad
// response can't price every trade wrongly.
func parseRate(s string) (int64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if f < 0.2 || f > 2 {
		return 0, fmt.Errorf("rate %s out of range", s)
	}
	return int64(math.Round(f * 1000000)), nil
}
