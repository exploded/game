package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
)

// Inviting friends. A player enters a friend's email on the game's home page
// and we email them a link to /invite/{code}. The link remembers the code in
// a cookie; if the friend then signs up, they join the game and the player
// who invited them gets the game's referral bonus. Existing accounts don't
// earn a bonus, and each player earns it once per game.
const (
	inviteCookie        = "invite"
	inviteCookieAge     = 30 * 24 * 60 * 60 // seconds
	maxInvitesPerDay    = 10                // per player, so the site can't be used to spam
	maxReferralsPerGame = 1                 // paid referrals per player per game
)

// invitesOpen reports whether players can invite friends to the game: it
// must pay a bonus and still be taking players.
func invitesOpen(g db.Game) bool {
	return g.ReferralBonusPct > 0 && (g.Status == "pending" || g.Status == "active")
}

// referralBonus is what a player earns for a friend who signs up.
func referralBonus(g db.Game) int64 {
	return g.StartingBalance * g.ReferralBonusPct / 100
}

// referralsUsed reports whether the user has had all the referral bonuses
// the game pays.
func (h *Handler) referralsUsed(ctx context.Context, gameID, userID int64) bool {
	n, err := h.q.CountReferralBonuses(ctx, db.CountReferralBonusesParams{ReferrerID: userID, GameID: gameID})
	return err != nil || n >= maxReferralsPerGame
}

// inviteEmail is the data for the invite email template.
type inviteEmail struct {
	From     string // the player's display name
	Game     string
	Code     string
	Starting string // the starting cash, formatted
}

// InviteSend emails a friend an invite to the game.
func (h *Handler) InviteSend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	game, err := h.q.GetGame(ctx, gameID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := h.q.GetParticipantByGameAndUser(ctx, db.GetParticipantByGameAndUserParams{
		GameID: game.ID, UserID: user.ID,
	}); err != nil {
		http.Error(w, "Join the game to invite friends.", http.StatusForbidden)
		return
	}
	home := fmt.Sprintf("/games/%d", game.ID)
	fail := func(msg string) {
		setFlashCookie(w, msg, "error")
		http.Redirect(w, r, home, http.StatusSeeOther)
	}
	if !invitesOpen(game) {
		fail("This game doesn't take invites.")
		return
	}
	if h.referralsUsed(ctx, game.ID, user.ID) {
		fail("You've had your referral bonus for this game. You can invite another friend in the next game.")
		return
	}

	email, ok := mail.NormalizeAddress(r.FormValue("email"))
	switch {
	case !ok:
		fail("Enter a valid email address, like name@example.com.")
		return
	case strings.EqualFold(email, user.Email):
		fail("That's your own address. Enter a friend's.")
		return
	}
	if _, err := h.q.GetUserByEmail(ctx, email); err == nil {
		fail(email + " already has a Wealth Lab account, so there's no bonus for inviting them.")
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		h.serverError(w, "invite: find user", err)
		return
	}
	if n, err := h.q.InviteSentTo(ctx, db.InviteSentToParams{
		GameID: game.ID, Email: sql.NullString{String: email, Valid: true},
	}); err != nil || n > 0 {
		fail(email + " has already been invited to this game.")
		return
	}
	if n, err := h.q.CountInvitesSentToday(ctx, user.ID); err != nil || n >= maxInvitesPerDay {
		fail(fmt.Sprintf("You can send %d invites a day. Try again tomorrow.", maxInvitesPerDay))
		return
	}

	code, err := auth.RandomHex(16)
	if err != nil {
		h.serverError(w, "invite code", err)
		return
	}
	if _, err := h.q.CreateInvite(ctx, db.CreateInviteParams{
		GameID:    game.ID,
		Code:      code,
		CreatedBy: user.ID,
		MaxUses:   sql.NullInt64{Int64: 1, Valid: true},
		Email:     sql.NullString{String: email, Valid: true},
	}); err != nil {
		h.serverError(w, "create invite", err)
		return
	}
	if err := h.mailer.ToAddress(ctx, email, "invite", inviteEmail{
		From: user.Name, Game: game.Name, Code: code, Starting: fmtDollars(game.StartingBalance),
	}); err != nil {
		h.serverError(w, "queue invite email", err)
		return
	}
	setFlashCookie(w, "Invite sent to "+email+".", "success")
	http.Redirect(w, r, home, http.StatusSeeOther)
}

