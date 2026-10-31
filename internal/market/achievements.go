package market

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/exploded/game/internal/db"
)

// GrantAchievement gives the user the achievement with key for gameID, once,
// and tells them with a notification. The achievements themselves are seeded
// by handler.SeedAchievements.
func GrantAchievement(ctx context.Context, q *db.Queries, userID int64, key string, gameID int64) {
	ach, err := q.GetAchievementByKey(ctx, key)
	if err != nil {
		slog.Error("achievement: get by key", "key", key, "error", err)
		return
	}
	gid := sql.NullInt64{Int64: gameID, Valid: true}
	already, err := q.HasAchievement(ctx, db.HasAchievementParams{
		UserID: userID, AchievementID: ach.ID, GameID: gid,
	})
	if err != nil {
		slog.Error("achievement: has check", "key", key, "error", err)
		return
	}
	if already > 0 {
		return
	}
	if err := q.GrantAchievement(ctx, db.GrantAchievementParams{
		UserID: userID, AchievementID: ach.ID, GameID: gid,
	}); err != nil {
		slog.Error("achievement: grant", "key", key, "error", err)
		return
	}
	slog.Info("achievement: granted", "key", key, "user", userID)

	if err := q.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  userID,
		GameID:  gid,
		Type:    "achievement",
		Title:   "Achievement Unlocked!",
		Message: ach.Icon + " " + ach.Name + " - " + ach.Description,
	}); err != nil {
		slog.Error("achievement: notification", "key", key, "error", err)
	}
}

// AwardPlaces gives Champion to whoever finished game first and Podium
// Finish to everyone in the top 3. Only players who made a trade are placed,
// so the players who joined and never traded can't win on interest alone.
// Players with the same value share a place, as on the leaderboard.
func AwardPlaces(ctx context.Context, q *db.Queries, game db.Game) {
	board, err := q.ListParticipantsByGame(ctx, game.ID) // best first
	if err != nil {
		slog.Error("award places: list players", "game", game.ID, "error", err)
		return
	}
	placed, rank := 0, 0
	var last int64
	for _, p := range board {
		if n, err := q.CountTransactions(ctx, p.ID); err != nil || n == 0 {
			continue
		}
		placed++
		if placed == 1 || p.PortfolioValue != last {
			rank = placed
		}
		last = p.PortfolioValue
		if rank > 3 {
			break
		}
		if rank == 1 {
			GrantAchievement(ctx, q, p.UserID, "first_place", game.ID)
		}
		GrantAchievement(ctx, q, p.UserID, "top_three", game.ID)
	}
}
