package handler

import (
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

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
// round to zero show as "0.00%", never "-0.00%". A short's base is
// negative, so its size is used: the sign follows the gain or loss.
func (c change) Pct() string {
	if c.Base == 0 {
		return ""
	}
	p := math.Round(float64(c.Amount)/math.Abs(float64(c.Base))*10000) / 100
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
	Avg         int64  // average price paid per share
	Price       int64  // the latest close, or the final one once the game is over
	Today       change // what the position made since the previous close (dayMove)
	Total       change // value against what the shares cost
	CloseQueued bool   // an order closing the position waits for the market to open
}

// CloseSide is the trade that closes the position: sell shares held, buy
// back a short.
func (r holdingRow) CloseSide() string {
	if r.Quantity < 0 {
		return "buy"
	}
	return "sell"
}

// Shares is the position's size without its sign.
func (r holdingRow) Shares() int64 {
	if r.Quantity < 0 {
		return -r.Quantity
	}
	return r.Quantity
}

// dayMove is how much a position of qty shares made since the previous
// close, from the newest two price bars and the player's trades in the
// stock. Shares held at that close count from it. Shares bought or sold
// since count from the price they traded at, so a buy made after the close
// isn't credited with the move before it. Trades are dated in UTC, not the
// market's time: one made near the close fills at that close, so which side
// of the line it lands on doesn't change the answer.
func dayMove(qty int64, bars []db.StockPrice, trades []db.ListTradesSinceRow) change {
	if len(bars) < 2 {
		return change{}
	}
	start := qty   // shares held at the previous close
	var paid int64 // paid for shares since, less what sales brought in
	for _, t := range trades {
		if len(t.CreatedAt) < 10 || t.CreatedAt[:10] <= bars[1].Date {
			continue
		}
		if t.Type == "sell" {
			start += t.Quantity
			paid -= t.Total
		} else {
			start -= t.Quantity
			paid += t.Total
		}
	}
	base := start*bars[1].Close + paid
	return change{qty*bars[0].Close - base, base}
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

// rankPlayer places p among the players on the leaderboard, which is sorted
// by value, best first. A player missing from it (blocked) is counted in too,
// so they still see their spot.
func rankPlayer(p db.Participant, board []db.ListParticipantsByGameRow) standing {
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
	board, _ := h.q.ListParticipantsByGame(ctx, game.ID)

	var rate int64 = 1000000
	if er, err := h.q.GetLatestExchangeRate(ctx); err == nil {
		rate = er.RateAudUsd
	}

	// Queued market orders, by stock and side.
	queued := map[int64]map[string]bool{}
	if orders, err := h.q.ListOpenLimitOrders(ctx, participant.ID); err == nil {
		for _, o := range orders {
			if o.Kind == "market" {
				if queued[o.StockID] == nil {
					queued[o.StockID] = map[string]bool{}
				}
				queued[o.StockID][o.Type] = true
			}
		}
	}

	// Once the game is over its values stay as they were at the end, so the
	// price comes from the final value and there's no today.
	over := game.Status != "active"

	// Every trade in this game, by stock. A month of one player's trades is
	// small, and it means no trade since a stock's previous close is missed.
	trades := map[int64][]db.ListTradesSinceRow{}
	if ts, err := h.q.ListTradesSince(ctx, db.ListTradesSinceParams{ParticipantID: participant.ID, CreatedAt: game.StartDate}); err == nil {
		for _, t := range ts {
			trades[t.StockID] = append(trades[t.StockID], t)
		}
	}

	rows := make([]holdingRow, 0, len(holdings))
	var todayBase int64 // today's move across all positions, in the game's currency
	for _, ho := range holdings {
		row := holdingRow{
			ListHoldingsRow: ho,
			Avg:             market.AvgCost(ho.Quantity, ho.CostBasis),
			Total:           change{ho.CurrentValue - ho.CostBasis, ho.CostBasis},
		}
		row.CloseQueued = queued[ho.StockID][row.CloseSide()]
		if over {
			if ho.Quantity != 0 {
				row.Price = ho.CurrentValue / ho.Quantity
			}
		} else if bars, err := h.q.ListLatestPrices(ctx, ho.StockID); err == nil && len(bars) > 0 {
			row.Price = bars[0].Close
			row.Today = dayMove(ho.Quantity, bars, trades[ho.StockID])
		}
		todayBase += convertCurrency(row.Today.Amount, ho.Currency, game.BaseCurrency, rate)
		rows = append(rows, row)
		delete(trades, ho.StockID)
	}
	var today *change
	if !over {
		// A stock sold out of today still made or lost money before the sale.
		for stockID, ts := range trades {
			if bars, err := h.q.ListLatestPrices(ctx, stockID); err == nil {
				todayBase += convertCurrency(dayMove(0, bars, ts).Amount, ts[0].Currency, game.BaseCurrency, rate)
			}
		}
		today = &change{todayBase, participant.PortfolioValue - todayBase}
	}
	interest, _ := h.q.TotalInterest(ctx, participant.ID)
	var invites []db.ListInvitesSentRow
	var inviteJoined bool
	if invitesOpen(game) {
		invites, _ = h.q.ListInvitesSent(ctx, db.ListInvitesSentParams{GameID: game.ID, CreatedBy: participant.UserID})
		for _, inv := range invites {
			inviteJoined = inviteJoined || inv.UseCount > 0
		}
	}

	h.render(w, r, "portfolio/index", "", PageData{
		Title: game.Name,
		Item:  game,
		Items: rows,
		Extra: map[string]any{
			"Participant":  participant,
			"Snapshots":    snapshots,
			"Over":         over,
			"Today":        today,
			"Total":        change{participant.PortfolioValue - game.StartingBalance, game.StartingBalance},
			"Standing":     rankPlayer(participant, board),
			"Interest":     interest,
			"InvitesOpen":  invitesOpen(game),
			"InviteJoined": inviteJoined,
			"Bonus":        fmtDollars(referralBonus(game)),
			"Invites":      invites,
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

	rows, _ := h.q.ListHistory(r.Context(), db.ListHistoryParams{
		ParticipantID: participant.ID,
		Lim:           perPage,
		Off:           offset,
	})
	total, _ := h.q.CountHistory(r.Context(), participant.ID)

	h.render(w, r, "portfolio/history", "", PageData{
		Title: "History - " + game.Name,
		Item:  game,
		Items: rows,
		Extra: map[string]any{
			"Participant": participant,
			"Page":        page,
			"TotalPages":  (total + perPage - 1) / perPage,
		},
	})
}