// InviteLanding is where the emailed link lands. Opening it changes nothing
// (mail scanners fetch links): it remembers the invite and offers to sign
// up. Someone already signed in goes to the game.
func (h *Handler) InviteLanding(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	invite, err := h.q.GetInviteByCode(ctx, r.PathValue("code"))
	if err != nil {
		setFlashCookie(w, "That invite link doesn't work. You can still sign up here.", "error")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if auth.UserFromContext(ctx) != nil {
		http.Redirect(w, r, fmt.Sprintf("/games/%d", invite.GameID), http.StatusSeeOther)
		return
	}
	game, err := h.q.GetGame(ctx, invite.GameID)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	from, err := h.q.GetUser(ctx, invite.CreatedBy)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: inviteCookie, Value: invite.Code, Path: "/",
		MaxAge: inviteCookieAge, HttpOnly: true, Secure: auth.IsSecure(r), SameSite: http.SameSiteLaxMode,
	})
	h.render(w, r, "auth/login", "", PageData{
		Title: "Join " + from.Name + " on The Wealth Lab",
		Extra: map[string]any{
			"Email":     invite.Email.String,
			"InvitedBy": from.Name,
			"Game":      game.Name,
			"Starting":  fmtDollars(game.StartingBalance),
		},
	})
}

// takeInvite returns the invite code InviteLanding saved, and forgets it.
func takeInvite(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(inviteCookie)
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{Name: inviteCookie, Value: "", Path: "/", MaxAge: -1})
	return c.Value
}

// acceptInvite runs when a new account signs up from an invite: it joins
// them to the game and pays the player who invited them, unless that player
// has already had the game's referral bonus.
func (h *Handler) acceptInvite(ctx context.Context, code string, user db.User) {
	invite, err := h.q.GetInviteByCode(ctx, code)
	if err != nil || invite.CreatedBy == user.ID {
		return
	}
	game, err := h.q.GetGame(ctx, invite.GameID)
	if err != nil || !invitesOpen(game) {
		return
	}
	referrer, err := h.q.GetParticipantByGameAndUser(ctx, db.GetParticipantByGameAndUserParams{
		GameID: game.ID, UserID: invite.CreatedBy,
	})
	if err != nil {
		return // they've left the game
	}
	if _, err := h.joinGame(ctx, game, user.ID); err != nil {
		slog.Info("invite: can't join game", "game", game.ID, "user", user.ID, "error", err)
		return
	}

	bonus := referralBonus(game)
	tx, err := h.rawDB.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("invite: begin", "error", err)
		return
	}
	defer tx.Rollback()
	qtx := h.q.WithTx(tx)
	if n, err := qtx.UseInvite(ctx, invite.ID); err != nil || n == 0 {
		return
	}
	// Counted inside the transaction, so two friends signing up at once
	// can't both pay.
	if n, err := qtx.CountReferralBonuses(ctx, db.CountReferralBonusesParams{
		ReferrerID: invite.CreatedBy, GameID: game.ID,
	}); err != nil || n >= maxReferralsPerGame {
		if err := tx.Commit(); err != nil { // the invite still shows as joined
			slog.Error("invite: commit", "error", err)
		}
		return
	}
	if err := qtx.CreditCash(ctx, db.CreditCashParams{Amount: bonus, ID: referrer.ID}); err != nil {
		slog.Error("invite: credit bonus", "error", err)
		return
	}
	if err := qtx.InsertReferralBonus(ctx, db.InsertReferralBonusParams{
		InviteID: invite.ID, ReferrerID: invite.CreatedBy, ReferredID: user.ID,
		GameID: game.ID, BonusAmount: bonus,
	}); err != nil {
		slog.Error("invite: record bonus", "error", err)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("invite: commit", "error", err)
		return
	}

	friend := invite.Email.String
	if friend == "" { // a plain link, not an emailed invite
		friend = user.Name
	}
	_ = h.q.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  invite.CreatedBy,
		GameID:  nint64(game.ID),
		Type:    "referral_bonus",
		Title:   "Referral bonus",
		Message: fmt.Sprintf("You earned %s because %s signed up and joined %s.", fmtDollars(bonus), friend, game.Name),
	})
	_ = h.q.InsertActivity(ctx, db.InsertActivityParams{
		GameID: game.ID,
		UserID: invite.CreatedBy,
		Action: "referral",
		Detail: fmt.Sprintf("earned a %s referral bonus", fmtDollars(bonus)),
	})
	// Recruiter: 3 referrals, in any games.
	if n, _ := h.q.CountAllReferralBonuses(ctx, invite.CreatedBy); n >= 3 {
		h.grantIfNew(ctx, invite.CreatedBy, "referral_3", game.ID)
	}
}

// fmtDollars is fmtCents without the cents when they're zero: "$10,000".
func fmtDollars(c int64) string {
	return strings.TrimSuffix(fmtCents(c), ".00")
}
