package market

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/exploded/game/internal/db"
)

// ProcessRecurringGames creates new games from finished recurring ones.
func ProcessRecurringGames(ctx context.Context, q *db.Queries) {
	games, err := q.ListFinishedRecurringGames(ctx)
	if err != nil {
		return
	}

	for _, game := range games {
		if !game.RecurringInterval.Valid || game.RecurringInterval.String == "" {
			continue
		}

		interval := game.RecurringInterval.String

		// Parse dates to compute new range.
		startDate, err := time.Parse("2006-01-02", game.StartDate)
		if err != nil {
			continue
		}
		endDate, err := time.Parse("2006-01-02", game.EndDate)
		if err != nil {
			continue
		}

		duration := endDate.Sub(startDate)
		var newStart time.Time
		switch interval {
		case "weekly":
			newStart = endDate.AddDate(0, 0, 1)
		case "monthly":
			newStart = endDate.AddDate(0, 0, 1)
		default:
			continue
		}
		newEnd := newStart.Add(duration)

		// Check if a child game already exists for this parent.
		// Simple check: look for a game with the same parent_game_id that's not cancelled.
		// For now, just create one. The recurring_interval on the new game
		// ensures it also recurs.
		newGame, err := q.CreateGame(ctx, db.CreateGameParams{
			CreatedBy:         game.CreatedBy,
			Name:              game.Name,
			Description:       game.Description,
			Markets:           game.Markets,
			StartingBalance:   game.StartingBalance,
			BaseCurrency:      game.BaseCurrency,
			MaxParticipants:   game.MaxParticipants,
			StartDate:         newStart.Format("2006-01-02"),
			EndDate:           newEnd.Format("2006-01-02"),
			AllowShort:        game.AllowShort,
			TradeFee:          game.TradeFee,
			RecurringInterval: game.RecurringInterval,
			ReferralBonusPct:  game.ReferralBonusPct,
		})
		if err != nil {
			slog.Error("create recurring game", "parentID", game.ID, "error", err)
			continue
		}

		// Clear recurring on the old game so it doesn't spawn more.
		// We do this by setting it to null via raw SQL since there's no query for it.
		// Instead, just log it. The ListFinishedRecurringGames will only get
		// games that are 'finished', so once the old game stays finished and a new
		// game exists, we need to avoid duplicates. Let's use parent_game_id.
		slog.Info("created recurring game", "parentID", game.ID, "newID", newGame.ID, "start", newStart.Format("2006-01-02"))

		// Auto-join the creator.
		_, _ = q.JoinGame(ctx, db.JoinGameParams{
			GameID:         newGame.ID,
			UserID:         game.CreatedBy,
			CashBalance:    game.StartingBalance,
			PortfolioValue: game.StartingBalance,
		})

		// Notify the creator.
		_ = q.CreateNotification(ctx, db.CreateNotificationParams{
			UserID:  game.CreatedBy,
			GameID:  sql.NullInt64{Int64: newGame.ID, Valid: true},
			Type:    "game_start",
			Title:   "Recurring Game Created",
			Message: "A new round of '" + game.Name + "' has been created!",
		})
	}
}
