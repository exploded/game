package market

import (
	"context"
	"log/slog"

	"github.com/exploded/game/internal/db"
)

// GrantAchievement gives the user the achievement with key, once ever, and
// tells them with a notification. The achievements themselves are seeded by
// handler.SeedAchievements.
func GrantAchievement(ctx context.Context, q *db.Queries, userID int64, key string) {
	ach, err := q.GetAchievementByKey(ctx, key)
	if err != nil {
		slog.Error("achievement: get by key", "key", key, "error", err)
		return
	}
	n, err := q.GrantAchievement(ctx, db.GrantAchievementParams{UserID: userID, AchievementID: ach.ID})
	if err != nil {
		slog.Error("achievement: grant", "key", key, "error", err)
		return
	}
	if n == 0 {
		return // already earned
	}
	slog.Info("achievement: granted", "key", key, "user", userID)

	if err := q.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  userID,
		Type:    "achievement",
		Title:   "Achievement Unlocked!",
		Message: ach.Icon + " " + ach.Name + " - " + ach.Description,
	}); err != nil {
		slog.Error("achievement: notification", "key", key, "error", err)
	}
}

// AwardPlaces gives Champion to whoever finished game first and Podium
// Finish to everyone in the top 3. The leaderboard holds only players who made
// a trade, so the players who joined and never traded can't win on interest
// alone. Players with the same value share a place, as on the leaderboard.
func AwardPlaces(ctx context.Context, q *db.Queries, game db.Game) {
	const page = 50
	rank, i := 0, 0
	var last int64
	for off := int64(0); ; off += page {
		board, err := q.ListLeaderboard(ctx, db.ListLeaderboardParams{GameID: game.ID, Lim: page, Off: off}) // best first
		if err != nil {
			slog.Error("award places: list players", "game", game.ID, "error", err)
			return
		}
		for _, p := range board {
			if i == 0 || p.PortfolioValue != last {
				rank = i + 1
			}
			i++
			last = p.PortfolioValue
			if rank > 3 {
				return
			}
			if rank == 1 {
				GrantAchievement(ctx, q, p.UserID, "first_place")
			}
			GrantAchievement(ctx, q, p.UserID, "top_three")
		}
		if len(board) < page {
			return // the end of the board
		}
	}
}
