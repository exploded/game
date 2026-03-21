package handler

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// SeedAchievements inserts all achievement definitions.
func SeedAchievements(ctx context.Context, q *db.Queries) {
	defs := []struct {
		Key, Name, Desc, Icon string
	}{
		{"first_trade", "First Trade", "Execute your first trade", "🎯"},
		{"ten_trades", "Active Trader", "Complete 10 trades in a single game", "📊"},
		{"fifty_trades", "Trading Machine", "Complete 50 trades in a single game", "⚡"},
		{"ten_pct_gain", "Rising Star", "Achieve a 10%+ portfolio return", "📈"},
		{"twenty_five_pct_gain", "Market Wizard", "Achieve a 25%+ portfolio return", "🧙"},
		{"diversified_5", "Diversified", "Hold 5 or more different stocks", "🎨"},
		{"diversified_10", "Portfolio Manager", "Hold 10 or more different stocks", "💼"},
		{"first_place", "Champion", "Finish in first place", "🏆"},
		{"top_three", "Podium Finish", "Finish in the top 3", "🥇"},
		{"joined_3_games", "Social Player", "Join 3 or more games", "🤝"},
		{"watchlist_10", "Market Watcher", "Add 10 stocks to your watchlist", "👀"},
		{"referral_3", "Recruiter", "Refer 3 players to a single game", "📣"},
	}
	for _, d := range defs {
		if err := q.UpsertAchievement(ctx, db.UpsertAchievementParams{
			Key: d.Key, Name: d.Name, Description: d.Desc, Icon: d.Icon,
		}); err != nil {
			slog.Debug("seed achievement", "key", d.Key, "error", err)
		}
	}
}

// SeedAvatars inserts all avatar definitions.
func SeedAvatars(ctx context.Context, q *db.Queries) {
	type def struct {
		Key, Name, Path, Category, AchKey string
		Sort                              int64
	}
	avatars := []def{
		// Free avatars (always available)
		{"bull", "Bull", "/static/avatars/bull.svg", "free", "", 1},
		{"bear", "Bear", "/static/avatars/bear.svg", "free", "", 2},
		{"rocket", "Rocket", "/static/avatars/rocket.svg", "free", "", 3},
		{"chart", "Chart", "/static/avatars/chart.svg", "free", "", 4},
		{"diamond", "Diamond Hands", "/static/avatars/diamond.svg", "free", "", 5},
		{"money", "Money Bags", "/static/avatars/money.svg", "free", "", 6},
		{"wolf", "Wolf", "/static/avatars/wolf.svg", "free", "", 7},
		{"eagle", "Eagle", "/static/avatars/eagle.svg", "free", "", 8},
		{"shark", "Shark", "/static/avatars/shark.svg", "free", "", 9},
		// Achievement-locked avatars
		{"crown", "Crown", "/static/avatars/crown.svg", "achievement", "first_place", 20},
		{"wizard", "Wizard", "/static/avatars/wizard.svg", "achievement", "twenty_five_pct_gain", 21},
		{"fire", "On Fire", "/static/avatars/fire.svg", "achievement", "fifty_trades", 22},
		{"trophy", "Trophy", "/static/avatars/trophy.svg", "achievement", "top_three", 23},
	}
	for _, a := range avatars {
		var achKey sql.NullString
		if a.AchKey != "" {
			achKey = sql.NullString{String: a.AchKey, Valid: true}
		}
		if err := q.UpsertAvatar(ctx, db.UpsertAvatarParams{
			Key: a.Key, Name: a.Name, ImagePath: a.Path,
			Category: a.Category, AchievementKey: achKey, SortOrder: a.Sort,
		}); err != nil {
			slog.Debug("seed avatar", "key", a.Key, "error", err)
		}
	}

	// Populate the template avatar path map for the avatarURL function.
	allAvatars, _ := q.ListAvatars(ctx)
	for _, av := range allAvatars {
		RegisterAvatarPath(av.ID, av.ImagePath)
	}
}

// CheckAndGrantAchievements checks if a user has earned new achievements after a trade.
func (h *Handler) CheckAndGrantAchievements(ctx context.Context, userID, gameID, participantID int64) {
	// Count trades.
	tradeCount, _ := h.q.CountTransactions(ctx, participantID)

	type check struct {
		key       string
		threshold int64
	}
	tradeChecks := []check{
		{"first_trade", 1},
		{"ten_trades", 10},
		{"fifty_trades", 50},
	}
	for _, c := range tradeChecks {
		if tradeCount >= c.threshold {
			h.grantIfNew(ctx, userID, c.key, gameID)
		}
	}

	// Check diversification.
	holdings, _ := h.q.ListHoldings(ctx, participantID)
	if len(holdings) >= 5 {
		h.grantIfNew(ctx, userID, "diversified_5", gameID)
	}
	if len(holdings) >= 10 {
		h.grantIfNew(ctx, userID, "diversified_10", gameID)
	}

	// Check portfolio return.
	participant, err := h.q.GetParticipant(ctx, participantID)
	if err != nil {
		return
	}
	game, err := h.q.GetGame(ctx, gameID)
	if err != nil {
		return
	}
	if game.StartingBalance > 0 {
		returnPct := float64(participant.PortfolioValue-game.StartingBalance) / float64(game.StartingBalance) * 100
		if returnPct >= 10 {
			h.grantIfNew(ctx, userID, "ten_pct_gain", gameID)
		}
		if returnPct >= 25 {
			h.grantIfNew(ctx, userID, "twenty_five_pct_gain", gameID)
		}
	}
}

func (h *Handler) grantIfNew(ctx context.Context, userID int64, key string, gameID int64) {
	ach, err := h.q.GetAchievementByKey(ctx, key)
	if err != nil {
		return
	}
	gid := sql.NullInt64{Int64: gameID, Valid: true}
	already, _ := h.q.HasAchievement(ctx, db.HasAchievementParams{
		UserID: userID, AchievementID: ach.ID, GameID: gid,
	})
	if already > 0 {
		return
	}
	_ = h.q.GrantAchievement(ctx, db.GrantAchievementParams{
		UserID: userID, AchievementID: ach.ID, GameID: gid,
	})

	// Also create a notification.
	_ = h.q.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  userID,
		GameID:  gid,
		Type:    "achievement",
		Title:   "Achievement Unlocked!",
		Message: ach.Icon + " " + ach.Name + " - " + ach.Description,
	})
}

func (h *Handler) AchievementsPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	achievements, _ := h.q.ListUserAchievements(r.Context(), user.ID)
	allAchievements, _ := h.q.ListAchievements(r.Context())

	h.render(w, r, "achievements/index", "", PageData{
		Title: "Achievements",
		Items: achievements,
		Extra: map[string]any{
			"All": allAchievements,
		},
	})
}
