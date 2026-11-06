package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	mrand "math/rand/v2"
	"os"
	"time"

	game "github.com/exploded/game"
	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/handler"
	"github.com/exploded/game/internal/market"
)

// The made-up month. Shares come from the production game on 6 November
// 2026 and what a fantasy share game usually sees: most players trade a few
// times and some never do.
const (
	traderShare     = 0.6  // players with holdings; the rest sit on their cash
	limitOrderShare = 0.15 // traders with open limit orders
	aiShare         = 0.05 // traders whose trades came through their AI (/mcp)
	popularShare    = 0.5  // holdings picked from Rob's Picks and the Mag 7
	lobbyMessages   = 2000
)

var mag7 = []string{"AAPL", "MSFT", "NVDA", "GOOGL", "AMZN", "META", "TSLA"}

// Players is what seed writes for run: this month's game, the stocks and
// videos to visit, and each made-up player's session.
type Players struct {
	GameID  int64    `json:"game_id"`
	Stocks  []int64  `json:"stocks"`
	Videos  []int64  `json:"videos"`
	Players []Player `json:"players"`
}

// Player is one made-up player's session cookie and CSRF token.
type Player struct {
	UserID  int64  `json:"user_id"`
	Session string `json:"session"`
	CSRF    string `json:"csrf"`
	Trader  bool   `json:"trader"`
}

