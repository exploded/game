package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	mrand "math/rand/v2"
	"path/filepath"
	"testing"
	"time"

	game "github.com/exploded/game"
	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/handler"
	"github.com/exploded/game/internal/market"
)

// Made-up players must look like real ones to the app: cash, trades and
// holdings that agree, and sessions the CSRF check accepts. Otherwise the
// load test measures a site no player would see.
func TestSeedPlayersAddUp(t *testing.T) {
	ctx := t.Context()
	rawDB, err := handler.OpenDB(filepath.Join(t.TempDir(), "game.db"), game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	q := db.New(rawDB)
	market.SeedStocks(ctx, q)
	if _, err := rawDB.Exec(`INSERT INTO stock_prices (stock_id, date, open, high, low, close)
		SELECT id, '2026-11-05', 10000 + id, 10000 + id, 10000 + id, 10000 + id FROM stocks`); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.Exec(`INSERT INTO exchange_rates (date, rate_aud_usd) VALUES ('2026-11-05', 650000)`); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Australia/Sydney")
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, loc)

	const n = 60
	p, err := seedPlayers(ctx, rawDB, loc, now, n, mrand.New(mrand.NewPCG(1, 2)))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Players) != n {
		t.Fatalf("got %d players, want %d", len(p.Players), n)
	}

	secret, err := auth.LoadCSRFSecret(rawDB)
	if err != nil {
		t.Fatal(err)
	}
	traders := 0
	for _, pl := range p.Players {
		var sessions int
		rawDB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ? AND user_id = ?`, pl.Session, pl.UserID).Scan(&sessions)
		if sessions != 1 {
			t.Fatalf("user %d: no session %s", pl.UserID, pl.Session)
		}
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(pl.Session))
		if pl.CSRF != hex.EncodeToString(mac.Sum(nil)) {
			t.Fatalf("user %d: CSRF token doesn't match the session", pl.UserID)
		}
		if pl.Trader {
			traders++
		}
	}
	if traders == 0 || traders == n {
		t.Fatalf("%d of %d players trade; want a mix", traders, n)
	}

	// Starting cash = cash left + everything the trades took.
	rows, err := rawDB.Query(`SELECT p.id, g.starting_balance, p.cash_balance, p.portfolio_value,
			(SELECT COALESCE(SUM(converted_total + fee), 0) FROM transactions WHERE participant_id = p.id),
			(SELECT COALESCE(SUM(converted_total), 0) FROM transactions WHERE participant_id = p.id)
		FROM participants p JOIN games g ON g.id = p.game_id WHERE p.game_id = ?`, p.GameID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	players := 0
	for rows.Next() {
		var id, start, cash, value, spent, bought int64
		if err := rows.Scan(&id, &start, &cash, &value, &spent, &bought); err != nil {
			t.Fatal(err)
		}
		players++
		if start != cash+spent {
			t.Errorf("participant %d: starting %d != cash %d + spent %d", id, start, cash, spent)
		}
		if value != cash+bought {
			t.Errorf("participant %d: value %d != cash %d + holdings bought %d", id, value, cash, bought)
		}
		if cash < 0 {
			t.Errorf("participant %d: cash %d below zero", id, cash)
		}
	}
	if players != n {
		t.Fatalf("%d participants in the game, want %d", players, n)
	}

	// Each holding is what its trades bought, at a cost basis market.Fill
	// would give.
	var bad int
	rawDB.QueryRow(`SELECT COUNT(*) FROM holdings h
		WHERE h.cost_basis != h.quantity * h.avg_cost
		   OR h.quantity != (SELECT SUM(quantity) FROM transactions t WHERE t.participant_id = h.participant_id AND t.stock_id = h.stock_id)`).Scan(&bad)
	if bad != 0 {
		t.Errorf("%d holdings don't match their trades", bad)
	}
}
