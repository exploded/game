package handler

import (
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/exploded/game/internal/db"
)

// AdminGames lists every game, newest first. The scheduler creates one per
// month; admins change their settings here.
func (h *Handler) AdminGames(w http.ResponseWriter, r *http.Request) {
	games, err := h.q.ListGamesWithPlayers(r.Context())
	if err != nil {
		h.serverError(w, "list games", err)
		return
	}
	h.render(w, r, "admin/games", "", PageData{Title: "Games", Items: games})
}

// adminGame loads the game in the URL, or answers 404.
func (h *Handler) adminGame(w http.ResponseWriter, r *http.Request) (db.Game, bool) {
	id, err := strconv.ParseInt(r.PathValue("gid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return db.Game{}, false
	}
	g, err := h.q.GetGame(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return db.Game{}, false
	}
	return g, true
}

func (h *Handler) renderAdminGame(w http.ResponseWriter, r *http.Request, g db.Game, errs map[string]string) {
	players, _ := h.q.CountParticipants(r.Context(), g.ID)
	h.render(w, r, "admin/game", "", PageData{
		Title:  g.Name,
		Item:   g,
		Errors: errs,
		Extra: map[string]any{
			"Players": players,
			// Everyone who joined got the old starting balance, in the old
			// currency.
			"MoneyLocked": players > 0,
			"Editable":    g.Status == "pending" || g.Status == "active",
		},
	})
}

// AdminGame shows a game's settings form.
func (h *Handler) AdminGame(w http.ResponseWriter, r *http.Request) {
	if g, ok := h.adminGame(w, r); ok {
		h.renderAdminGame(w, r, g, nil)
	}
}

// AdminGameUpdate saves a game's settings. They apply straight away, to
// everyone in the game. Dates come from the month and can't change.
func (h *Handler) AdminGameUpdate(w http.ResponseWriter, r *http.Request) {
	g, ok := h.adminGame(w, r)
	if !ok {
		return
	}
	if g.Status != "pending" && g.Status != "active" {
		setFlashCookie(w, "A finished or cancelled game can't be changed", "error")
		http.Redirect(w, r, fmt.Sprintf("/admin/games/%d", g.ID), http.StatusSeeOther)
		return
	}
	r.ParseForm()
	errs := map[string]string{}

	p := db.UpdateGameSettingsParams{
		ID:               g.ID,
		Name:             strings.TrimSpace(r.FormValue("name")),
		Description:      strings.TrimSpace(r.FormValue("description")),
		StartingBalance:  g.StartingBalance,
		BaseCurrency:     g.BaseCurrency,
		MaxParticipants:  g.MaxParticipants,
		TradeFee:         g.TradeFee,
		ReferralBonusPct: g.ReferralBonusPct,
	}
	if p.Name == "" {
		errs["name"] = "Enter a name"
	}
	if r.FormValue("allow_short") == "1" {
		p.AllowShort = 1
	}
	if r.FormValue("limit_orders") == "1" {
		p.LimitOrders = 1
	}
	if r.FormValue("after_hours_trading") == "1" {
		p.AfterHoursTrading = 1
	}

	players, _ := h.q.CountParticipants(r.Context(), g.ID)
	if players == 0 {
		if dollars, err := strconv.ParseFloat(r.FormValue("starting_balance"), 64); err == nil && dollars >= 1000 {
			p.StartingBalance = int64(math.Round(dollars)) * 100
		} else {
			errs["starting_balance"] = "Enter at least $1,000"
		}
		if c := r.FormValue("base_currency"); c == "AUD" || c == "USD" {
			p.BaseCurrency = c
		} else {
			errs["base_currency"] = "Choose AUD or USD"
		}
	}

	if v := strings.TrimSpace(r.FormValue("max_participants")); v == "" {
		p.MaxParticipants = sql.NullInt64{}
	} else if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
		p.MaxParticipants = sql.NullInt64{Int64: n, Valid: true}
	} else {
		errs["max_participants"] = "Enter a number, or leave it blank for no limit"
	}
	if dollars, err := strconv.ParseFloat(r.FormValue("trade_fee"), 64); err == nil && dollars >= 0 {
		p.TradeFee = int64(math.Round(dollars * 100))
	} else {
		errs["trade_fee"] = "Enter a fee of $0 or more"
	}
	if pct, err := strconv.ParseFloat(r.FormValue("fx_fee"), 64); err == nil && pct >= 0 && pct <= 5 {
		p.FxFee = int64(math.Round(pct * 100))
	} else {
		errs["fx_fee"] = "Enter 0 to 5"
	}
	if n, err := strconv.ParseInt(r.FormValue("referral_bonus_pct"), 10, 64); err == nil && n >= 0 && n <= 10 {
		p.ReferralBonusPct = n
	} else {
		errs["referral_bonus_pct"] = "Enter 0 to 10"
	}
	if pct, err := strconv.ParseFloat(r.FormValue("credit_interest_rate"), 64); err == nil && pct >= 0 && pct <= 20 {
		p.CreditInterestRate = int64(math.Round(pct * 100))
	} else {
		errs["credit_interest_rate"] = "Enter 0 to 20"
	}

	if len(errs) > 0 {
		g.Name, g.Description = p.Name, p.Description
		h.renderAdminGame(w, r, g, errs)
		return
	}
	if err := h.q.UpdateGameSettings(r.Context(), p); err != nil {
		h.serverError(w, "update game settings", err)
		return
	}
	setFlashCookie(w, "Settings saved", "success")
	http.Redirect(w, r, fmt.Sprintf("/admin/games/%d", g.ID), http.StatusSeeOther)
}

// AdminGameCancel cancels a game. Its month gets no replacement.
func (h *Handler) AdminGameCancel(w http.ResponseWriter, r *http.Request) {
	g, ok := h.adminGame(w, r)
	if !ok {
		return
	}
	if g.Status != "pending" && g.Status != "active" {
		setFlashCookie(w, "That game has already ended", "error")
	} else if err := h.q.UpdateGameStatus(r.Context(), db.UpdateGameStatusParams{Status: "cancelled", ID: g.ID}); err != nil {
		h.serverError(w, "cancel game", err)
		return
	} else {
		setFlashCookie(w, "Game cancelled", "info")
	}
	http.Redirect(w, r, "/admin/games", http.StatusSeeOther)
}
