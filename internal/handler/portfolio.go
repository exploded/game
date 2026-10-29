package handler

import (
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// Portfolio redirects old links: the portfolio is now the game's home page.
func (h *Handler) Portfolio(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/games/"+r.PathValue("id"), http.StatusMovedPermanently)
}

// change is a gain or loss, and the amount it's measured against.
type change struct {
	Amount int64
	Base   int64
}

// Text is the amount with its sign: "+$12.50" or "-$3.00".
func (c change) Text() string {
	if c.Amount > 0 {
		return "+" + fmtCents(c.Amount)
	}
	return fmtCents(c.Amount)
}

// Pct is the change as a percentage of the base: "+1.25%". Changes that
// round to zero show as "0.00%", never "-0.00%".
func (c change) Pct() string {
	if c.Base == 0 {
		return ""
	}
	p := math.Round(float64(c.Amount)/float64(c.Base)*10000) / 100
	if p == 0 {
		return "0.00%"
	}
	return fmt.Sprintf("%+.2f%%", p)
}

// Class colours a pill green for a gain and red for a loss.
func (c change) Class() string {
	switch {
	case c.Amount > 0:
		return "pill-up"
	case c.Amount < 0:
		return "pill-down"
	}
	return ""
}

// holdingRow is one line of the holdings table. Amounts are in the
// stock's own currency.
type holdingRow struct {
	db.ListHoldingsRow
	Price int64  // the latest close
	Today change // the latest close against the one before
	Total change // value against what the shares cost
}

// standing is where a player sits on the game's leaderboard.
type standing struct {
	Rank, Of int
	Ahead    string // the player one place up, "" when you're first
	Gap      int64  // how far behind them you are
}

// Place is the rank as an ordinal: "1st", "2nd", "13th".
func (s standing) Place() string {
	suffix := "th"
	if n := s.Rank % 100; n < 11 || n > 13 {
		switch s.Rank % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(s.Rank) + suffix
}

// rankPlayer places p among the players on the public leaderboard, which
// is sorted by value, best first. A private player is counted in too, so
// they still see their spot.
func rankPlayer(p db.Participant, board []db.ListPublicParticipantsByGameRow) standing {
	s := standing{Rank: 1, Of: len(board)}
	listed := false
	for _, o := range board {
		if o.ID == p.ID {
			listed = true
			continue
		}
		if o.PortfolioValue > p.PortfolioValue {
			s.Rank++
			// The board is sorted, so the last one above is the closest.
			s.Ahead, s.Gap = o.UserName, o.PortfolioValue-p.PortfolioValue
		}
	}
	if !listed {
		s.Of++
	}
	return s
}

// portfolio renders a player's home page for a game: what they're worth,
// how it moved today and since the start, their leaderboard spot, and how
// each holding is doing.
func (h *Handler) portfolio(w http.ResponseWriter, r *http.Request, game db.Game, participant db.Participant) {
	ctx := r.Context()
	holdings, _ := h.q.ListHoldings(ctx, participant.ID)
	snapshots, _ := h.q.ListSnapshots(ctx, db.ListSnapshotsParams{
		ParticipantID: participant.ID,
		Date:          game.StartDate,
		Date_2:        game.EndDate,
	})
	board, _ := h.q.ListPublicParticipantsByGame(ctx, game.ID)

	var rate int64 = 1000000
	if er, err := h.q.GetLatestExchangeRate(ctx); err == nil {
		rate = er.RateAudUsd
	}

	rows := make([]holdingRow, 0, len(holdings))
	var todayBase int64 // today's move across all holdings, in the game's currency
	for _, ho := range holdings {
		row := holdingRow{
			ListHoldingsRow: ho,
			Total:           change{ho.CurrentValue - ho.Quantity*ho.AvgCost, ho.Quantity * ho.AvgCost},
		}
		if bars, err := h.q.ListLatestPrices(ctx, ho.StockID); err == nil && len(bars) > 0 {
			row.Price = bars[0].Close
			if len(bars) > 1 {
				row.Today = change{ho.Quantity * (bars[0].Close - bars[1].Close), ho.Quantity * bars[1].Close}
			}
		}
		todayBase += convertCurrency(row.Today.Amount, ho.Currency, game.BaseCurrency, rate)
		rows = append(rows, row)
	}

	h.render(w, r, "portfolio/index", "", PageData{
		Title: game.Name,
		Item:  game,
		Items: rows,
		Extra: map[string]any{
			"Participant": participant,
			"Snapshots":   snapshots,
			"Today":       change{todayBase, participant.PortfolioValue - todayBase},
			"Total":       change{participant.PortfolioValue - game.StartingBalance, game.StartingBalance},
			"Standing":    rankPlayer(participant, board),
		},
	})
}

func (h *Handler) TransactionHistory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.q.GetGame(r.Context(), gameID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	participant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}

	page, _ := strconv.ParseInt(r.URL.Query().Get("page"), 10, 64)
	if page < 1 {
		page = 1
	}
	perPage := int64(25)
	offset := (page - 1) * perPage

	txns, _ := h.q.ListTransactions(r.Context(), db.ListTransactionsParams{
		ParticipantID: participant.ID,
		Limit:         perPage,
		Offset:        offset,
	})
	total, _ := h.q.CountTransactions(r.Context(), participant.ID)

	h.render(w, r, "portfolio/history", "", PageData{
		Title: "History - " + game.Name,
		Item:  game,
		Items: txns,
		Extra: map[string]any{
			"Participant": participant,
			"Page":        page,
			"TotalPages":  (total + perPage - 1) / perPage,
		},
	})
}

func (h *Handler) ToggleVisibility(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	participant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}

	newVal := int64(1)
	if participant.IsPublic == 1 {
		newVal = 0
	}
	_ = h.q.UpdateParticipantVisibility(r.Context(), db.UpdateParticipantVisibilityParams{
		IsPublic: newVal, ID: participant.ID,
	})

	if isHTMX(r) {
		label := "Public"
		if newVal == 0 {
			label = "Private"
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<button hx-post="/games/` + r.PathValue("id") + `/visibility" hx-swap="outerHTML" class="btn btn-sm">` + label + `</button>`))
		return
	}
	http.Redirect(w, r, "/games/"+r.PathValue("id"), http.StatusSeeOther)
}
