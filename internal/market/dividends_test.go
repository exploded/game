package market

import (
	"context"
	"net/http"
	"testing"
)

// TestCheckDividends: MS went ex-dividend ($1.15) on Friday 30 October
// 2026, at 13:30 UTC, and the check ran late, on Monday. Holders at the open
// are paid, a short pays, shares bought after the open don't count, an AUD
// game is paid once a rate is stored, and a finished game isn't paid.
func TestCheckDividends(t *testing.T) {
	if NewYork == nil {
		t.Skip("no America/New_York timezone")
	}
	d, q := monthlyDB(t)
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token) VALUES
			(1, 'a@x', 'A', 't1'), (2, 'b@x', 'B', 't2'), (3, 'c@x', 'C', 't3'), (4, 'd@x', 'D', 't4'), (5, 'e@x', 'E', 't5')`,
		`INSERT INTO games (id, created_by, name, status, base_currency, start_date, end_date) VALUES
			(1, 1, 'USD game', 'active', 'USD', '2026-10-01', '2026-11-30'),
			(2, 1, 'AUD game', 'active', 'AUD', '2026-10-01', '2026-11-30'),
			(3, 1, 'Finished', 'finished', 'USD', '2026-09-01', '2026-09-30')`,
		`INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES
			(1, 1, 1, 0, 0), (2, 1, 2, 0, 0), (3, 1, 3, 0, 0), (4, 2, 4, 0, 0), (5, 3, 5, 0, 0)`,
		`INSERT INTO stocks (id, symbol, name, market, currency) VALUES
			(1, 'MS', 'Morgan Stanley', 'sp500', 'USD'), (2, 'KO', 'Coca-Cola', 'sp500', 'USD')`,
		// Player 2 is short. Player 3 bought 10 of their 30 after the open.
		// Player 1's 5 KO earn under half a cent.
		`INSERT INTO holdings (participant_id, stock_id, quantity, avg_cost, cost_basis) VALUES
			(1, 1, 100, 10000, 1000000), (2, 1, -40, 10000, -400000), (3, 1, 30, 10000, 300000),
			(4, 1, 100, 10000, 1000000), (5, 1, 100, 10000, 1000000), (1, 2, 5, 7000, 35000)`,
		`INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total, created_at) VALUES
			(3, 1, 'buy', 20, 10000, 200000, 200000, '2026-10-29 15:00:00'),
			(3, 1, 'buy', 10, 10000, 100000, 100000, '2026-10-30 15:00:00')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	body := `[
		{"symbol":"MS","mic_code":"XNYS","exchange":"NYSE","ex_date":"2026-10-30","amount":1.15},
		{"symbol":"KO","mic_code":"XNYS","exchange":"NYSE","ex_date":"2026-10-30","amount":0.0009},
		{"symbol":"MS","mic_code":"OTCM","exchange":"OTC","ex_date":"2026-10-29","amount":9},
		{"symbol":"MS","mic_code":"XNYS","exchange":"NYSE","ex_date":"2026-11-05","amount":1.15},
		{"symbol":"ZZZZ","mic_code":"XNYS","exchange":"NYSE","ex_date":"2026-10-30","amount":1}
	]`
	f := &Fetcher{q: q, rawDB: d, client: &http.Client{Transport: twelveDataStub(body)}}
	ctx := context.Background()

	cash := func(p int64) (c int64) {
		d.QueryRow(`SELECT cash_balance FROM participants WHERE id = ?`, p).Scan(&c)
		return
	}

	// No AUD/USD rate yet: MS can't be paid to the AUD game, so it's paid
	// to nobody and the check fails, to be tried again. KO is done.
	n, err := f.CheckDividends(ctx, q, "2026-11-02")
	if err == nil || n != 1 {
		t.Fatalf("without a rate: %d paid, %v; want 1 and an error", n, err)
	}
	if c := cash(1); c != 0 {
		t.Errorf("player 1 paid %d before MS could be paid to everyone", c)
	}

	if _, err := d.Exec(`INSERT INTO exchange_rates (date, rate_aud_usd) VALUES ('2026-10-30', 650000)`); err != nil {
		t.Fatal(err)
	}
	if n, err := f.CheckDividends(ctx, q, "2026-11-02"); err != nil || n != 1 {
		t.Fatalf("CheckDividends: %d paid, %v; want 1", n, err)
	}
	if n, err := f.CheckDividends(ctx, q, "2026-11-02"); err != nil || n != 0 {
		t.Fatalf("second check: %d paid, %v; want 0", n, err)
	}

	for _, c := range []struct{ player, want int64 }{
		{1, 11500}, // 100 × $1.15
		{2, -4600}, // short 40: pays it
		{3, 2300},  // 20 held at the open
		{4, 17692}, // $115 at 0.65
		{5, 0},     // finished game
	} {
		if got := cash(c.player); got != c.want {
			t.Errorf("player %d cash %d, want %d", c.player, got, c.want)
		}
	}
	var value int64
	d.QueryRow(`SELECT portfolio_value FROM participants WHERE id = 1`).Scan(&value)
	if value != 11500 {
		t.Errorf("player 1 portfolio value %d, want 11500", value)
	}

	var notes int
	d.QueryRow(`SELECT COUNT(*) FROM notifications WHERE type = 'dividend'`).Scan(&notes)
	if notes != 4 {
		t.Errorf("%d dividend notifications, want 4 (no KO, none for the finished game)", notes)
	}

	if total, _ := q.TotalDividends(ctx, 2); total != -4600 {
		t.Errorf("TotalDividends(2) = %d, want -4600", total)
	}
	rows, err := q.ListHistory(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	last := rows[len(rows)-1]
	if last.Kind != "dividend" || last.Quantity != 20 || last.Amount != 2300 || last.Day != "2026-10-30" || last.Symbol != "MS" {
		t.Errorf("player 3's last history row: %+v", last)
	}
}

func TestFmtPerShare(t *testing.T) {
	for in, want := range map[int64]string{
		176000:  "$0.176",
		1150000: "$1.15",
		1000000: "$1.00",
		749409:  "$0.7494",
		52000:   "$0.052",
	} {
		if got := FmtPerShare(in); got != want {
			t.Errorf("FmtPerShare(%d) = %q, want %q", in, got, want)
		}
	}
}
