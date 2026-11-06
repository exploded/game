package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	game "github.com/exploded/game"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/handler"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/market"
)

// jobsCmd times the background work that grows with players, on one
// connection and two CPUs, as in production. Some jobs change the copy
// (interest is paid, emails queued), so seed a fresh copy to time again.
func jobsCmd(args []string) error {
	fs := flag.NewFlagSet("jobs", flag.ExitOnError)
	dbPath := fs.String("db", "", "seeded copy of game.db (never the real one)")
	tz := fs.String("tz", "Australia/Sydney", "TIMEZONE, as in production")
	procs := fs.Int("procs", 2, "GOMAXPROCS: the Linode has 2 vCPUs")
	fs.Parse(args)
	if err := checkCopy(*dbPath); err != nil {
		return err
	}
	runtime.GOMAXPROCS(*procs)
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		return err
	}
	rawDB, err := handler.OpenDB(*dbPath, game.Schema)
	if err != nil {
		return err
	}
	defer rawDB.Close()
	ctx := context.Background()
	q := db.New(rawDB)
	now := time.Now()
	g, err := q.GetGameByMonth(ctx, sql.NullString{String: market.MonthOf(now.In(loc)).Key(), Valid: true})
	if err != nil {
		return fmt.Errorf("this month's game: %w", err)
	}

	var players, holdings, ranked int
	rawDB.QueryRow(`SELECT COUNT(*) FROM participants WHERE game_id = ?`, g.ID).Scan(&players)
	rawDB.QueryRow(`SELECT COUNT(*) FROM holdings h JOIN participants p ON p.id = h.participant_id WHERE p.game_id = ?`, g.ID).Scan(&holdings)
	board, _ := q.ListParticipantsByGame(ctx, g.ID)
	ranked = len(board)
	fmt.Printf("game %d: %d players, %d ranked, %d holdings, database %s\n\n", g.ID, players, ranked, holdings, fileSize(*dbPath))
	fmt.Printf("%-34s %5s %10s %10s\n", "job", "runs", "median", "max")

	timeIt := func(name string, runs int, fn func() error) {
		var d []time.Duration
		for range runs {
			start := time.Now()
			if err := fn(); err != nil {
				fmt.Printf("%-34s error: %v\n", name, err)
				return
			}
			d = append(d, time.Since(start))
		}
		slices.Sort(d)
		fmt.Printf("%-34s %5d %10s %10s\n", name, runs, round(d[len(d)/2]), round(d[len(d)-1]))
	}

	// Per request: the leaderboard query (dashboard, game home, leaderboard)
	// and a stock's whole history (every stock page).
	timeIt("leaderboard query (per page)", 5, func() error {
		_, err := q.ListParticipantsByGame(ctx, g.ID)
		return err
	})
	var aapl int64
	rawDB.QueryRow(`SELECT id FROM stocks WHERE symbol = 'AAPL'`).Scan(&aapl)
	timeIt("price history (per stock page)", 20, func() error {
		_, err := q.ListPriceHistory(ctx, db.ListPriceHistoryParams{StockID: aapl, Date: "2000-01-01", Date_2: now.UTC().Format(time.DateOnly)})
		return err
	})

	// Every 2 minutes in the US session.
	timeIt("process limit orders", 3, func() error {
		market.ProcessLimitOrders(ctx, q)
		return nil
	})
	timeIt("revalue portfolios", 3, func() error { return market.RevaluePortfolios(ctx, q) })

	// Once a day, just after midnight: today's interest, as if it's ended.
	tomorrow := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day()+1, 0, 5, 0, 0, loc)
	timeIt("pay interest (one transaction)", 1, func() error { return market.CreditInterest(ctx, q, rawDB, tomorrow) })

	// A game email to every player, queued as notify.sendGameEmail does.
	mailer, err := mail.New(q, mail.Log{}, mail.Config{BaseURL: "https://thewealthlab.au"})
	if err != nil {
		return err
	}
	queued := 0
	timeIt("queue a game email to everyone", 1, func() error {
		list, err := q.ListGamePlayers(ctx, g.ID)
		if err != nil {
			return err
		}
		for _, p := range list {
			you := map[string]any{"Name": p.UserName, "Value": "$1,000,000", "Return": "0.0%", "Players": ranked, "Ranked": p.Traded != 0, "Rank": 1, "Place": "1st"}
			data := map[string]any{"GameName": g.Name, "URL": fmt.Sprintf("/games/%d/leaderboard", g.ID), "EndDate": g.EndDate,
				"Starting": "$1,000,000", "Tomorrow": false, "InGame": len(list), "Top": []map[string]any{you}, "You": you}
			ok, err := mailer.ToUser(ctx, p.UserID, mail.Games, "game_ending", data)
			if err != nil {
				return err
			}
			if ok {
				queued++
			}
		}
		return nil
	})
	fmt.Printf("%-34s %d emails, %s to send at 10 a second\n", "", queued, (time.Duration(queued) * time.Second / 10).Round(time.Second))
	timeIt("prune old emails (hourly)", 1, func() error { return q.DeleteOldEmails(ctx) })

	// The daily backup's snapshot (backup.go uses its own connection).
	snap := filepath.Join(filepath.Dir(*dbPath), "snapshot-test.db")
	os.Remove(snap)
	timeIt("backup snapshot (VACUUM INTO)", 1, func() error {
		_, err := rawDB.ExecContext(ctx, "VACUUM INTO ?", snap)
		return err
	})
	fmt.Printf("%-34s snapshot %s\n", "", fileSize(snap))
	os.Remove(snap)
	return nil
}

func round(d time.Duration) time.Duration {
	switch {
	case d > 10*time.Second:
		return d.Round(100 * time.Millisecond)
	case d > 10*time.Millisecond:
		return d.Round(time.Millisecond)
	default:
		return d.Round(10 * time.Microsecond)
	}
}

func fileSize(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%.0f MB", float64(fi.Size())/1e6)
}
