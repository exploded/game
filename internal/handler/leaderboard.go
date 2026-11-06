package handler

import (
	"context"
	"net/http"
	"slices"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// leaderboardPageSize is how many players a page of Ranks shows. Until
// 6 November 2026 it showed everyone, which at 10,000 players was a 130 ms
// query and thousands of rows (docs/capacity.md).
const leaderboardPageSize = 100

// boardRow is a leaderboard row with its place. Equal values share a place.
type boardRow struct {
	db.ListLeaderboardRow
	Rank int
}

// leaderboardPage reads lim rows of the game's leaderboard from off, with
// their places. A page after the first counts how many are worth more than
// its first row, since a tie can run across pages.
func (h *Handler) leaderboardPage(ctx context.Context, gameID int64, lim, off int) ([]boardRow, error) {
	rows, err := h.q.ListLeaderboard(ctx, db.ListLeaderboardParams{GameID: gameID, Lim: int64(lim), Off: int64(off)})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	rank := 1
	if off > 0 {
		c, err := h.q.CountRanked(ctx, db.CountRankedParams{GameID: gameID, Value: rows[0].PortfolioValue})
		if err != nil {
			return nil, err
		}
		rank = int(c.Above) + 1
	}
	out := make([]boardRow, len(rows))
	for i, r := range rows {
		// Every row before this one is worth at least as much, so a lower
		// value is placed after all of them.
		if i > 0 && r.PortfolioValue != rows[i-1].PortfolioValue {
			rank = off + i + 1
		}
		out[i] = boardRow{r, rank}
	}
	return out, nil
}

func (h *Handler) Leaderboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.q.GetGame(ctx, gameID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(page, 1)
	off := (page - 1) * leaderboardPageSize
	// One more than a page, to know whether there's a next one.
	rows, err := h.leaderboardPage(ctx, game.ID, leaderboardPageSize+1, off)
	if err != nil {
		h.serverError(w, "leaderboard", err)
		return
	}
	if len(rows) == 0 && page > 1 {
		http.Redirect(w, r, "/games/"+strconv.FormatInt(game.ID, 10)+"/leaderboard", http.StatusSeeOther)
		return
	}
	more := len(rows) > leaderboardPageSize
	if more {
		rows = rows[:leaderboardPageSize]
	}

	// The player's own place, and their row when it's on another page.
	myParticipant, err := h.q.GetParticipantByGameAndUser(ctx, db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	playing := err == nil
	var st standing
	if playing {
		st = h.standingIn(ctx, myParticipant)
	} else if c, err := h.q.CountRanked(ctx, db.CountRankedParams{GameID: game.ID}); err == nil {
		st.Of = int(c.Ranked)
	}
	var me *boardRow
	if playing && !st.Unranked && !slices.ContainsFunc(rows, func(b boardRow) bool { return b.ID == myParticipant.ID }) {
		me = &boardRow{ListLeaderboardRow: db.ListLeaderboardRow{
			ID: myParticipant.ID, PortfolioValue: myParticipant.PortfolioValue, UsesAi: myParticipant.UsesAi, UserName: user.Name,
		}, Rank: st.Rank}
	}

	extra := map[string]any{
		"MyParticipant": myParticipant,
		// A player with no trade isn't on the board: tell them why.
		"Unranked": playing && st.Unranked,
		"Me":       me,
		"Of":       st.Of,
		"First":    off + 1,
		"Last":     off + len(rows),
		"Page":     page,
	}
	if page > 1 {
		extra["Prev"] = page - 1
	}
	if more {
		extra["Next"] = page + 1
	}
	h.render(w, r, "leaderboard/index", "", PageData{
		Title: "Leaderboard - " + game.Name,
		Item:  game,
		Items: rows,
		Extra: extra,
	})
}
