package handler

import (
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) Portfolio(w http.ResponseWriter, r *http.Request) {
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
		setFlashCookie(w, "You haven't joined this game", "error")
		http.Redirect(w, r, "/games/"+r.PathValue("id"), http.StatusSeeOther)
		return
	}

	holdings, _ := h.q.ListHoldings(r.Context(), participant.ID)
	snapshots, _ := h.q.ListSnapshots(r.Context(), db.ListSnapshotsParams{
		ParticipantID: participant.ID,
		Date:          game.StartDate,
		Date_2:        game.EndDate,
	})

	h.render(w, r, "portfolio/index", "", PageData{
		Title: "Portfolio - " + game.Name,
		Item:  game,
		Items: holdings,
		Extra: map[string]any{
			"Participant": participant,
			"Snapshots":   snapshots,
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
	http.Redirect(w, r, "/games/"+r.PathValue("id")+"/portfolio", http.StatusSeeOther)
}
