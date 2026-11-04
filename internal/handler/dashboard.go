package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// homeGame is the Game Home tile's live line: the player's value, how it
// has moved and where they sit in this month's game.
type homeGame struct {
	Game     db.Game
	P        db.Participant
	Value    string // the value in short, "$1.02M", to fit a phone's tile
	Total    change
	Standing standing
	DaysLeft string
}

// Dashboard is the signed-in home page: three tiles (Game Home, Ask a
// Question, Learn) and Rob's intro video. Sign-in, the logo and the Home tab
// land here. It joins the player to this month's game, as the game pages do,
// so the Game Home tile can show their value and rank.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	extra := map[string]any{}
	if g, ok := h.currentGame(ctx, user.ID); ok {
		extra["GameID"] = g.ID // Game Home opens it, even if the figures below fail
		p, err := h.q.GetParticipantByGameAndUser(ctx, db.GetParticipantByGameAndUserParams{GameID: g.ID, UserID: user.ID})
		if err == nil {
			board, _ := h.q.ListParticipantsByGame(ctx, g.ID)
			extra["Game"] = homeGame{
				Game:     g,
				P:        p,
				Value:    bigMoney(p.PortfolioValue / 100),
				Total:    change{p.PortfolioValue - g.StartingBalance, g.StartingBalance},
				Standing: rankPlayer(p, board),
				DaysLeft: daysLeft(g, time.Now().In(h.loc)),
			}
		}
	}
	h.render(w, r, "dashboard/index", "", PageData{Title: "Home", Extra: h.homeExtra(ctx, extra)})
}

// StockInCurrentGame opens a stock's page in this month's game, joining the
// player as Dashboard does. Learn links stocks here, since it has no game.
func (h *Handler) StockInCurrentGame(w http.ResponseWriter, r *http.Request) {
	sid, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user := auth.UserFromContext(r.Context())
	g, ok := h.currentGame(r.Context(), user.ID)
	if !ok {
		http.Redirect(w, r, "/games", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/games/%d/stocks/%d", g.ID, sid), http.StatusSeeOther)
}
