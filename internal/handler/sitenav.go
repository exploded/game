package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

// siteNav is what the top bar and the phone's tab bar need on every
// signed-in page (partials/_nav.html): the same five sections everywhere,
// so Learn and the lobby are one tap from home.
type siteNav struct {
	GameID int64  // the game Home, Stocks and Ranks open; 0 before one exists
	Tab    string // the section to highlight: home, stocks, ranks, learn, chat, notifications or ""
	Unread int64  // notifications not yet read, for the bell
}

// Home is the home page with the three section tiles (Dashboard), where the
// logo and the Home tab go. The game's own home page is its first tile.
func (n *siteNav) Home() string {
	return "/dashboard"
}

// In is a page of the nav's game, such as In "stocks".
func (n *siteNav) In(page string) string {
	if n.GameID == 0 {
		return "/dashboard"
	}
	return fmt.Sprintf("/games/%d/%s", n.GameID, page)
}

// siteNav fills the nav for a full page. Its game is the one on screen when
// the user plays it and it's running, otherwise this month's: a finished
// game's Stocks tab would have nothing to trade. The unread count is read
// here, not fetched after the page loads, so the bar never changes width.
func (h *Handler) siteNav(r *http.Request, user *db.User, bar *gameBar) *siteNav {
	ctx := r.Context()
	n := &siteNav{Tab: navTab(r.URL.Path)}
	if bar != nil && bar.Participant != nil && bar.Game.Status == "active" {
		n.GameID = bar.Game.ID
	} else {
		month := market.MonthOf(time.Now().In(h.loc))
		if g, err := h.q.GetGameByMonth(ctx, sql.NullString{String: month.Key(), Valid: true}); err == nil {
			n.GameID = g.ID
		}
	}
	n.Unread, _ = h.q.CountUnreadNotifications(ctx, user.ID)
	return n
}

// navTab is the section a path belongs to. A game's home page, orders,
// history and rules count as Home, whose Game Home tile leads to them;
// Activity sits under Ranks.
func navTab(path string) string {
	under := func(prefix string) bool {
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	switch {
	case strings.HasPrefix(path, "/games/"):
		switch gameTab(path) {
		case "home", "portfolio", "rules", "orders":
			return "home"
		case "stocks":
			return "stocks"
		case "leaderboard", "activity":
			return "ranks"
		}
	case path == "/dashboard":
		return "home"
	case under("/learn"), under("/courses"):
		return "learn"
	case path == "/lobby":
		return "chat"
	case path == "/notifications":
		return "notifications" // the bell, in the top bar
	}
	return ""
}
