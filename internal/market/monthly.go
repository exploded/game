package market

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/exploded/game/internal/db"
)

// Month is a calendar month. There's one game per month: it starts at
// midnight on the 1st and ends at midnight after the month's last day.
type Month struct {
	Year int
	M    time.Month
}

// MonthOf is the month t falls in, by t's own date.
func MonthOf(t time.Time) Month { return Month{Year: t.Year(), M: t.Month()} }

// Next is the month after m.
func (m Month) Next() Month {
	n := m.first().AddDate(0, 1, 0)
	return Month{Year: n.Year(), M: n.Month()}
}

// Key identifies the month's game in games.quarter: "2026-11".
func (m Month) Key() string { return m.first().Format("2006-01") }

// Name is the game's default name: "November 2026".
func (m Month) Name() string { return m.first().Format("January 2006") }

// StartDate is the month's first day, "2026-11-01".
func (m Month) StartDate() string { return m.first().Format(time.DateOnly) }

// EndDate is the month's last day, "2026-11-30". FinishExpiredGames ends the
// game once that day is over.
func (m Month) EndDate() string { return m.first().AddDate(0, 1, -1).Format(time.DateOnly) }

func (m Month) first() time.Time { return time.Date(m.Year, m.M, 1, 0, 0, 0, 0, time.UTC) }

// defaultGame is the settings for the first monthly game. Later games copy
// the one before, so an admin's changes carry forward.
var defaultGame = db.Game{
	Description:        "One month to grow $1,000,000. The biggest portfolio when the game ends wins.",
	Markets:            "both",
	StartingBalance:    100000000,
	BaseCurrency:       "AUD",
	ReferralBonusPct:   1,
	CreditInterestRate: 525,
	FxFee:              50,
	LimitOrders:        1,
	AfterHoursTrading:  1,
}

// EnsureMonthlyGames creates the games for now's month and the next one if
// they don't exist yet, so there's always a game running and the next one to
// join. The scheduler runs it every minute; now is in the game time zone.
func EnsureMonthlyGames(ctx context.Context, q *db.Queries, now time.Time) {
	this := MonthOf(now)
	for _, m := range []Month{this, this.Next()} {
		if err := ensureMonthGame(ctx, q, m); err != nil {
			slog.Error("create monthly game", "month", m.Key(), "error", err)
			return
		}
	}
}

func ensureMonthGame(ctx context.Context, q *db.Queries, m Month) error {
	if _, err := q.GetGameByMonth(ctx, sql.NullString{String: m.Key(), Valid: true}); err == nil {
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
	prev, err := q.GetLatestMonthlyGame(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		prev = defaultGame
	} else if err != nil {
		return err
	}
	g, err := q.CreateGame(ctx, db.CreateGameParams{
		CreatedBy:          owner,
		Name:               m.Name(),
		Description:        prev.Description,
		Markets:            prev.Markets,
		StartingBalance:    prev.StartingBalance,
		BaseCurrency:       prev.BaseCurrency,
		MaxParticipants:    prev.MaxParticipants,
		StartDate:          m.StartDate(),
		EndDate:            m.EndDate(),
		AllowShort:         prev.AllowShort,
		TradeFee:           prev.TradeFee,
		FxFee:              prev.FxFee,
		ReferralBonusPct:   prev.ReferralBonusPct,
		CreditInterestRate: prev.CreditInterestRate,
		LimitOrders:        prev.LimitOrders,
		AfterHoursTrading:  prev.AfterHoursTrading,
		Quarter:            sql.NullString{String: m.Key(), Valid: true},
	})
	if err != nil {
		return err
	}
	slog.Info("created monthly game", "id", g.ID, "month", m.Key(), "start", g.StartDate)
	return nil
}
