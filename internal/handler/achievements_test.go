package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// achievementsDB has Alice in game 1, the achievements and the avatars.
func achievementsDB(t *testing.T) (*Handler, *db.Queries) {
	t.Helper()
	d := openTestDB(t)
	if _, err := d.Exec(`
		INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x', 'Alice', 't1');
		INSERT INTO games (id, created_by, name, status, start_date, end_date) VALUES
			(1, 1, 'October 2026', 'active', '2026-10-01', '2026-10-31');
		INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 1, 0, 0);`); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	SeedAchievements(context.Background(), q)
	SeedAvatars(context.Background(), q)
	return New(q, d, nil, false, time.UTC), q
}

func asAlice(t *testing.T, q *db.Queries, r *http.Request) *http.Request {
	t.Helper()
	user, err := q.GetUser(r.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	return r.WithContext(auth.WithUser(r.Context(), &user))
}

// TestEarnedAvatarCanBeChosen: achievements are earned in a game, and the
// avatar they unlock can be picked from the profile page in any game.
func TestEarnedAvatarCanBeChosen(t *testing.T) {
	h, q := achievementsDB(t)
	ctx := context.Background()
	var crown db.Avatar
	avatars, _ := q.ListAvatars(ctx)
	for _, a := range avatars {
		if a.Key == "crown" {
			crown = a
		}
	}
	if crown.ID == 0 {
		t.Fatal("no crown avatar")
	}
	choose := func() int64 {
		form := url.Values{"avatar_id": {fmt.Sprint(crown.ID)}}
		r := httptest.NewRequest("POST", "/profile/avatar", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.ProfileUpdateAvatar(httptest.NewRecorder(), asAlice(t, q, r))
		u, _ := q.GetUser(ctx, 1)
		return u.AvatarID.Int64
	}

	if got := choose(); got != 0 {
		t.Fatalf("crown chosen before Champion was earned")
	}
	h.grantIfNew(ctx, 1, "first_place")
	if got := choose(); got != crown.ID {
		t.Errorf("avatar = %d, want the crown (%d) once Champion is earned", got, crown.ID)
	}
}

// TestMarketWatcher: the 10th stock on the watchlist earns it.
func TestMarketWatcher(t *testing.T) {
	h, q := achievementsDB(t)
	ctx := context.Background()
	watcher, _ := q.GetAchievementByKey(ctx, "watchlist_10")
	for i := int64(1); i <= 10; i++ {
		if err := q.InsertStock(ctx, db.InsertStockParams{
			Symbol: fmt.Sprintf("S%d", i), Name: "Stock", Market: "sp500", Currency: "USD",
		}); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/", nil)
		r.SetPathValue("id", "1")
		r.SetPathValue("sid", fmt.Sprint(i))
		h.WatchlistAdd(httptest.NewRecorder(), asAlice(t, q, r))

		has, _ := q.HasAchievement(ctx, db.HasAchievementParams{UserID: 1, AchievementID: watcher.ID})
		if want := i == 10; (has > 0) != want {
			t.Fatalf("after %d stocks: has Market Watcher = %v, want %v", i, has > 0, want)
		}
	}
}

// TestBadgeEarnedOnce: a badge belongs to the player, so earning it again in
// another game neither adds a second one nor sends another notification.
func TestBadgeEarnedOnce(t *testing.T) {
	h, _ := achievementsDB(t)
	ctx := context.Background()
	h.grantIfNew(ctx, 1, "first_trade")
	h.grantIfNew(ctx, 1, "first_trade")

	var badges, notes int
	h.rawDB.QueryRow("SELECT COUNT(*) FROM user_achievements WHERE user_id = 1").Scan(&badges)
	h.rawDB.QueryRow("SELECT COUNT(*) FROM notifications WHERE user_id = 1 AND type = 'achievement'").Scan(&notes)
	if badges != 1 || notes != 1 {
		t.Errorf("%d badges and %d notifications, want 1 of each", badges, notes)
	}
}
