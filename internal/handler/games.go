package handler

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

// gameCard is one game on the Games page and the dashboard.
type gameCard struct {
	Game        db.Game
	Label       string          // "This quarter" or "Next game"; "" for past games
	Participant *db.Participant // nil when the user hasn't joined
	Players     int64
}

func (h *Handler) gameCard(ctx context.Context, g db.Game, label string, userID int64) gameCard {
	c := gameCard{Game: g, Label: label}
	c.Players, _ = h.q.CountParticipants(ctx, g.ID)
	if p, err := h.q.GetParticipantByGameAndUser(ctx, db.GetParticipantByGameAndUserParams{
		GameID: g.ID, UserID: userID,
	}); err == nil {
		c.Participant = &p
	}
	return c
}

// quarterCards are this quarter's game and the next one, for those the
// scheduler has created (see market.EnsureQuarterlyGames).
func (h *Handler) quarterCards(ctx context.Context, userID int64) []gameCard {
	this := market.QuarterOf(time.Now().In(h.loc))
	var cards []gameCard
	for _, c := range []struct {
		qtr   market.Quarter
		label string
	}{{this, "This quarter"}, {this.Next(), "Next game"}} {
		g, err := h.q.GetGameByQuarter(ctx, sql.NullString{String: c.qtr.Key(), Valid: true})
		if err != nil {
			continue
		}
		cards = append(cards, h.gameCard(ctx, g, c.label, userID))
	}
	return cards
}

// GamesList shows this quarter's game, the next one and every past game.
func (h *Handler) GamesList(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	finished, _ := h.q.ListFinishedGames(r.Context(), db.ListFinishedGamesParams{Limit: 200, Offset: 0})
	past := make([]gameCard, len(finished))
	for i, g := range finished {
		past[i] = h.gameCard(r.Context(), g, "", user.ID)
	}

	h.render(w, r, "games/list", "", PageData{
		Title: "Games",
		Items: h.quarterCards(r.Context(), user.ID),
		Extra: map[string]any{"Past": past},
	})
}

// GameDetail is a game's home page. Players get their portfolio; everyone
// else gets the game's rules and a Join button.
func (h *Handler) GameDetail(w http.ResponseWriter, r *http.Request) {
	h.gamePage(w, r, true)
}

// GameRules shows a game's rules and settings.
func (h *Handler) GameRules(w http.ResponseWriter, r *http.Request) {
	h.gamePage(w, r, false)
}

func (h *Handler) gamePage(w http.ResponseWriter, r *http.Request, portfolioIfJoined bool) {
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

	participant, _ := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: game.ID,
		UserID: user.ID,
	})
	if portfolioIfJoined && participant.ID != 0 {
		h.portfolio(w, r, game, participant)
		return
	}

	count, _ := h.q.CountParticipants(r.Context(), game.ID)
	isJoined := participant.ID != 0

	canJoin := !isJoined && (game.Status == "pending" || game.Status == "active")
	if canJoin && game.MaxParticipants.Valid && count >= game.MaxParticipants.Int64 {
		canJoin = false
	}

	h.render(w, r, "games/detail", "", PageData{
		Title: game.Name,
		Item:  game,
		Extra: map[string]any{
			"Participant": participant,
			"Count":       count,
			"IsAdmin":     user.IsAdmin == 1,
			"IsJoined":    isJoined,
			"CanJoin":     canJoin,
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
				CurrentValue:  0, // revaluation will update
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
