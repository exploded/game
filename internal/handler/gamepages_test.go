package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/db"
)

// TestRetiredStockHasNoTicket checks a retired stock's page says it can't be
// traded, with no ticket, Buy/Sell bar or Watch button, while a holder still
// sees their position.
func TestRetiredStockHasNoTicket(t *testing.T) {
	srv, q, d := gameServerDB(t)
	p, _ := q.GetParticipantByGameAndUser(t.Context(), db.GetParticipantByGameAndUserParams{GameID: 1, UserID: 1})
	for _, stmt := range []string{
		`UPDATE stocks SET is_active = 0 WHERE id = 1`,
		`INSERT INTO holdings (participant_id, stock_id, quantity, avg_cost, cost_basis, current_value)
		 VALUES (?1, 1, 10, 1000, 10000, 10500)`,
	} {
		if _, err := d.Exec(stmt, p.ID); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}

	body := getPage(t, srv, "/games/1/stocks/1", "")
	if !strings.Contains(body, "ACME is no longer available to trade.") {
		t.Error("a retired stock's page doesn't say it can't be traded")
	}
	if !strings.Contains(body, "Your position") {
		t.Error("a holder of a retired stock can't see their position")
	}
	for _, unwanted := range []string{`id="ticket-form"`, `class="trade-bar"`, "/watchlist/1"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("a retired stock's page has %q", unwanted)
		}
	}
}

// TestHistoryRestoreRunsGameContext checks Back's history restore, which
// htmx sends with HX-Request, gets the whole game context (the top bar's
// cash, last_game_id), while other htmx requests and the chart's bars fetch
// skip it.
func TestHistoryRestoreRunsGameContext(t *testing.T) {
	srv, q, _ := gameServerDB(t)
	lastGame := func() bool {
		u, err := q.GetUser(t.Context(), 1)
		if err != nil {
			t.Fatal(err)
		}
		return u.LastGameID.Valid
	}
	get := func(path string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w
	}

	get("/games/1/stocks/1/bars", nil)
	if lastGame() {
		t.Error("the chart's bars fetch ran GameContext")
	}
	get("/games/1/stocks?f=all", map[string]string{"HX-Request": "true"})
	if lastGame() {
		t.Error("an htmx fragment request ran GameContext")
	}
	w := get("/games/1/stocks?f=all", map[string]string{"HX-Request": "true", "HX-History-Restore-Request": "true"})
	if !lastGame() {
		t.Error("a history restore didn't run GameContext")
	}
	if !strings.Contains(w.Body.String(), `id="game-bar-stats"`) {
		t.Error("a history restore's page has no cash in the top bar")
	}
}

// TestNotificationsPagesAndMarkRead checks the notifications page links to
// older ones past 25, and Mark Read sends the item back read with the bell's
// new count out of band.
func TestNotificationsPagesAndMarkRead(t *testing.T) {
	_, q, d := gameServerDB(t)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	for i := 0; i < 27; i++ {
		if err := q.CreateNotification(t.Context(), db.CreateNotificationParams{
			UserID: 1, Type: "info", Title: "Note", Message: "Hello",
		}); err != nil {
			t.Fatal(err)
		}
	}

	page := func(query string) string {
		r := asAlice(t, q, httptest.NewRequest("GET", "/notifications"+query, nil))
		w := httptest.NewRecorder()
		h.NotificationsPage(w, r)
		return w.Body.String()
	}
	first := page("")
	if !strings.Contains(first, `href="/notifications?page=2"`) || strings.Contains(first, "Newer") {
		t.Error("page 1 doesn't link to page 2, or links to a newer page")
	}
	second := page("?page=2")
	if !strings.Contains(second, `href="/notifications?page=1"`) || strings.Contains(second, "Older") {
		t.Error("page 2 doesn't link back to page 1, or links to an older page")
	}
	if n := strings.Count(second, `class="notification-item`); n != 2 {
		t.Errorf("page 2 lists %d notifications, want 2", n)
	}
	if !strings.Contains(page("?page=99999999999999999"), "No older notifications") {
		t.Error("a huge page number doesn't show the empty page")
	}

	r := httptest.NewRequest("POST", "/notifications/1/read", nil)
	r.Header.Set("HX-Request", "true")
	r.SetPathValue("nid", "1")
	w := httptest.NewRecorder()
	h.NotificationMarkRead(w, asAlice(t, q, r))
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("mark read: status %d", w.Code)
	}
	if !strings.Contains(body, `id="notif-1"`) || strings.Contains(body, "notification-unread") || strings.Contains(body, "Mark Read") {
		t.Errorf("mark read didn't send the notification back read:\n%s", body)
	}
	if !strings.Contains(body, `id="bell"`) || !strings.Contains(body, `hx-swap-oob="true"`) ||
		!strings.Contains(body, `<span class="bell-count">9+</span>`) || !strings.Contains(body, "26 unread") {
		t.Errorf("mark read didn't update the bell:\n%s", body)
	}
}

