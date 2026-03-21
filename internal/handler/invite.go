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

		// Referral bonus: credit the invite creator.
		if game.ReferralBonusPct > 0 && invite.CreatedBy != user.ID {
			bonusAmount := game.StartingBalance * game.ReferralBonusPct / 100
			referrerPart, refErr := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
				GameID: game.ID, UserID: invite.CreatedBy,
			})
			if refErr == nil && bonusAmount > 0 {
				_ = h.q.UpdateCashBalance(r.Context(), db.UpdateCashBalanceParams{
					CashBalance: referrerPart.CashBalance + bonusAmount,
					ID:          referrerPart.ID,
				})
				_ = h.q.UpdatePortfolioValue(r.Context(), db.UpdatePortfolioValueParams{
					PortfolioValue: referrerPart.PortfolioValue + bonusAmount,
					ID:             referrerPart.ID,
				})
				_ = h.q.InsertReferralBonus(r.Context(), db.InsertReferralBonusParams{
					InviteID:    invite.ID,
					ReferrerID:  invite.CreatedBy,
					ReferredID:  user.ID,
					GameID:      game.ID,
					BonusAmount: bonusAmount,
				})
				_ = h.q.CreateNotification(r.Context(), db.CreateNotificationParams{
					UserID:  invite.CreatedBy,
					GameID:  nint64(game.ID),
					Type:    "referral_bonus",
					Title:   "Referral Bonus!",
					Message: fmt.Sprintf("You earned %s when %s joined %s!", fmtCents(bonusAmount), user.Name, game.Name),
				})
				_ = h.q.InsertActivity(r.Context(), db.InsertActivityParams{
					GameID:   game.ID,
					UserID:   invite.CreatedBy,
					Action:   "referral",
					Detail:   fmt.Sprintf("earned %s referral bonus", fmtCents(bonusAmount)),
					IsPublic: 1,
				})

				// Check recruiter achievement (3+ referrals in one game).
				refCount, _ := h.q.CountReferralBonuses(r.Context(), db.CountReferralBonusesParams{
					ReferrerID: invite.CreatedBy, GameID: game.ID,
				})
				if refCount >= 3 {
					h.grantIfNew(r.Context(), invite.CreatedBy, "referral_3", game.ID)
				}
			}
		}

		setFlashCookie(w, "Joined "+game.Name+"!", "success")
	}

	http.Redirect(w, r, fmt.Sprintf("/games/%d", game.ID), http.StatusSeeOther)
}
