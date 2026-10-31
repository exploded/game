package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
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
	Label       string          // "This month" or "Next game"; "" for past games
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

// monthCards are this month's game and the next one, for those the
// scheduler has created (see market.EnsureMonthlyGames).
func (h *Handler) monthCards(ctx context.Context, userID int64) []gameCard {
	this := market.MonthOf(time.Now().In(h.loc))
	var cards []gameCard
	for _, c := range []struct {
		month market.Month
		label string
	}{{this, "This month"}, {this.Next(), "Next game"}} {
		g, err := h.q.GetGameByMonth(ctx, sql.NullString{String: c.month.Key(), Valid: true})
		if err != nil {
			continue
		}
		cards = append(cards, h.gameCard(ctx, g, c.label, userID))
	}
	return cards
}

// GamesList shows this month's game, the next one and every past game.
func (h *Handler) GamesList(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	finished, _ := h.q.ListFinishedGames(r.Context(), db.ListFinishedGamesParams{Limit: 200, Offset: 0})
	past := make([]gameCard, len(finished))
	for i, g := range finished {
		past[i] = h.gameCard(r.Context(), g, "", user.ID)
	}

	h.render(w, r, "games/list", "", PageData{
		Title: "Games",
		Items: h.monthCards(r.Context(), user.ID),
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
			"CanJoin":     canJoin,
		},
	})
}

// errGameClosed and errGameFull are why joinGame can't add a player.
var (
	errGameClosed = errors.New("game not accepting players")
	errGameFull   = errors.New("game full")
)

// joinGame adds the user to the game with its starting cash and any
// starting stocks, and posts it to the game's activity feed.
func (h *Handler) joinGame(ctx context.Context, game db.Game, userID int64) (db.Participant, error) {
	if game.Status != "pending" && game.Status != "active" {
		return db.Participant{}, errGameClosed
	}
	if game.MaxParticipants.Valid {
		if count, _ := h.q.CountParticipants(ctx, game.ID); count >= game.MaxParticipants.Int64 {
			return db.Participant{}, errGameFull
		}
	}

	participant, err := h.q.JoinGame(ctx, db.JoinGameParams{
		GameID:         game.ID,
		UserID:         userID,
		CashBalance:    game.StartingBalance,
		PortfolioValue: game.StartingBalance,
	})
	if err != nil {
		return db.Participant{}, err
	}

	// Grant starting stocks if configured.
	startingStocks, _ := h.q.ListStartingStocks(ctx, game.ID)
	for _, ss := range startingStocks {
		_ = h.q.UpsertHolding(ctx, db.UpsertHoldingParams{
			ParticipantID: participant.ID,
			StockID:       ss.StockID,
			Quantity:      ss.Quantity,
			AvgCost:       0, // given for free
			CostBasis:     0,
			CurrentValue:  0, // revaluation will update
		})
	}

	_ = h.q.InsertActivity(ctx, db.InsertActivityParams{
		GameID: game.ID,
		UserID: userID,
		Action: "join",
		Detail: "joined the game",
	})
	return participant, nil
}

// isCurrentGame reports whether g is this month's game, which every player
// is in without having to join.
func (h *Handler) isCurrentGame(g db.Game) bool {
	return g.Quarter.Valid && g.Quarter.String == market.MonthOf(time.Now().In(h.loc)).Key()
}

// currentGame is this month's game, with the user joined to it. It's false
// until the scheduler has created the game.
func (h *Handler) currentGame(ctx context.Context, userID int64) (db.Game, bool) {
	month := market.MonthOf(time.Now().In(h.loc))
	g, err := h.q.GetGameByMonth(ctx, sql.NullString{String: month.Key(), Valid: true})
	if err != nil {
		return db.Game{}, false
	}
	if _, err := h.q.GetParticipantByGameAndUser(ctx, db.GetParticipantByGameAndUserParams{
		GameID: g.ID, UserID: userID,
	}); errors.Is(err, sql.ErrNoRows) {
		if _, err := h.joinGame(ctx, g, userID); err != nil {
			slog.Error("join current game", "game", g.ID, "user", userID, "error", err)
		}
	}
	return g, true
}

// GameJoin joins a game that isn't this month's, such as the next one.
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

	switch _, err := h.joinGame(r.Context(), game, user.ID); {
	case errors.Is(err, errGameClosed):
		setFlashCookie(w, "This game is not accepting participants", "error")
	case errors.Is(err, errGameFull):
		setFlashCookie(w, "Game is full", "error")
	case err != nil:
		setFlashCookie(w, "Already joined or error", "error")
	default:
		setFlashCookie(w, "Joined game!", "success")
	}
	http.Redirect(w, r, fmt.Sprintf("/games/%d", id), http.StatusSeeOther)
}
