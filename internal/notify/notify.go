// Package notify sends the emails for things that happen in games: the
// in-app notifications (orders, dividends, splits, referral bonuses) and the
// game lifecycle (started, ending soon, final results). The scheduler runs
// it every minute. docs/emails.md lists the emails.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
)

// sendFromHour is the earliest local hour for game emails, so a game that
// starts or ends at midnight doesn't email everyone at midnight.
const sendFromHour = 8

// notificationCategory says which notifications become email, and in which
// category. Types not listed (achievements) stay in-app only.
var notificationCategory = map[string]mail.Category{
	"order_filled":    mail.Trades,
	"order_cancelled": mail.Trades,
	"order_expired":   mail.Trades,
	"dividend":        mail.Trades,
	"stock_split":     mail.Trades,
	"referral_bonus":  mail.Games,
	"game_start":      mail.Games, // a recurring game's next round
}

// Run sends whatever is due. now is the current time and loc the game time
// zone.
func Run(ctx context.Context, q *db.Queries, m *mail.Mailer, now time.Time, loc *time.Location) {
	emailNotifications(ctx, q, m)
	local := now.In(loc)
	if local.Hour() >= sendFromHour {
		gameEmails(ctx, q, m, local)
	}
}

// update is one notification in the updates email.
type update struct {
	Title, Message, GameName, URL string
}

// updatesEmail is the data for updates.tmpl.
type updatesEmail struct {
	Category string
	Updates  []update
}

// emailNotifications emails each user's new notifications: one email per
// user and category, so five orders filling at the open make one email.
func emailNotifications(ctx context.Context, q *db.Queries, m *mail.Mailer) {
	rows, err := q.ListUnemailedNotifications(ctx)
	if err != nil {
		slog.Error("list unemailed notifications", "error", err)
		return
	}
	type key struct {
		user int64
		cat  mail.Category
	}
	groups := map[key][]update{}
	var order []key
	for _, n := range rows {
		if cat, ok := notificationCategory[n.Type]; ok {
			k := key{n.UserID, cat}
			if _, seen := groups[k]; !seen {
				order = append(order, k)
			}
			u := update{Title: n.Title, Message: n.Message, GameName: n.GameName}
			if n.GameID.Valid {
				u.URL = "/games/" + strconv.FormatInt(n.GameID.Int64, 10)
			}
			groups[k] = append(groups[k], u)
		}
		// Marked first: a failed render shouldn't resend the rest forever.
		if err := q.MarkNotificationEmailed(ctx, n.ID); err != nil {
			slog.Error("mark notification emailed", "id", n.ID, "error", err)
			return
		}
	}
	for _, k := range order {
		info, _ := mail.Info(k.cat)
		if _, err := m.ToUser(ctx, k.user, k.cat, "updates", updatesEmail{
			Category: info.Label, Updates: groups[k],
		}); err != nil {
			slog.Error("queue updates email", "user", k.user, "error", err)
		}
	}
}

// standing is one player's place in a game.
type standing struct {
	Name    string
	Rank    int
	Place   string // "2nd"
	Value   string
	Return  string
	Players int
}

// gameEmail is the data for the game_* templates.
type gameEmail struct {
	GameName string
	URL      string // the game's leaderboard
	EndDate  string // "Fri 31 Oct"
	Starting string // starting cash
	Tomorrow bool   // the last ending-soon email
	You      standing
	Top      []standing // the top 3
}

// gameEmails sends started, ending-soon and results emails, once each per
// game. Each look-back window is short, so games that were already running
// when this shipped don't send a burst of stale email.
func gameEmails(ctx context.Context, q *db.Queries, m *mail.Mailer, local time.Time) {
	day := func(offset int) string { return local.AddDate(0, 0, offset).Format(time.DateOnly) }

	started, err := q.ListActiveGamesStartedSince(ctx, day(-1))
	if err != nil {
		slog.Error("list started games", "error", err)
	}
	for _, g := range started {
		sendGameEmail(ctx, q, m, g, "started", "game_started")
	}
	for _, kind := range []struct {
		days int
		name string
	}{{3, "ending_3"}, {1, "ending_1"}} {
		games, err := q.ListActiveGamesEndingOn(ctx, day(kind.days))
		if err != nil {
			slog.Error("list ending games", "error", err)
		}
		for _, g := range games {
			sendGameEmail(ctx, q, m, g, kind.name, "game_ending")
		}
	}
	finished, err := q.ListFinishedGamesEndedSince(ctx, day(-3))
	if err != nil {
		slog.Error("list finished games", "error", err)
	}
	for _, g := range finished {
		sendGameEmail(ctx, q, m, g, "results", "game_results")
	}
}

// sendGameEmail claims kind for the game, then emails every player.
func sendGameEmail(ctx context.Context, q *db.Queries, m *mail.Mailer, g db.Game, kind, template string) {
	claimed, err := q.ClaimGameEmail(ctx, db.ClaimGameEmailParams{GameID: g.ID, Kind: kind})
	if err != nil || claimed == 0 {
		if err != nil {
			slog.Error("claim game email", "game", g.ID, "kind", kind, "error", err)
		}
		return
	}
	players, err := q.ListParticipantsByGame(ctx, g.ID)
	if err != nil {
		slog.Error("list participants", "game", g.ID, "error", err)
		return
	}
	standings := make([]standing, len(players))
	for i, p := range players {
		standings[i] = standing{
			Name:    p.UserName,
			Rank:    i + 1,
			Place:   ordinal(i + 1),
			Value:   money(p.PortfolioValue),
			Return:  returnPct(p.PortfolioValue, g.StartingBalance),
			Players: len(players),
		}
	}
	data := gameEmail{
		GameName: g.Name,
		URL:      fmt.Sprintf("/games/%d/leaderboard", g.ID),
		EndDate:  shortDate(g.EndDate),
		Starting: money(g.StartingBalance),
		Tomorrow: kind == "ending_1",
		Top:      standings[:min(3, len(standings))],
	}
	for i, p := range players {
		data.You = standings[i]
		if _, err := m.ToUser(ctx, p.UserID, mail.Games, template, data); err != nil {
			slog.Error("queue game email", "game", g.ID, "user", p.UserID, "error", err)
		}
	}
}

// money formats cents as whole dollars with thousands separators.
func money(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	s := strconv.FormatInt(cents/100, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + "$" + s
}

// returnPct is the gain on start as "+3.2%".
func returnPct(value, start int64) string {
	if start == 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%+.1f%%", float64(value-start)/float64(start)*100)
}

// ordinal turns 2 into "2nd".
func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return strconv.Itoa(n) + suffix
}

// shortDate turns "2026-10-31" into "Sat 31 Oct".
func shortDate(d string) string {
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return d
	}
	return t.Format("Mon 2 Jan")
}
