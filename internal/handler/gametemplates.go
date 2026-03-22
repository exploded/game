package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) GameTemplatesList(w http.ResponseWriter, r *http.Request) {
	templates, _ := h.q.ListGameTemplates(r.Context())
	h.render(w, r, "templates/list", "", PageData{
		Title: "Game Templates",
		Items: templates,
	})
}

func (h *Handler) GameFromTemplate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	tmplID, err := strconv.ParseInt(r.PathValue("tid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	tmpl, err := h.q.GetGameTemplate(r.Context(), tmplID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	r.ParseForm()
	name := r.FormValue("name")
	if name == "" {
		name = tmpl.Name + " - " + time.Now().Format("Jan 2006")
	}

	startDate := r.FormValue("start_date")
	if startDate == "" {
		startDate = time.Now().In(h.loc).Format("2006-01-02")
	}
	endDate := r.FormValue("end_date")
	if endDate == "" {
		end := time.Now().In(h.loc).AddDate(0, 0, int(tmpl.DurationDays))
		endDate = end.Format("2006-01-02")
	}

	game, err := h.q.CreateGame(r.Context(), db.CreateGameParams{
		CreatedBy:        user.ID,
		Name:             name,
		Description:      tmpl.Description,
		Markets:          tmpl.Markets,
		StartingBalance:  tmpl.StartingBalance,
		BaseCurrency:     tmpl.BaseCurrency,
		StartDate:        startDate,
		EndDate:          endDate,
		AllowShort:       tmpl.AllowShort,
		TradeFee:         tmpl.TradeFee,
		ReferralBonusPct: 1,
	})
	if err != nil {
		setFlashCookie(w, "Failed to create game from template", "error")
		http.Redirect(w, r, "/templates", http.StatusSeeOther)
		return
	}

	// Auto-join the creator.
	_, _ = h.q.JoinGame(r.Context(), db.JoinGameParams{
		GameID:         game.ID,
		UserID:         user.ID,
		CashBalance:    game.StartingBalance,
		PortfolioValue: game.StartingBalance,
	})

	setFlashCookie(w, "Game created from template!", "success")
	http.Redirect(w, r, fmt.Sprintf("/games/%d", game.ID), http.StatusSeeOther)
}

func (h *Handler) GameTemplateCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	r.ParseForm()

	name := r.FormValue("name")
	if name == "" {
		setFlashCookie(w, "Template name is required", "error")
		http.Redirect(w, r, "/templates", http.StatusSeeOther)
		return
	}

	markets := r.FormValue("markets")
	if markets == "" {
		markets = "both"
	}
	baseCurrency := r.FormValue("base_currency")
	if baseCurrency == "" {
		baseCurrency = "AUD"
	}
	durationDays, _ := strconv.ParseInt(r.FormValue("duration_days"), 10, 64)
	if durationDays <= 0 {
		durationDays = 30
	}
	startingBalance := int64(100000000)
	if v := r.FormValue("starting_balance"); v != "" {
		if dollars, err := strconv.ParseInt(v, 10, 64); err == nil && dollars > 0 {
			startingBalance = dollars * 100
		}
	}
	tradeFee := int64(0)
	if v := r.FormValue("trade_fee"); v != "" {
		if dollars, err := strconv.ParseFloat(v, 64); err == nil && dollars >= 0 {
			tradeFee = int64(dollars * 100)
		}
	}
	allowShort := int64(0)
	if r.FormValue("allow_short") == "1" {
		allowShort = 1
	}

	var createdBy sql.NullInt64
	createdBy = sql.NullInt64{Int64: user.ID, Valid: true}

	_, err := h.q.CreateGameTemplate(r.Context(), db.CreateGameTemplateParams{
		Name:            name,
		Description:     r.FormValue("description"),
		Markets:         markets,
		StartingBalance: startingBalance,
		BaseCurrency:    baseCurrency,
		DurationDays:    durationDays,
		AllowShort:      allowShort,
		TradeFee:        tradeFee,
		IsBuiltin:       0,
		CreatedBy:       createdBy,
	})
	if err != nil {
		setFlashCookie(w, "Failed to create template", "error")
	} else {
		setFlashCookie(w, "Template created!", "success")
	}
	http.Redirect(w, r, "/templates", http.StatusSeeOther)
}
