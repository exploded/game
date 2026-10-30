package market

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/exploded/game/internal/db"
)

// DailyInterest is a day's interest on balance cents at rateBps basis points
// a year, rounded to the nearest cent. A year is 365 days.
func DailyInterest(balance, rateBps int64) int64 {
	const perYear = 10000 * 365
	return (balance*rateBps + perYear/2) / perYear
}

// CreditInterest pays each player a day's interest on their cash for the day
// before now, in each game that ran that day, at the game's
// credit_interest_rate. It uses the cash held when it runs, which the
// scheduler does every minute, so in practice just after midnight. Each
// player is paid once a day; a day the server was down isn't made up later.
// now is in the game time zone.
func CreditInterest(ctx context.Context, q *db.Queries, rawDB *sql.DB, now time.Time) error {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := midnight.AddDate(0, 0, -1).Format(time.DateOnly)
	due, err := q.ListInterestDue(ctx, db.ListInterestDueParams{
		Day: day,
		// joined_at is UTC, in SQLite's datetime('now') format.
		DayEnd: midnight.UTC().Format(time.DateTime),
	})
	if err != nil || len(due) == 0 {
		return err
	}

	tx, err := rawDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	var total int64
	for _, p := range due {
		amount := DailyInterest(p.CashBalance, p.CreditInterestRate)
		// Record it even when it rounds to nothing, so the player isn't
		// listed again every minute.
		if err := qtx.InsertInterestPayment(ctx, db.InsertInterestPaymentParams{
			ParticipantID: p.ID,
			Date:          day,
			Balance:       p.CashBalance,
			Rate:          p.CreditInterestRate,
			Amount:        amount,
		}); err != nil {
			return err
		}
		if amount == 0 {
			continue
		}
		if err := qtx.CreditCash(ctx, db.CreditCashParams{Amount: amount, ID: p.ID}); err != nil {
			return err
		}
		total += amount
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	slog.Info("interest credited", "day", day, "players", len(due), "cents", total)
	return nil
}
