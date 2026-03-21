package handler

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func generateInviteCode() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Handler) InviteCreate(w http.ResponseWriter, r *http.Request) {
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

	// Only creator can make invites.
	if game.CreatedBy != user.ID && user.IsAdmin == 0 {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	code := generateInviteCode()
	var maxUses sql.NullInt64
	if v := r.FormValue("max_uses"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			maxUses = sql.NullInt64{Int64: n, Valid: true}
		}
	}

	var expiresAt sql.NullString
	if v := r.FormValue("expires_at"); v != "" {
		expiresAt = sql.NullString{String: v, Valid: true}
	}

	_, err = h.q.CreateInvite(r.Context(), db.CreateInviteParams{
		GameID:    gameID,
		Code:      code,
		CreatedBy: user.ID,
		MaxUses:   maxUses,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		setFlashCookie(w, "Failed to create invite", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d", gameID), http.StatusSeeOther)
		return
	}

	setFlashCookie(w, "Invite link created!", "success")
	http.Redirect(w, r, fmt.Sprintf("/games/%d", gameID), http.StatusSeeOther)
}

func (h *Handler) InviteJoin(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	code := r.PathValue("code")

	invite, err := h.q.GetInviteByCode(r.Context(), code)
	if err != nil {
		setFlashCookie(w, "Invalid invite link", "error")
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	// Check uses.
	if invite.MaxUses.Valid && invite.UseCount >= invite.MaxUses.Int64 {
		setFlashCookie(w, "This invite has been fully used", "error")
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	game, err := h.q.GetGame(r.Context(), invite.GameID)
	if err != nil {
		setFlashCookie(w, "Game not found", "error")
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	if game.Status != "pending" && game.Status != "active" {
		setFlashCookie(w, "Game is no longer accepting participants", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d", game.ID), http.StatusSeeOther)
		return
	}

	// Try to join.
	_, err = h.q.JoinGame(r.Context(), db.JoinGameParams{
		GameID:         game.ID,
		UserID:         user.ID,
		CashBalance:    game.StartingBalance,
		PortfolioValue: game.StartingBalance,
	})
	if err != nil {
		// Already joined.
		setFlashCookie(w, "You're already in this game!", "info")
	} else {
		_ = h.q.IncrementInviteUse(r.Context(), invite.ID)
		setFlashCookie(w, "Joined "+game.Name+"!", "success")
	}

	http.Redirect(w, r, fmt.Sprintf("/games/%d", game.ID), http.StatusSeeOther)
}
