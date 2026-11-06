package handler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"

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

	// Value, Today and Total in the game's currency, to sit beside the
	// player's cash. Price and Avg stay in the stock's own currency.
	ValueBase int64
	TodayBase change
	TotalBase change
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
func dayMove(qty int64, bars []db.StockPrice, trades []db.ListParticipantTradesRow) change {
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

// sessionDay names the US session a "today" move covers, when it isn't
// today's in New York: "Friday" over a weekend, which is still Saturday or
// Sunday in Sydney, or "23 Oct" for one more than a few days old. Empty for
// today's session, or with no session or timezone to go on.
func sessionDay(date string, now time.Time) string {
	d, err := time.Parse("2006-01-02", date)
	if err != nil || market.NewYork == nil {
		return ""
	}
	y, m, dd := now.In(market.NewYork).Date()
	days := int(time.Date(y, m, dd, 0, 0, 0, 0, time.UTC).Sub(d).Hours() / 24)
	switch {
	case days <= 0:
		return ""
	case days < 7:
		return d.Weekday().String()
	}
	return d.Format("2 Jan")
}

// standing is where a player sits on the game's leaderboard.
type standing struct {
	Rank, Of int
	Ahead    string // the player one place up, "" when you're first
	Gap      int64  // how far behind them you are
	Unranked bool   // no trade yet, so not on the leaderboard: Rank is 0
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

// standingIn is where p sits on its game's leaderboard, from counts rather
// than the whole board (docs/capacity.md). A player with no trade isn't
// ranked, even sitting on more than the leader. Equal values share a place.
// Blocked players aren't on the board, but they can't sign in to look.
func (h *Handler) standingIn(ctx context.Context, p db.Participant) standing {
	c, err := h.q.CountRanked(ctx, db.CountRankedParams{GameID: p.GameID, Value: p.PortfolioValue})
	if err != nil {
		slog.Error("count ranked players", "game", p.GameID, "error", err)
	}
	if p.Traded == 0 {
		return standing{Of: int(c.Ranked), Unranked: true}
	}
	s := standing{Rank: int(c.Above) + 1, Of: int(c.Ranked)}
	if s.Rank > 1 {
		if a, err := h.q.GetPlayerAhead(ctx, db.GetPlayerAheadParams{GameID: p.GameID, Value: p.PortfolioValue}); err == nil {
			s.Ahead, s.Gap = a.UserName, a.PortfolioValue-p.PortfolioValue
		}
	}
	return s
}

// portfolioFigures is what a player is worth in a game and how it moved:
// their home page, and the API's portfolio.
type portfolioFigures struct {
	Rows      []holdingRow
	Over      bool    // the game has ended: values are from its end, with no Today
	Today     *change // today's move across every position; nil once the game is over
	Session   string  // the newest bar's date: the session Today covers
	Total     change  // against the starting balance
	Standing  standing
	Interest  int64
	Dividends int64                       // received, less what shorts paid
	Orders    []db.ListOpenLimitOrdersRow // waiting to fill
}

// portfolioFigures works out a player's holdings, moves and rank.
func (h *Handler) portfolioFigures(ctx context.Context, game db.Game, participant db.Participant) portfolioFigures {
	holdings, _ := h.q.ListHoldings(ctx, participant.ID)

	var rate int64 = 1000000
	if er, err := h.q.GetLatestExchangeRate(ctx); err == nil {
		rate = er.RateAudUsd
	}

	// Queued market orders, by stock and side.
	queued := map[int64]map[string]bool{}
	orders, _ := h.q.ListOpenLimitOrders(ctx, participant.ID)
	for _, o := range orders {
		if o.Kind == "market" {
			if queued[o.StockID] == nil {
				queued[o.StockID] = map[string]bool{}
			}
			queued[o.StockID][o.Type] = true
		}
	}

	// Once the game is over its values stay as they were at the end, so the
	// price comes from the final value and there's no today.
	over := game.Status != "active"

	// Every trade in this game, by stock. A month of one player's trades is
	// small, and it means no trade since a stock's previous close is missed.
	trades := map[int64][]db.ListParticipantTradesRow{}
	if ts, err := h.q.ListParticipantTrades(ctx, participant.ID); err == nil {
		for _, t := range ts {
			trades[t.StockID] = append(trades[t.StockID], t)
		}
	}

	rows := make([]holdingRow, 0, len(holdings))
	var todayBase int64 // today's move across all positions, in the game's currency
	var session string  // the newest bar's date: the session "today" covers
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
			session = max(session, bars[0].Date)
		}
		toBase := func(c change) change {
			return change{
				convertCurrency(c.Amount, ho.Currency, game.BaseCurrency, rate),
				convertCurrency(c.Base, ho.Currency, game.BaseCurrency, rate),
			}
		}
		row.ValueBase = convertCurrency(ho.CurrentValue, ho.Currency, game.BaseCurrency, rate)
		row.TodayBase, row.TotalBase = toBase(row.Today), toBase(row.Total)
		todayBase += row.TodayBase.Amount
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
	dividends, _ := h.q.TotalDividends(ctx, participant.ID)
	return portfolioFigures{
		Rows:      rows,
		Over:      over,
		Today:     today,
		Session:   session,
		Total:     change{participant.PortfolioValue - game.StartingBalance, game.StartingBalance},
		Standing:  h.standingIn(ctx, participant),
		Interest:  interest,
		Dividends: dividends,
		Orders:    orders,
	}
}

// portfolio renders a player's home page for a game: what they're worth,
// how it moved today and since the start, their leaderboard spot, and how
// each holding is doing.
func (h *Handler) portfolio(w http.ResponseWriter, r *http.Request, game db.Game, participant db.Participant) {
	ctx := r.Context()
	pf := h.portfolioFigures(ctx, game, participant)
	snapshots, _ := h.q.ListSnapshots(ctx, db.ListSnapshotsParams{
		ParticipantID: participant.ID,
		Date:          game.StartDate,
		Date_2:        game.EndDate,
	})
	var invites []db.ListInvitesSentRow
	var inviteJoined bool
	if invitesOpen(game) {
		invites, _ = h.q.ListInvitesSent(ctx, db.ListInvitesSentParams{GameID: game.ID, CreatedBy: participant.UserID})
		for _, inv := range invites {
			inviteJoined = inviteJoined || inv.UseCount > 0
		}
	}

	// A player who holds nothing yet starts from Rob's Picks.
	var picks []screenRow
	if len(pf.Rows) == 0 && game.Status == "active" {
		if rows, err := h.screenRows(ctx, game.Markets); err == nil {
			picks = applyFilter(rows, defaultFilter, "", "", nil).Rows
			picks = picks[:min(len(picks), homePicks)]
			h.markRows(ctx, participant.ID, participant.UserID, picks)
		}
	}
	sectors, _ := h.q.ListHoldingsBySector(ctx, participant.ID)

	h.render(w, r, "portfolio/index", "", PageData{
		Title: game.Name,
		Item:  game,
		Items: pf.Rows,
		Extra: map[string]any{
			"Participant":  participant,
			"Snapshots":    snapshots,
			"Over":         pf.Over,
			"Today":        pf.Today,
			"SessionDay":   sessionDay(pf.Session, marketNow()),
			"Total":        pf.Total,
			"Standing":     pf.Standing,
			"Interest":     pf.Interest,
			"Dividends":    pf.Dividends,
			"InvitesOpen":  invitesOpen(game),
			"InviteJoined": inviteJoined,
			"Bonus":        fmtDollars(referralBonus(game)),
			"Invites":      invites,
			"Orders":       pf.Orders[:min(len(pf.Orders), homeOrders)],
			"MoreOrders":   len(pf.Orders) > homeOrders,
			"Sectors":      sectorShares(sectors),
			"Picks":        picks,
			"DaysLeft":     daysLeft(game, time.Now().In(h.loc)),
		},
	})
}

// How many waiting orders and Rob's Picks the home page lists.
const (
	homeOrders = 3
	homePicks  = 5
)

// sectorShare is one line of the home page's sector breakdown.
type sectorShare struct {
	Name  string
	Value int64   // in the stocks' own currency
	Pct   float64 // of the total held, 0 to 100
}

// sectorShares turns the holdings' value by sector into shares of the
// total, biggest first. Shorts aren't counted: a sector the player is
// short in isn't part of what they hold.
func sectorShares(rows []db.ListHoldingsBySectorRow) []sectorShare {
	var out []sectorShare
	var total int64
	for _, r := range rows {
		v := int64(math.Round(r.TotalValue.Float64))
		if v <= 0 {
			continue
		}
		name := r.Sector
		if name == "" {
			name = "Other"
		}
		out = append(out, sectorShare{Name: name, Value: v})
		total += v
	}
	for i := range out {
		out[i].Pct = float64(out[i].Value) / float64(total) * 100
	}
	slices.SortStableFunc(out, func(a, b sectorShare) int { return int(b.Value - a.Value) })
	return out
}

// daysLeft says where a game is in its month: "12 days left", "Last day",
// "Starts 1 Dec" or "Finished". A game ends at midnight after its end date.
func daysLeft(g db.Game, now time.Time) string {
	start, err1 := time.ParseInLocation(time.DateOnly, g.StartDate, now.Location())
	end, err2 := time.ParseInLocation(time.DateOnly, g.EndDate, now.Location())
	if err1 != nil || err2 != nil {
		return ""
	}
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch n := int(math.Round(end.Sub(today).Hours()/24)) + 1; {
	case g.Status == "pending" || today.Before(start):
		return "Starts " + start.Format("2 Jan")
	case g.Status != "active" || n <= 0:
		return "Finished"
	case n == 1:
		return "Last day"
	default:
		return strconv.Itoa(n) + " days left"
	}
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

	// The whole history, to keep a running cash balance. A month of one
	// player's trades and interest is small.
	all, _ := h.q.ListHistory(r.Context(), participant.ID)
	rows, sum := cashLedger(all, game.StartingBalance, participant)
	slices.Reverse(rows) // newest first
	totalPages := (int64(len(rows)) + perPage - 1) / perPage
	start := min((page-1)*perPage, int64(len(rows)))
	end := min(start+perPage, int64(len(rows)))

	h.render(w, r, "portfolio/history", "", PageData{
		Title: "My history - " + game.Name,
		Item:  game,
		Items: rows[start:end],
		Extra: map[string]any{
			"Participant": participant,
			"Sum":         sum,
			"Page":        page,
			"TotalPages":  totalPages,
			"LastPage":    page >= totalPages,
		},
	})
}

// historyRow is one line of a player's history, with what it did to their
// cash.
type historyRow struct {
	db.ListHistoryRow
	Change int64 // added to cash, or taken from it when negative
	Cash   int64 // cash after it
}

// ledgerSum adds a player's history up, from the cash they started with to
// the total the leaderboard shows. Amounts are in the game's currency.
type ledgerSum struct {
	Opening   int64
	Bought    int64 // paid for shares, before fees
	Sold      int64 // received for shares, before fees
	Fees      int64
	Interest  int64
	Dividends int64 // received, less what shorts paid
	Bonuses   int64
	Other     int64 // cash moved by nothing in the history: a reverse split's part share
	Cash      int64
	Invested  int64 // the shares, at the latest prices
	Total     int64
}

// cashLedger runs through a player's history, oldest first, keeping their
// cash balance, and totals it. Cash and Total come from the participant, so
// the sum always ends at the figures the rest of the site shows: anything the
// history doesn't explain lands in Other.
func cashLedger(history []db.ListHistoryRow, opening int64, p db.Participant) ([]historyRow, ledgerSum) {
	sum := ledgerSum{Opening: opening}
	rows := make([]historyRow, len(history))
	cash := opening
	for i, h := range history {
		var change int64
		switch h.Kind {
		case "buy":
			change = -(h.Amount + h.Fee)
			sum.Bought += h.Amount
			sum.Fees += h.Fee
		case "sell":
			change = max(h.Amount-h.Fee, 0) // a fee bigger than the sale takes it all, no more
			sum.Sold += h.Amount
			sum.Fees += h.Amount - change
		case "interest":
			change = h.Amount
			sum.Interest += h.Amount
		case "dividend":
			change = h.Amount
			sum.Dividends += h.Amount
		case "bonus":
			change = h.Amount
			sum.Bonuses += h.Amount
		}
		cash += change
		rows[i] = historyRow{ListHistoryRow: h, Change: change, Cash: cash}
	}
	sum.Cash = p.CashBalance
	sum.Other = p.CashBalance - cash
	sum.Invested = p.PortfolioValue - p.CashBalance
	sum.Total = p.PortfolioValue
	return rows, sum
}
