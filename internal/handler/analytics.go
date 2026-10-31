package handler

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// TradeAnalytics shows win rate, avg gain/loss, best/worst trade (Feature 15).
func (h *Handler) TradeAnalytics(w http.ResponseWriter, r *http.Request) {
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
		setFlashCookie(w, "Join the game first", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d", gameID), http.StatusSeeOther)
		return
	}

	txns, _ := h.q.ListAllTransactions(r.Context(), participant.ID)

	var totalBuys, totalSells int64
	var buyCount, sellCount int64
	for _, t := range txns {
		if t.Type == "buy" {
			totalBuys += t.ConvertedTotal
			buyCount++
		} else {
			totalSells += t.ConvertedTotal
			sellCount++
		}
	}

	closedTrades := realisedPnL(txns)
	var winners, losers int
	var bestTrade, worstTrade StockPnL
	var totalPnL int64
	for _, c := range closedTrades {
		totalPnL += c.PnL
		if c.PnL > 0 {
			winners++
			if c.PnL > bestTrade.PnL {
				bestTrade = c
			}
		} else if c.PnL < 0 {
			losers++
			if c.PnL < worstTrade.PnL {
				worstTrade = c
			}
		}
	}

	winRate := 0.0
	if len(closedTrades) > 0 {
		winRate = float64(winners) / float64(len(closedTrades)) * 100
	}

	h.render(w, r, "analytics/index", "", PageData{
		Title: "Analytics - " + game.Name,
		Item:  game,
		Extra: map[string]any{
			"Participant":  participant,
			"TotalTrades":  buyCount + sellCount,
			"BuyCount":     buyCount,
			"SellCount":    sellCount,
			"TotalBuys":    totalBuys,
			"TotalSells":   totalSells,
			"WinRate":      fmt.Sprintf("%.1f", winRate),
			"Winners":      winners,
			"Losers":       losers,
			"BestTrade":    bestTrade,
			"WorstTrade":   worstTrade,
			"TotalPnL":     totalPnL,
			"ClosedTrades": closedTrades,
		},
	})
}

// StockPnL is the profit or loss realised on one stock, in the game's base
// currency.
type StockPnL struct {
	Symbol string
	PnL    int64
}

