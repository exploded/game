package market

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/exploded/game/internal/db"
)

// Quarter is a calendar quarter. There's one game per quarter: it starts at
// midnight on the 1st of January, April, July or October and ends at midnight
// after the last day of March, June, September or December.
type Quarter struct {
	Year int
	N    int // 1 to 4
}

// QuarterOf is the quarter t falls in, by t's own date.
func QuarterOf(t time.Time) Quarter {
	return Quarter{Year: t.Year(), N: (int(t.Month())-1)/3 + 1}
}

// Next is the quarter after q.
func (q Quarter) Next() Quarter {
	if q.N == 4 {
		return Quarter{Year: q.Year + 1, N: 1}
	}
	return Quarter{Year: q.Year, N: q.N + 1}
}

// Key identifies the quarter's game in games.quarter: "2027-Q1".
func (q Quarter) Key() string { return fmt.Sprintf("%d-Q%d", q.Year, q.N) }

// Name is the game's default name: "Jan–Mar 2027". Not "Q1 2027", which in
// Australia often means July to September.
func (q Quarter) Name() string {
	first := q.first()
	return first.Format("Jan") + "–" + first.AddDate(0, 2, 0).Format("Jan 2006")
}

// StartDate is the quarter's first day, "2027-01-01".
func (q Quarter) StartDate() string { return q.first().Format(time.DateOnly) }

// EndDate is the quarter's last day, "2027-03-31". FinishExpiredGames ends
// the game once that day is over.
func (q Quarter) EndDate() string { return q.first().AddDate(0, 3, -1).Format(time.DateOnly) }

func (q Quarter) first() time.Time {
	return time.Date(q.Year, time.Month(q.N*3-2), 1, 0, 0, 0, 0, time.UTC)
}

// defaultGame is the settings for the first quarterly game. Later games copy
// the one before, so an admin's changes carry forward.
var defaultGame = db.Game{
	Description:          "Three months to grow $1,000,000. The biggest portfolio when the game ends wins.",
	Markets:              "both",
	StartingBalance:      100000000,
	BaseCurrency:         "AUD",
	ReferralBonusPct:     1,
	IsPublic:             1,
	PortfolioVisibility:  "public",
	CreditInterestRate:   525,
	LeverageInterestRate: 500,
	LimitOrders:          1,
}

// EnsureQuarterlyGames creates the games for now's quarter and the next one
// if they don't exist yet, so there's always a game running and the next one
// to join. The scheduler runs it every minute; now is in the game time zone.
func EnsureQuarterlyGames(ctx context.Context, q *db.Queries, now time.Time) {
	this := QuarterOf(now)
	for _, qtr := range []Quarter{this, this.Next()} {
		if err := ensureQuarterGame(ctx, q, qtr); err != nil {
			slog.Error("create quarterly game", "quarter", qtr.Key(), "error", err)
			return
		}
	}
}

func ensureQuarterGame(ctx context.Context, q *db.Queries, qtr Quarter) error {
	if _, err := q.GetGameByQuarter(ctx, sql.NullString{String: qtr.Key(), Valid: true}); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	owner, err := q.GetGameOwner(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // no users yet
	} else if err != nil {
		return err
	}
	prev, err := q.GetLatestQuarterlyGame(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		prev = defaultGame
	} else if err != nil {
		return err
	}
	g, err := q.CreateGame(ctx, db.CreateGameParams{
		CreatedBy:            owner,
		Name:                 qtr.Name(),
		Description:          prev.Description,
		Markets:              prev.Markets,
		StartingBalance:      prev.StartingBalance,
		BaseCurrency:         prev.BaseCurrency,
		MaxParticipants:      prev.MaxParticipants,
		StartDate:            qtr.StartDate(),
		EndDate:              qtr.EndDate(),
		AllowShort:           prev.AllowShort,
		TradeFee:             prev.TradeFee,
		ReferralBonusPct:     prev.ReferralBonusPct,
		IsPublic:             1,
		PortfolioVisibility:  prev.PortfolioVisibility,
		CreditInterestRate:   prev.CreditInterestRate,
		LeverageInterestRate: prev.LeverageInterestRate,
		MinStockPrice:        prev.MinStockPrice,
		MaxStockPrice:        prev.MaxStockPrice,
		MarginTrading:        prev.MarginTrading,
		LimitOrders:          prev.LimitOrders,
		StopLoss:             prev.StopLoss,
		FractionalShares:     prev.FractionalShares,
		Quarter:              sql.NullString{String: qtr.Key(), Valid: true},
	})
	if err != nil {
		return err
	}
	slog.Info("created quarterly game", "id", g.ID, "quarter", qtr.Key(), "start", g.StartDate)
	return nil
}
