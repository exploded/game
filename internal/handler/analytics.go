package handler

import (
	"encoding/json"
	"fmt"
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

	// Compute analytics from transactions.
	type StockTrade struct {
		Symbol     string
		TotalBuy   int64 // total cost in base currency
		TotalSell  int64 // total proceeds in base currency
		QtyBought  int64
		QtySold    int64
	}

	stocks := make(map[int64]*StockTrade)
	var totalBuys, totalSells int64
	var buyCount, sellCount int64

	for _, t := range txns {
		st, ok := stocks[t.StockID]
		if !ok {
			st = &StockTrade{Symbol: t.Symbol}
			stocks[t.StockID] = st
		}
		if t.Type == "buy" {
			st.TotalBuy += t.ConvertedTotal
			st.QtyBought += t.Quantity
			totalBuys += t.ConvertedTotal
			buyCount++
		} else {
			st.TotalSell += t.ConvertedTotal
			st.QtySold += t.Quantity
			totalSells += t.ConvertedTotal
			sellCount++
		}
	}

	// Compute per-stock P&L for closed positions.
	type StockPnL struct {
		Symbol string
		PnL    int64
	}
	var closedTrades []StockPnL
	var winners, losers int
	var bestTrade, worstTrade StockPnL
	var totalPnL int64

	for _, st := range stocks {
		if st.QtySold == 0 {
			continue
		}
		// Approximate P&L based on average cost.
		avgBuy := int64(0)
		if st.QtyBought > 0 {
			avgBuy = st.TotalBuy / st.QtyBought
		}
		pnl := st.TotalSell - (avgBuy * st.QtySold)
		spnl := StockPnL{Symbol: st.Symbol, PnL: pnl}
		closedTrades = append(closedTrades, spnl)
		totalPnL += pnl

		if pnl > 0 {
			winners++
		} else if pnl < 0 {
			losers++
		}
		if len(closedTrades) == 1 || pnl > bestTrade.PnL {
			bestTrade = spnl
		}
		if len(closedTrades) == 1 || pnl < worstTrade.PnL {
			worstTrade = spnl
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
		PnL       int64
		PnLPct    string
		LatestPx  int64
	}
	var holdingsPnL []HoldingPnL
	for _, h_ := range holdings {
		pl := HoldingPnL{ListHoldingsRow: h_}
		if latestPrice, err := h.q.GetLatestPrice(r.Context(), h_.StockID); err == nil {
			pl.LatestPx = latestPrice.Close
			costBasis := h_.Quantity * h_.AvgCost
			currentVal := h_.Quantity * latestPrice.Close
			pl.PnL = currentVal - costBasis
			if costBasis > 0 {
				pct := float64(pl.PnL) / float64(costBasis) * 100
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

	participants, _ := h.q.ListPublicParticipantsByGame(r.Context(), gameID)

	var otherHoldings []db.ListHoldingsRow
	var otherParticipant db.ListPublicParticipantsByGameRow
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