func seedCmd(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	dbPath := fs.String("db", "", "copy of game.db to add players to (never the real one)")
	n := fs.Int("players", 1000, "made-up players to add")
	out := fs.String("out", "players.json", "where to write the players' sessions for run")
	tz := fs.String("tz", "Australia/Sydney", "TIMEZONE, as in production")
	fs.Parse(args)
	if err := checkCopy(*dbPath); err != nil {
		return err
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		return err
	}
	rawDB, err := handler.OpenDB(*dbPath, game.Schema)
	if err != nil {
		return err
	}
	defer rawDB.Close()

	start := time.Now()
	p, err := seedPlayers(context.Background(), rawDB, loc, time.Now(), *n, mrand.New(mrand.NewPCG(1, 2)))
	if err != nil {
		return err
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(p); err != nil {
		return err
	}
	fmt.Printf("added %d players to game %d in %s; sessions in %s\n", *n, p.GameID, time.Since(start).Round(time.Millisecond), *out)
	return nil
}

type seedStock struct {
	id       int64
	symbol   string
	currency string
	close    int64
}

// seedPlayers adds n made-up players to this month's game, each with a
// session, and a month of what players leave behind: holdings and the
// trades that bought them, open orders, daily snapshots and interest,
// notifications, watchlists and lobby messages. Cash, holdings and cost
// basis agree, as market.Fill would leave them.
func seedPlayers(ctx context.Context, rawDB *sql.DB, loc *time.Location, now time.Time, n int, r *mrand.Rand) (*Players, error) {
	q := db.New(rawDB)
	stocks, popular, err := seedStocks(ctx, rawDB)
	if err != nil {
		return nil, err
	}
	rate, err := q.GetLatestExchangeRate(ctx)
	if err != nil {
		return nil, fmt.Errorf("no AUD/USD rate in the copy: %w", err)
	}
	secret, err := auth.LoadCSRFSecret(rawDB)
	if err != nil {
		return nil, err
	}
	run := now.Unix() // keeps a second seed of the same copy from clashing on emails

	// Users first: the monthly game needs an owner.
	users := make([]int64, n)
	if err := inTx(ctx, rawDB, func(tx *sql.Tx) error {
		ins, err := tx.PrepareContext(ctx, `INSERT INTO users (email, name, email_token, created_at, last_login, last_seen, age_confirmed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		for i := range n {
			joined := sqlTime(now.Add(-time.Duration(r.Int64N(int64(30 * 24 * time.Hour)))))
			res, err := ins.ExecContext(ctx, fmt.Sprintf("loadtest-%d-%d@example.invalid", run, i), fmt.Sprintf("Test Player %d", i),
				randHex(16), joined, sqlTime(now), sqlTime(now), joined)
			if err != nil {
				return err
			}
			users[i], _ = res.LastInsertId()
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}

	market.EnsureMonthlyGames(ctx, q, now.In(loc))
	g, err := q.GetGameByMonth(ctx, sql.NullString{String: market.MonthOf(now.In(loc)).Key(), Valid: true})
	if err != nil {
		return nil, fmt.Errorf("this month's game: %w", err)
	}
	gameStart, err := time.ParseInLocation(time.DateOnly, g.StartDate, loc)
	if err != nil {
		return nil, err
	}
	if gameStart.After(now) {
		return nil, errors.New("this month's game hasn't started")
	}

	out := &Players{GameID: g.ID, Players: make([]Player, n)}
	for _, s := range stocks {
		out.Stocks = append(out.Stocks, s.id)
	}
	rows, err := rawDB.QueryContext(ctx, `SELECT id FROM videos WHERE hidden = 0 ORDER BY id LIMIT 500`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		out.Videos = append(out.Videos, id)
	}
	rows.Close()

	err = inTx(ctx, rawDB, func(tx *sql.Tx) error {
		st, err := prepareSeed(ctx, tx)
		if err != nil {
			return err
		}
		for i, userID := range users {
			joined := gameStart.Add(time.Duration(r.Int64N(int64(now.Sub(gameStart)) + 1)))
			trader := r.Float64() < traderShare
			res, err := st.participant.ExecContext(ctx, g.ID, userID, g.StartingBalance, g.StartingBalance, sqlTime(joined))
			if err != nil {
				return err
			}
			pid, _ := res.LastInsertId()

			cash, value := g.StartingBalance, g.StartingBalance
			if trader {
				source := ""
				if r.Float64() < aiShare {
					source = "mcp"
				}
				// Spend 60–95% of the cash across 5–15 stocks.
				picked := pickStocks(r, stocks, popular, 5+r.IntN(11))
				budget := g.StartingBalance * int64(60+r.IntN(36)) / 100
				var holdingsValue int64
				for _, s := range picked {
					target := budget / int64(len(picked)) // in the game's currency
					native := target
					if s.currency != g.BaseCurrency {
						native = convert(target, g.BaseCurrency, s.currency, rate.RateAudUsd)
					}
					qty := max(native/s.close, 1)
					total := qty * s.close
					converted := total
					if s.currency != g.BaseCurrency {
						converted = convert(total, s.currency, g.BaseCurrency, rate.RateAudUsd)
					}
					fee := market.TradeFees(g, s.currency, converted)
					cash -= converted + fee
					holdingsValue += converted

					// One buy, or two at the same price. The last takes what's
					// left of the converted total and fee, so the trades add
					// up to the cash taken to the cent.
					buys := []int64{qty}
					if qty > 1 && r.Float64() < 0.3 {
						buys = []int64{qty / 2, qty - qty/2}
					}
					leftC, leftF := converted, fee
					for i, bq := range buys {
						at := sqlTime(joined.Add(time.Duration(r.Int64N(int64(now.Sub(joined)) + 1))))
						bt := bq * s.close
						bc, bf := converted*bq/qty, fee*bq/qty
						if i == len(buys)-1 {
							bc, bf = leftC, leftF
						}
						leftC, leftF = leftC-bc, leftF-bf
						if _, err := st.trade.ExecContext(ctx, pid, s.id, bq, s.close, bt, bc, rate.RateAudUsd, bf, at, source); err != nil {
							return err
						}
						if _, err := st.activity.ExecContext(ctx, g.ID, userID, fmt.Sprintf("bought %d shares of %s", bq, s.symbol), s.id, at, source); err != nil {
							return err
						}
					}
					if _, err := st.holding.ExecContext(ctx, pid, s.id, qty, s.close, total, total, sqlTime(now)); err != nil {
						return err
					}
				}
				value = cash + holdingsValue
				if _, err := st.cash.ExecContext(ctx, cash, value, pid); err != nil {
					return err
				}
				if r.Float64() < limitOrderShare {
					for range 1 + r.IntN(3) {
						s := stocks[r.IntN(len(stocks))]
						if _, err := st.order.ExecContext(ctx, pid, s.id, 1+r.IntN(20), s.close*9/10, g.EndDate, source); err != nil {
							return err
						}
					}
				}
				for range 3 {
					st.watch.ExecContext(ctx, userID, stocks[r.IntN(len(stocks))].id)
				}
				for range 2 {
					if _, err := st.notify.ExecContext(ctx, userID, g.ID, r.IntN(2), sqlTime(joined)); err != nil {
						return err
					}
				}
			}

			// A snapshot for each UTC day so far, as RevaluePortfolios takes
			// them, and interest for each local day that's ended.
			for d := joined.UTC(); !d.After(now.UTC()); d = d.AddDate(0, 0, 1) {
				if _, err := st.snapshot.ExecContext(ctx, pid, d.Format(time.DateOnly), cash, value-cash, value); err != nil {
					return err
				}
			}
			today := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
			for d := joined.In(loc).AddDate(0, 0, 1); d.Before(today); d = d.AddDate(0, 0, 1) {
				day := d.AddDate(0, 0, -1).Format(time.DateOnly)
				if _, err := st.interest.ExecContext(ctx, pid, day, cash, g.CreditInterestRate, market.DailyInterest(cash, g.CreditInterestRate)); err != nil {
					return err
				}
			}

			session := randHex(32)
			if _, err := st.session.ExecContext(ctx, session, userID, sqlTime(now.Add(auth.SessionDuration))); err != nil {
				return err
			}
			mac := hmac.New(sha256.New, secret)
			mac.Write([]byte(session))
			out.Players[i] = Player{UserID: userID, Session: session, CSRF: hex.EncodeToString(mac.Sum(nil)), Trader: trader}
		}

		for i := range min(lobbyMessages, 2*n) {
			blocked := ""
			if i%100 == 0 {
				blocked = "made-up blocked message"
			}
			at := sqlTime(now.Add(-time.Duration(r.Int64N(int64(30 * 24 * time.Hour)))))
			if _, err := st.lobby.ExecContext(ctx, users[r.IntN(n)], lobbyLines[i%len(lobbyLines)], at, blocked); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("players: %w", err)
	}
	return out, nil
}

var lobbyLines = []string{
	"Anyone else holding NVDA through earnings?",
	"Went heavy on banks this month, let's see how that goes.",
	"How does the interest on cash work?",
	"Just made my first trade!",
	"Rob's video on dividends was really useful.",
}

type seedStmts struct {
	participant, cash, trade, activity, holding, order, watch, notify, snapshot, interest, session, lobby *sql.Stmt
}

func prepareSeed(ctx context.Context, tx *sql.Tx) (*seedStmts, error) {
	var s seedStmts
	for _, p := range []struct {
		dst **sql.Stmt
		sql string
	}{
		{&s.participant, `INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value, joined_at) VALUES (?, ?, ?, ?, ?)`},
		{&s.cash, `UPDATE participants SET cash_balance = ?, portfolio_value = ? WHERE id = ?`},
		{&s.trade, `INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total, exchange_rate_used, fee, created_at, source)
			VALUES (?, ?, 'buy', ?, ?, ?, ?, ?, ?, ?, ?)`},
		{&s.activity, `INSERT INTO activity_feed (game_id, user_id, action, detail, stock_id, created_at, source) VALUES (?, ?, 'buy', ?, ?, ?, ?)`},
		{&s.holding, `INSERT INTO holdings (participant_id, stock_id, quantity, avg_cost, cost_basis, current_value, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`},
		{&s.order, `INSERT INTO limit_orders (participant_id, stock_id, type, quantity, limit_price, expires_at, source) VALUES (?, ?, 'buy', ?, ?, ?, ?)`},
		{&s.watch, `INSERT OR IGNORE INTO watchlist (user_id, stock_id) VALUES (?, ?)`},
		{&s.notify, `INSERT INTO notifications (user_id, game_id, type, title, message, is_read, created_at, emailed)
			VALUES (?, ?, 'order_filled', 'Order filled', 'Your order was filled.', ?, ?, 1)`},
		{&s.snapshot, `INSERT INTO portfolio_snapshots (participant_id, date, cash_balance, holdings_value, total_value) VALUES (?, ?, ?, ?, ?)`},
		{&s.interest, `INSERT INTO interest_payments (participant_id, date, balance, rate, amount) VALUES (?, ?, ?, ?, ?)`},
		{&s.session, `INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`},
		{&s.lobby, `INSERT INTO lobby_messages (user_id, message, created_at, blocked_reason) VALUES (?, ?, ?, ?)`},
	} {
		stmt, err := tx.PrepareContext(ctx, p.sql)
		if err != nil {
			return nil, err
		}
		*p.dst = stmt
	}
	return &s, nil
}

// seedStocks lists the active stocks that have a price, and which of them
// are popular: Rob's Picks and the Mag 7.
func seedStocks(ctx context.Context, rawDB *sql.DB) (all, popular []seedStock, err error) {
	rows, err := rawDB.QueryContext(ctx, `SELECT s.id, s.symbol, s.currency, p.close,
			s.symbol IN ('`+joinQuoted(mag7)+`') OR EXISTS (SELECT 1 FROM stock_picks k WHERE k.stock_id = s.id)
		FROM stocks s JOIN stock_prices p ON p.stock_id = s.id
		WHERE s.is_active = 1 AND p.close > 0
		  AND p.date = (SELECT MAX(date) FROM stock_prices WHERE stock_id = s.id)
		ORDER BY s.id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s seedStock
		var pop bool
		if err := rows.Scan(&s.id, &s.symbol, &s.currency, &s.close, &pop); err != nil {
			return nil, nil, err
		}
		all = append(all, s)
		if pop {
			popular = append(popular, s)
		}
	}
	if len(all) < 20 {
		return nil, nil, fmt.Errorf("only %d stocks have a price; seed a copy with real prices", len(all))
	}
	if len(popular) == 0 {
		popular = all[:10]
	}
	return all, popular, rows.Err()
}

// pickStocks picks n different stocks, about half of them popular ones.
func pickStocks(r *mrand.Rand, all, popular []seedStock, n int) []seedStock {
	seen := map[int64]bool{}
	var out []seedStock
	for tries := 0; len(out) < n && tries < 10*n; tries++ {
		from := all
		if r.Float64() < popularShare {
			from = popular
		}
		s := from[r.IntN(len(from))]
		if !seen[s.id] {
			seen[s.id] = true
			out = append(out, s)
		}
	}
	return out
}

func inTx(ctx context.Context, rawDB *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := rawDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// sqlTime is SQLite's datetime('now') format, in UTC.
func sqlTime(t time.Time) string { return t.UTC().Format(time.DateTime) }

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func joinQuoted(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += "','"
		}
		out += v
	}
	return out
}

// convert is market's convertCurrency (valuator.go): rate is AUD/USD × 1,000,000.
func convert(amount int64, from, to string, rate int64) int64 {
	switch {
	case from == to:
		return amount
	case from == "AUD" && to == "USD":
		return amount * rate / 1000000
	case from == "USD" && to == "AUD":
		return amount * 1000000 / rate
	}
	return amount
}