// TestStockSearch checks the empty portfolio's search: nothing for an empty
// query, rows for a match, and the stocks page without htmx.
func TestStockSearch(t *testing.T) {
	_, q, d := gameServerDB(t)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	search := func(query string, htmx bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/games/1/search?q="+url.QueryEscape(query), nil)
		if htmx {
			r.Header.Set("HX-Request", "true")
		}
		r.SetPathValue("id", "1")
		w := httptest.NewRecorder()
		h.StockSearch(w, asAlice(t, q, r))
		return w
	}

	if w := search("", true); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "" {
		t.Errorf("an empty search sent %d %q, want nothing", w.Code, w.Body.String())
	}
	if w := search("acme", true); !strings.Contains(w.Body.String(), "<strong>ACME</strong>") {
		t.Errorf("searching acme didn't find ACME:\n%s", w.Body.String())
	}
	if w := search("acme corp", false); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/games/1/stocks?q=acme+corp" {
		t.Errorf("a search without htmx sent %d to %q", w.Code, w.Header().Get("Location"))
	}
}

// TestLeaderboardHugePage checks a huge ?page= doesn't overflow into
// negative places.
func TestLeaderboardHugePage(t *testing.T) {
	srv, _ := gameServer(t)
	req := httptest.NewRequest("GET", "/games/1/leaderboard?page=9223372036854775807", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "rank-place\">-") {
		t.Error("a huge page shows negative places")
	}
}

// TestAdminGameRejectsHugeMoney checks the game settings form turns away
// amounts that would overflow the cents, and keeps the old settings.
func TestAdminGameRejectsHugeMoney(t *testing.T) {
	_, q, d := gameServerDB(t)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	if _, err := d.Exec(`DELETE FROM participants`); err != nil { // so the starting balance can change
		t.Fatal(err)
	}
	before, _ := q.GetGame(t.Context(), 1)
	for _, tt := range []struct{ field, value string }{
		{"starting_balance", "Inf"},
		{"starting_balance", "1e20"},
		{"starting_balance", "NaN"},
		{"trade_fee", "Inf"},
		{"trade_fee", "1e20"},
	} {
		form := url.Values{
			"name": {"Spring Cup"}, "starting_balance": {"100000"}, "base_currency": {"USD"},
			"trade_fee": {"0"}, "fx_fee": {"0.5"}, "referral_bonus_pct": {"0"}, "credit_interest_rate": {"0"},
		}
		form.Set(tt.field, tt.value)
		r := httptest.NewRequest("POST", "/admin/games/1", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetPathValue("gid", "1")
		w := httptest.NewRecorder()
		h.AdminGameUpdate(w, asAlice(t, q, r))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Settings not saved") {
			t.Errorf("%s=%s: status %d, no error shown", tt.field, tt.value, w.Code)
		}
		if g, _ := q.GetGame(t.Context(), 1); g.StartingBalance != before.StartingBalance || g.TradeFee != before.TradeFee {
			t.Errorf("%s=%s changed the game", tt.field, tt.value)
		}
	}
}

// TestAdminCourseCreateSlugError checks a database error while finding a
// free slug answers 500 instead of looping for ever.
func TestAdminCourseCreateSlugError(t *testing.T) {
	_, q, d := gameServerDB(t)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	r := asAlice(t, q, httptest.NewRequest("POST", "/admin/courses", strings.NewReader("title=Investing+101")))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	d.Close()
	done := make(chan int)
	go func() {
		w := httptest.NewRecorder()
		h.AdminCourseCreate(w, r)
		done <- w.Code
	}()
	select {
	case code := <-done:
		if code != http.StatusInternalServerError {
			t.Errorf("status %d, want 500", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AdminCourseCreate is still looping")
	}
}
