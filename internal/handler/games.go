package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) GamesList(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	active, _ := h.q.ListActiveGames(r.Context())
	myGames, _ := h.q.ListUserGames(r.Context(), user.ID)

	// Build a set of game IDs the user has joined.
	joined := make(map[int64]bool)
	for _, g := range myGames {
		joined[g.ID] = true
	}

	h.render(w, r, "games/list", "", PageData{
		Title: "Games",
		Items: active,
		Extra: map[string]any{
			"MyGames": myGames,
			"Joined":  joined,
		},
	})
}

func (h *Handler) GameNew(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "games/create", "", PageData{
		Title: "Create Game",
	})
}

func (h *Handler) GameCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	r.ParseForm()

	name := r.FormValue("name")
	description := r.FormValue("description")
	markets := r.FormValue("markets")
	baseCurrency := r.FormValue("base_currency")
	startDate := r.FormValue("start_date")
	endDate := r.FormValue("end_date")

	errors := make(map[string]string)
	if name == "" {
		errors["name"] = "Name is required"
	}
	if markets == "" {
		markets = "both"
	}
	if baseCurrency == "" {
		baseCurrency = "AUD"
	}
	if startDate == "" {
		errors["start_date"] = "Start date is required"
	}
	if endDate == "" {
		errors["end_date"] = "End date is required"
	}
	if startDate >= endDate {
		errors["end_date"] = "End date must be after start date"
	}

	startingBalance := int64(100000000) // $1,000,000
	if v := r.FormValue("starting_balance"); v != "" {
		if dollars, err := strconv.ParseInt(v, 10, 64); err == nil && dollars > 0 {
			startingBalance = dollars * 100
		}
	}

	var maxParticipants sql.NullInt64
	if v := r.FormValue("max_participants"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			maxParticipants = sql.NullInt64{Int64: n, Valid: true}
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

	if len(errors) > 0 {
		h.render(w, r, "games/create", "", PageData{
			Title:  "Create Game",
			Errors: errors,
		})
		return
	}

	var recurringInterval sql.NullString
	if v := r.FormValue("recurring_interval"); v != "" {
		recurringInterval = sql.NullString{String: v, Valid: true}
	}

	referralBonusPct := int64(1)
	if v := r.FormValue("referral_bonus_pct"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 && n <= 10 {
			referralBonusPct = n
		}
	}

	game, err := h.q.CreateGame(r.Context(), db.CreateGameParams{
		CreatedBy:          user.ID,
		Name:               name,
		Description:        description,
		Markets:            markets,
		StartingBalance:    startingBalance,
		BaseCurrency:       baseCurrency,
		MaxParticipants:    maxParticipants,
		StartDate:          startDate,
		EndDate:            endDate,
		AllowShort:         allowShort,
		TradeFee:           tradeFee,
		RecurringInterval:  recurringInterval,
		ReferralBonusPct:   referralBonusPct,
	})
	if err != nil {
		h.render(w, r, "games/create", "", PageData{
			Title:  "Create Game",
			Errors: map[string]string{"name": "Failed to create game"},
		})
		return
	}

	// Auto-join the creator.
	_, _ = h.q.JoinGame(r.Context(), db.JoinGameParams{
		GameID:      game.ID,
		UserID:      user.ID,
		CashBalance: game.StartingBalance,
		PortfolioValue: game.StartingBalance,
	})

	setFlashCookie(w, "Game created!", "success")
	http.Redirect(w, r, fmt.Sprintf("/games/%d", game.ID), http.StatusSeeOther)
}

func (h *Handler) GameDetail(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.q.GetGame(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	participants, _ := h.q.ListPublicParticipantsByGame(r.Context(), game.ID)
	count, _ := h.q.CountParticipants(r.Context(), game.ID)
	participant, _ := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: game.ID,
		UserID: user.ID,
	})

	creator, _ := h.q.GetUser(r.Context(), game.CreatedBy)

	isCreator := game.CreatedBy == user.ID
	isJoined := participant.ID != 0

	canJoin := !isJoined && (game.Status == "pending" || game.Status == "active")
	if canJoin && game.MaxParticipants.Valid && count >= game.MaxParticipants.Int64 {
		canJoin = false
	}

	h.render(w, r, "games/detail", "", PageData{
		Title: game.Name,
		Item:  game,
		Items: participants,
		Extra: map[string]any{
			"Participant":  participant,
			"Count":        count,
			"IsCreator":    isCreator,
			"IsJoined":     isJoined,
			"CanJoin":      canJoin,
			"Creator":      creator,
		},
	})
}

func (h *Handler) GameJoin(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.q.GetGame(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if game.Status != "pending" && game.Status != "active" {
		setFlashCookie(w, "This game is not accepting participants", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d", id), http.StatusSeeOther)
		return
	}

	// Check max participants.
	if game.MaxParticipants.Valid {
		count, _ := h.q.CountParticipants(r.Context(), game.ID)
		if count >= game.MaxParticipants.Int64 {
			setFlashCookie(w, "Game is full", "error")
			http.Redirect(w, r, fmt.Sprintf("/games/%d", id), http.StatusSeeOther)
			return
		}
	}

	participant, err := h.q.JoinGame(r.Context(), db.JoinGameParams{
		GameID:         game.ID,
		UserID:         user.ID,
		CashBalance:    game.StartingBalance,
		PortfolioValue: game.StartingBalance,
	})
	if err != nil {
		setFlashCookie(w, "Already joined or error", "error")
	} else {
		// Grant starting stocks if configured.
		startingStocks, _ := h.q.ListStartingStocks(r.Context(), game.ID)
		for _, ss := range startingStocks {
			_ = h.q.UpsertHolding(r.Context(), db.UpsertHoldingParams{
				ParticipantID: participant.ID,
				StockID:       ss.StockID,
				Quantity:      ss.Quantity,
				AvgCost:       0, // given for free
			})
		}

		// Record join in activity feed.
		_ = h.q.InsertActivity(r.Context(), db.InsertActivityParams{
			GameID:   game.ID,
			UserID:   user.ID,
			Action:   "join",
			Detail:   "joined the game",
			IsPublic: 1,
		})

		setFlashCookie(w, "Joined game!", "success")
	}
	http.Redirect(w, r, fmt.Sprintf("/games/%d", id), http.StatusSeeOther)
}

func (h *Handler) GameCancel(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.q.GetGame(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if game.CreatedBy != user.ID && user.IsAdmin == 0 {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	_ = h.q.UpdateGameStatus(r.Context(), db.UpdateGameStatusParams{Status: "cancelled", ID: game.ID})
	setFlashCookie(w, "Game cancelled", "info")
	http.Redirect(w, r, "/games", http.StatusSeeOther)
}
