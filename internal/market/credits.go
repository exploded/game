package market

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// creditsPerMinute is the Twelve Data credit budget shared by every request
// this process makes. The Venture plan allows 610 a minute; 600 leaves a little
// headroom.
//
// Costs per symbol: time_series 1, quote 1, logo 1, profile 10, statistics 50.
// The Api-Credits-Request response header under-reports profile and statistics,
// so don't trust it; /api_usage before and after a call shows the real cost.
const creditsPerMinute = 600

// creditWindow is a rolling window, not a clock minute. Twelve Data's minute
// doesn't line up with ours, so a limiter that resets on the clock minute can
// spend 600 at the end of one minute and 600 at the start of the next, and
// Twelve Data sees 1,200. A second of slack covers clock skew.
const creditWindow = 61 * time.Second

type creditSpend struct {
	at time.Time
	n  int
}

var credits struct {
	mu    sync.Mutex
	spent []creditSpend // oldest first, all within creditWindow
}

// takeCredits blocks until n credits are free in the rolling window, then
// spends them. The price fetcher, intraday quotes and profile refresh all go
// through here, so they can run at the same time without going over the plan.
func takeCredits(ctx context.Context, n int) error {
	for {
		credits.mu.Lock()
		now := time.Now()
		for len(credits.spent) > 0 && now.Sub(credits.spent[0].at) >= creditWindow {
			credits.spent = credits.spent[1:]
		}
		used := 0
		for _, s := range credits.spent {
			used += s.n
		}
		// A request bigger than the whole budget still goes through once the
		// window is empty rather than waiting forever.
		if used+n <= creditsPerMinute || used == 0 {
			credits.spent = append(credits.spent, creditSpend{at: now, n: n})
			credits.mu.Unlock()
			return nil
		}
		wait := credits.spent[0].at.Add(creditWindow).Sub(now)
		credits.mu.Unlock()

		slog.Debug("twelve data credit limit reached, waiting",
			"used", used, "want", n, "wait_ms", wait.Milliseconds())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
