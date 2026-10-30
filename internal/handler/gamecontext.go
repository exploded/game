package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/go-chi/chi/v5"
)

// gameBar is the data for the bar under the nav on every /games/{id} page
// (partials/_gamebar.html): which game you're in, your cash, the tabs and a
// switcher for your other games.
type gameBar struct {
	Game        db.Game
	Participant *db.Participant // nil when the user hasn't joined
	Others      []db.Game       // the user's other pending or active games
	Tab         string          // the highlighted tab, "" for none
}

type gameBarKey struct{}

// GameContext loads the game bar for full-page GETs under /games/{id} and
// remembers the game as the user's current one. A user who isn't in this
// month's game is joined to it. render picks it up from the request context,
// so the handlers don't need to know about it.
func (h *Handler) GameContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if r.Method != http.MethodGet || isHTMX(r) || user == nil {
			next.ServeHTTP(w, r)
			return
		}
		// PathValue isn't set yet while subrouter middleware runs.
		gameID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		game, err := h.q.GetGame(r.Context(), gameID)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		bar := &gameBar{Game: game, Tab: gameTab(r.URL.Path)}
		p, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
			GameID: gameID, UserID: user.ID,
		})
		if errors.Is(err, sql.ErrNoRows) && h.isCurrentGame(game) {
			// Everyone plays this month's game: no Join button to find.
			p, err = h.joinGame(r.Context(), game, user.ID)
		}
		if err == nil {
			bar.Participant = &p
			if !user.LastGameID.Valid || user.LastGameID.Int64 != gameID {
				_ = h.q.SetLastGame(r.Context(), db.SetLastGameParams{LastGameID: nint64(gameID), ID: user.ID})
			}
		}
		if games, err := h.q.ListUserGames(r.Context(), user.ID); err == nil {
			for _, g := range games {
				if g.ID != gameID && (g.Status == "active" || g.Status == "pending") {
					bar.Others = append(bar.Others, g)
				}
			}
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), gameBarKey{}, bar)))
	})
}

// gameTab maps a /games/{id}/... path to the tab it belongs under. The
// game's home page is the portfolio for players and the rules for anyone
// else; the game bar knows which.
func gameTab(path string) string {
	parts := strings.SplitN(strings.Trim(path, "/"), "/", 4) // games, id, section, rest
	if len(parts) < 3 {
		return "home"
	}
	switch parts[2] {
	case "portfolio", "history":
		return "portfolio"
	case "rules":
		return "rules"
	case "stocks", "search", "trade":
		return "stocks"
	case "orders":
		return "orders"
	case "watchlist":
		return "watchlist"
	case "leaderboard":
		return "leaderboard"
	case "analytics", "sectors", "compare", "benchmark":
		return "analytics"
	case "chat":
		return "chat"
	}
	return ""
}