// realisedPnL works out what each stock has made or lost on the shares
// already sold, or bought back for a short, replaying the trades in order.
// A position's cost is in the game's base currency and negative for a short
// (what the sale raised), as in market.Fill. Closing part of a position
// realises that part's share of the cost; a trade that crosses zero closes
// the old position and opens the other way. Stocks with nothing closed yet
// are left out.
func realisedPnL(txns []db.ListAllTransactionsRow) []StockPnL {
	type position struct {
		qty, cost int64
		at        int // index in out, or -1
	}
	positions := make(map[int64]*position)
	var out []StockPnL

	for _, t := range txns {
		p, ok := positions[t.StockID]
		if !ok {
			p = &position{at: -1}
			positions[t.StockID] = p
		}
		n, value := t.Quantity, t.ConvertedTotal
		if t.Type == "sell" {
			n, value = -n, -value
		}
		if n == 0 {
			continue
		}
		if p.qty != 0 && (p.qty > 0) != (n > 0) {
			closed := min(abs64(n), abs64(p.qty))
			costClosed := p.cost * closed / abs64(p.qty)
			valueClosed := value * closed / abs64(n)
			if p.at < 0 {
				p.at = len(out)
				out = append(out, StockPnL{Symbol: t.Symbol})
			}
			out[p.at].PnL -= costClosed + valueClosed
			p.cost -= costClosed
			value -= valueClosed
		}
		p.qty += n
		if p.qty == 0 {
			p.cost = 0
		} else {
			p.cost += value
		}
	}
	return out
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// SectorBreakdown returns sector allocation data (Feature 12).
func (h *Handler) SectorBreakdown(w http.ResponseWriter, r *http.Request) {
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
		setFlashCookie(w, "Join the game first", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d", gameID), http.StatusSeeOther)
		return
	}

	sectors, _ := h.q.ListHoldingsBySector(r.Context(), participant.ID)
	holdings, _ := h.q.ListHoldings(r.Context(), participant.ID)

	// Build gain/loss data (Feature 13).
	type HoldingPnL struct {
		db.ListHoldingsRow
		PnL      int64
		PnLPct   string
		LatestPx int64
	}
	var holdingsPnL []HoldingPnL
	for _, h_ := range holdings {
		pl := HoldingPnL{ListHoldingsRow: h_}
		if latestPrice, err := h.q.GetLatestPrice(r.Context(), h_.StockID); err == nil {
			pl.LatestPx = latestPrice.Close
			currentVal := h_.Quantity * latestPrice.Close
			pl.PnL = currentVal - h_.CostBasis
			if h_.CostBasis != 0 {
				// A short's cost is negative; the sign follows the gain.
				pct := float64(pl.PnL) / math.Abs(float64(h_.CostBasis)) * 100
				pl.PnLPct = fmt.Sprintf("%+.2f", pct)
			} else {
				pl.PnLPct = "0.00"
			}
		}
		holdingsPnL = append(holdingsPnL, pl)
	}

	// JSON for chart.
	type SectorData struct {
		Sector string  `json:"sector"`
		Value  float64 `json:"value"`
	}
	var chartData []SectorData
	for _, s := range sectors {
		name := s.Sector
		if name == "" {
			name = "Other"
		}
		val := float64(0)
		if s.TotalValue.Valid {
			val = s.TotalValue.Float64
		}
		chartData = append(chartData, SectorData{Sector: name, Value: val})
	}
	chartJSON, _ := json.Marshal(chartData)

	h.render(w, r, "analytics/sectors", "", PageData{
		Title: "Sector Analysis - " + game.Name,
		Item:  game,
		Items: holdingsPnL,
		Extra: map[string]any{
			"Participant": participant,
			"Sectors":     sectors,
			"ChartJSON":   string(chartJSON),
		},
	})
}

// PortfolioComparison compares two players side by side (Feature 9).
func (h *Handler) PortfolioComparison(w http.ResponseWriter, r *http.Request) {
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

	myParticipant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		setFlashCookie(w, "Join the game first", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d", gameID), http.StatusSeeOther)
		return
	}

	// Get the other player's participant ID from query.
	otherPID, _ := strconv.ParseInt(r.URL.Query().Get("vs"), 10, 64)

	participants, _ := h.q.ListParticipantsByGame(r.Context(), gameID)

	var otherHoldings []db.ListHoldingsRow
	var otherParticipant db.ListParticipantsByGameRow
	if otherPID > 0 {
		for _, p := range participants {
			if p.ID == otherPID {
				otherParticipant = p
				break
			}
		}
		if otherParticipant.ID != 0 {
			otherHoldings, _ = h.q.ListHoldings(r.Context(), otherPID)
		}
	}

	myHoldings, _ := h.q.ListHoldings(r.Context(), myParticipant.ID)

	// Get snapshots for both for chart overlay.
	mySnapshots, _ := h.q.ListSnapshots(r.Context(), db.ListSnapshotsParams{
		ParticipantID: myParticipant.ID,
		Date:          game.StartDate,
		Date_2:        game.EndDate,
	})

	var otherSnapshots []db.PortfolioSnapshot
	if otherPID > 0 {
		otherSnapshots, _ = h.q.ListSnapshots(r.Context(), db.ListSnapshotsParams{
			ParticipantID: otherPID,
			Date:          game.StartDate,
			Date_2:        game.EndDate,
		})
	}

	h.render(w, r, "analytics/compare", "", PageData{
		Title: "Compare - " + game.Name,
		Item:  game,
		Extra: map[string]any{
			"MyParticipant":    myParticipant,
			"MyHoldings":       myHoldings,
			"MySnapshots":      mySnapshots,
			"OtherParticipant": otherParticipant,
			"OtherHoldings":    otherHoldings,
			"OtherSnapshots":   otherSnapshots,
			"Participants":     participants,
			"SelectedVs":       otherPID,
		},
	})
}

// BenchmarkComparison overlays portfolio vs index (Feature 14).
func (h *Handler) BenchmarkComparison(w http.ResponseWriter, r *http.Request) {
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
		setFlashCookie(w, "Join the game first", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d", gameID), http.StatusSeeOther)
		return
	}

	snapshots, _ := h.q.ListSnapshots(r.Context(), db.ListSnapshotsParams{
		ParticipantID: participant.ID,
		Date:          game.StartDate,
		Date_2:        game.EndDate,
	})

	h.render(w, r, "analytics/benchmark", "", PageData{
		Title: "Benchmark - " + game.Name,
		Item:  game,
		Extra: map[string]any{
			"Participant": participant,
			"Snapshots":   snapshots,
		},
	})
}
