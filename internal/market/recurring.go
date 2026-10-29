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

		// The next round copies every setting. It recurs in turn, and its
		// parent_game_id stops this game producing another round.
		newGame, err := q.CreateGame(ctx, db.CreateGameParams{
			CreatedBy:            game.CreatedBy,
			Name:                 game.Name,
			Description:          game.Description,
			Markets:              game.Markets,
			StartingBalance:      game.StartingBalance,
			BaseCurrency:         game.BaseCurrency,
			MaxParticipants:      game.MaxParticipants,
			StartDate:            newStart.Format("2006-01-02"),
			EndDate:              newEnd.Format("2006-01-02"),
			AllowShort:           game.AllowShort,
			TradeFee:             game.TradeFee,
			RecurringInterval:    game.RecurringInterval,
			ReferralBonusPct:     game.ReferralBonusPct,
			IsPublic:             game.IsPublic,
			PortfolioVisibility:  game.PortfolioVisibility,
			CreditInterestRate:   game.CreditInterestRate,
			LeverageInterestRate: game.LeverageInterestRate,
			MinStockPrice:        game.MinStockPrice,
			MaxStockPrice:        game.MaxStockPrice,
			MarginTrading:        game.MarginTrading,
			LimitOrders:          game.LimitOrders,
			StopLoss:             game.StopLoss,
			FractionalShares:     game.FractionalShares,
		})
		if err != nil {
			slog.Error("create recurring game", "parentID", game.ID, "error", err)
			continue
		}
		if err := q.SetGameParent(ctx, db.SetGameParentParams{
			ParentGameID: sql.NullInt64{Int64: game.ID, Valid: true}, ID: newGame.ID,
		}); err != nil {
			slog.Error("set recurring game parent", "parentID", game.ID, "newID", newGame.ID, "error", err)
		}
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
