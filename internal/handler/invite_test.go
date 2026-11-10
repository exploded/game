package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/market"
	"github.com/go-chi/chi/v5"
)

// inviteServer seeds an active game (id 1, 1% referral bonus on $1M) that
// Alice (user 1) plays, and Bob (user 2), who has an account but hasn't
// joined. X-User picks the signed-in user; without it, nobody is. It serves
// the invite and sign-in routes as main.go does.
func inviteServer(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	d := openTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token) VALUES (1, 'alice@example.com', 'Alice', 't1'), (2, 'bob@example.com', 'Bob', 't2')`,
		`INSERT INTO games (id, created_by, name, status, start_date, end_date, referral_bonus_pct)
		 VALUES (1, 1, 'Spring Cup', 'active', '2026-01-01', '2099-12-31', 1)`,
		`INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 100000000, 100000000)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	q := db.New(d)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	m, err := mail.New(q, mail.Log{}, mail.Config{BaseURL: "https://game.example", NewsSender: "Consortium Private Wealth, Horsham VIC"})
	if err != nil {
		t.Fatal(err)
	}
	h.SetMailer(m)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := r.Header.Get("X-User"); id != "" {
				user, err := q.GetUserByEmail(r.Context(), map[string]string{"1": "alice@example.com", "2": "bob@example.com"}[id])
				if err != nil {
					t.Fatalf("GetUser: %v", err)
				}
				r = r.WithContext(auth.WithUser(r.Context(), &user))
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Post("/login", h.LoginSubmit)
	r.Post("/login/code", h.LoginCode)
	r.Get("/invite/{code}", h.InviteLanding)
	r.Get("/invite/{code}/stop", h.InviteStopPage)
	r.Post("/invite/{code}/stop", h.InviteStop)
	r.Get("/admin/email", h.AdminEmail)
	r.Post("/admin/email/optouts", h.AdminInviteOptoutAdd)
	r.Post("/admin/email/optouts/delete", h.AdminInviteOptoutDelete)
	r.Route("/games/{id}", func(r chi.Router) {
		r.Use(h.GameContext)
		r.Get("/", h.GameDetail)
		r.Post("/invite", h.InviteSend)
	})
	return r, d
}

// invite sends an invite to email as user, and returns the flash message.
func invite(t *testing.T, srv http.Handler, user, email string) string {
	t.Helper()
	req := httptest.NewRequest("POST", "/games/1/invite", strings.NewReader(url.Values{"email": {email}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-User", user)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/games/1" {
		t.Fatalf("invite %s: %d to %q, want 303 to /games/1\n%s", email, w.Code, w.Header().Get("Location"), w.Body)
	}
	if c := cookie(w, "flash"); c != nil {
		return c.Value
	}
	return ""
}

var inviteRE = regexp.MustCompile(`/invite/([0-9a-f]{32})`)

// openInvite emails an invite from Alice and opens its link signed out. It
// returns the invite cookie.
func openInvite(t *testing.T, srv http.Handler, d *sql.DB, email string) *http.Cookie {
	t.Helper()
	if msg := invite(t, srv, "1", email); !strings.HasPrefix(msg, "success|") {
		t.Fatalf("invite %s: %q", email, msg)
	}
	var body string
	if err := d.QueryRow("SELECT text_body FROM email_outbox WHERE template = 'invite' AND to_email = ?", email).Scan(&body); err != nil {
		t.Fatalf("no invite email to %s: %v", email, err)
	}
	m := inviteRE.FindStringSubmatch(body)
	if m == nil || !strings.Contains(body, "Alice invited you to join the Spring Cup share game") ||
		!strings.Contains(body, "To stop all invitations: https://game.example/invite/"+m[1]+"/stop\n\nSent by Consortium Private Wealth, Horsham VIC.") {
		t.Fatalf("invite email:\n%s", body)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/invite/"+m[1], nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Alice invited you to The Investor Project") || !strings.Contains(w.Body.String(), `value="`+email+`"`) {
		t.Fatalf("invite link: %d\n%s", w.Code, w.Body)
	}
	c := cookie(w, inviteCookie)
	if c == nil || c.Value != m[1] {
		t.Fatal("invite link didn't remember the invite")
	}
	return c
}

func cash(t *testing.T, d *sql.DB, userID int64) int64 {
	t.Helper()
	var c int64
	if err := d.QueryRow("SELECT cash_balance FROM participants WHERE game_id = 1 AND user_id = ?", userID).Scan(&c); err != nil {
		t.Fatalf("cash for user %d: %v", userID, err)
	}
	return c
}

func TestInviteFriendPaysBonusOnSignUp(t *testing.T) {
	srv, d := inviteServer(t)
	inv := openInvite(t, srv, d, "friend@example.com")

	req, code, _ := requestLogin(t, srv, d, "friend@example.com")
	w := post(srv, "/login/code", url.Values{"code": {code}}, req, inv)
	if w.Header().Get("Location") != "/welcome" {
		t.Fatalf("sign up: %d to %q, want /welcome", w.Code, w.Header().Get("Location"))
	}
	if got, want := cash(t, d, 1), int64(100000000+1000000); got != want {
		t.Errorf("Alice's cash = %d, want %d (a 1%% bonus)", got, want)
	}
	var friendID int64
	d.QueryRow("SELECT id FROM users WHERE email = 'friend@example.com'").Scan(&friendID)
	if got := cash(t, d, friendID); got != 100000000 {
		t.Errorf("friend's cash = %d, want the starting $1M", got)
	}
	var bonuses, notes int
	d.QueryRow("SELECT COUNT(*) FROM referral_bonuses WHERE referrer_id = 1 AND referred_id = ?", friendID).Scan(&bonuses)
	d.QueryRow("SELECT COUNT(*) FROM notifications WHERE user_id = 1 AND type = 'referral_bonus'").Scan(&notes)
	if bonuses != 1 || notes != 1 {
		t.Errorf("%d bonus rows and %d notifications, want 1 of each", bonuses, notes)
	}

	// Alice's home page shows the friend joined.
	body := getPage(t, srv, "/games/1", "1")
	if !strings.Contains(body, "Invite a friend") || !strings.Contains(body, "friend@example.com") || !strings.Contains(body, "Joined") {
		t.Error("home page doesn't list the joined invite")
	}
}

func TestInviteNoBonusForExistingAccount(t *testing.T) {
	srv, d := inviteServer(t)
	inv := openInvite(t, srv, d, "new@example.com")

	// Bob already has an account; signing in through the link pays nothing.
	req, code, _ := requestLogin(t, srv, d, "bob@example.com")
	w := post(srv, "/login/code", url.Values{"code": {code}}, req, inv)
	if w.Header().Get("Location") != "/dashboard" {
		t.Fatalf("sign in: %d to %q", w.Code, w.Header().Get("Location"))
	}
	if got := cash(t, d, 1); got != 100000000 {
		t.Errorf("Alice's cash = %d, want no bonus", got)
	}
}

func TestInviteRules(t *testing.T) {
	srv, d := inviteServer(t)
	for _, tc := range []struct{ user, email, want string }{
		{"1", "not an email", "Enter a valid email"},
		{"1", "Alice@Example.com", "your own address"},
		{"1", "bob@example.com", "already has an Investor Project account"},
		{"1", "friend@example.com", "success|Invite sent to friend@example.com."},
		{"1", "other@example.com", "used your invite"},
	} {
		if got := invite(t, srv, tc.user, tc.email); !strings.Contains(got, tc.want) {
			t.Errorf("invite %q: flash %q, want %q", tc.email, got, tc.want)
		}
	}

	// Bob isn't in the game.
	req := httptest.NewRequest("POST", "/games/1/invite", strings.NewReader("email=x%40example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-User", "2")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("non-player invite: %d, want 403", w.Code)
	}

	// Once Bob plays, he can't invite the friend Alice invited.
	d.Exec("INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value) VALUES (1, 2, 100000000, 100000000)")
	if got := invite(t, srv, "2", "FRIEND@example.com"); !strings.Contains(got, "already been invited") {
		t.Errorf("invite someone already invited: flash %q", got)
	}

	// No bonus, no invites.
	d.Exec("UPDATE games SET referral_bonus_pct = 0 WHERE id = 1")
	if got := invite(t, srv, "1", "other@example.com"); !strings.Contains(got, "doesn't take invites") {
		t.Errorf("invite with no bonus: flash %q", got)
	}
	if body := getPage(t, srv, "/games/1", "1"); strings.Contains(body, "Invite a friend") {
		t.Error("home page offers invites when the bonus is 0")
	}
}

func TestOneInvitePerGame(t *testing.T) {
	srv, d := inviteServer(t)
	inv := openInvite(t, srv, d, "friend@example.com")

	// The invite is used up while the friend hasn't signed up yet.
	if got := invite(t, srv, "1", "other@example.com"); !strings.Contains(got, "used your invite") {
		t.Errorf("second invite: flash %q", got)
	}
	body := getPage(t, srv, "/games/1", "1")
	if !strings.Contains(body, "used your invite for this game") || strings.Contains(body, `action="/games/1/invite"`) {
		t.Error("home page still offers an invite after one was sent")
	}

	// The friend signs up later, and Alice is paid in this game.
	req, code, _ := requestLogin(t, srv, d, "friend@example.com")
	if w := post(srv, "/login/code", url.Values{"code": {code}}, req, inv); w.Header().Get("Location") != "/welcome" {
		t.Fatalf("sign up: %d to %q", w.Code, w.Header().Get("Location"))
	}
	if got, want := cash(t, d, 1), int64(100000000+1000000); got != want {
		t.Errorf("Alice's cash = %d, want %d", got, want)
	}
	body = getPage(t, srv, "/games/1", "1")
	if !strings.Contains(body, "Your friend signed up") || strings.Contains(body, `action="/games/1/invite"`) {
		t.Error("home page doesn't show the bonus was paid")
	}
}

// A friend who signs up after the game has ended joins this month's game,
// and Alice is paid there, on top of any bonus she's had in it already.
func TestInviteAfterGameEndsPaysCurrentGame(t *testing.T) {
	srv, d := inviteServer(t)
	inv := openInvite(t, srv, d, "friend@example.com")

	month := market.MonthOf(time.Now().UTC()).Key()
	for _, stmt := range []string{
		`UPDATE games SET status = 'finished', quarter = '2000-01' WHERE id = 1`,
		`INSERT INTO games (id, created_by, name, status, start_date, end_date, referral_bonus_pct, quarter)
		 VALUES (2, 1, 'This Month', 'active', '2026-01-01', '2099-12-31', 1, '` + month + `')`,
		`INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value) VALUES (2, 1, 101000000, 101000000)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}

	req, code, _ := requestLogin(t, srv, d, "friend@example.com")
	if w := post(srv, "/login/code", url.Values{"code": {code}}, req, inv); w.Header().Get("Location") != "/welcome" {
		t.Fatalf("sign up: %d to %q", w.Code, w.Header().Get("Location"))
	}
	var aliceCash, friendIn int64
	d.QueryRow("SELECT cash_balance FROM participants WHERE game_id = 2 AND user_id = 1").Scan(&aliceCash)
	d.QueryRow("SELECT COUNT(*) FROM participants p JOIN users u ON u.id = p.user_id WHERE p.game_id = 2 AND u.email = 'friend@example.com'").Scan(&friendIn)
	if want := int64(101000000 + 1000000); aliceCash != want || friendIn != 1 {
		t.Errorf("game 2: Alice's cash %d (want %d), friend joined %d (want 1)", aliceCash, want, friendIn)
	}
	if got := cash(t, d, 1); got != 100000000 {
		t.Errorf("Alice's cash in the ended game = %d, want it unchanged", got)
	}
}

// The stop link in an invite email keeps the friend from being invited again,
// by anyone. Opening the link changes nothing; its button does.
func TestInviteStopLink(t *testing.T) {
	srv, d := inviteServer(t)
	openInvite(t, srv, d, "friend@example.com")
	var code, stopURL string
	d.QueryRow("SELECT code FROM game_invites WHERE email = 'friend@example.com'").Scan(&code)
	d.QueryRow("SELECT unsubscribe_url FROM email_outbox WHERE template = 'invite'").Scan(&stopURL)
	if want := "https://game.example/invite/" + code + "/stop"; stopURL != want {
		t.Errorf("List-Unsubscribe URL = %q, want %q", stopURL, want)
	}
	optouts := func() int {
		var n int
		d.QueryRow("SELECT COUNT(*) FROM invite_optouts WHERE email = 'friend@example.com' AND source = 'link'").Scan(&n)
		return n
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/invite/"+code+"/stop", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Stop invitations?") || optouts() != 0 {
		t.Fatalf("opening the stop link: %d, %d opt-outs\n%s", w.Code, optouts(), w.Body)
	}
	// A mail client's RFC 8058 one-click POST, with no session.
	w = post(srv, "/invite/"+code+"/stop", url.Values{"List-Unsubscribe": {"One-Click"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "No more invitations") || optouts() != 1 {
		t.Fatalf("stop: %d, %d opt-outs\n%s", w.Code, optouts(), w.Body)
	}

	// Bob joins and tries to invite the same friend.
	d.Exec("INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value) VALUES (1, 2, 100000000, 100000000)")
	if got := invite(t, srv, "2", "Friend@Example.com"); !strings.Contains(got, "asked not to get invitations") {
		t.Errorf("invite after stop: flash %q", got)
	}
	var invites int
	d.QueryRow("SELECT COUNT(*) FROM game_invites WHERE created_by = 2").Scan(&invites)
	if invites != 0 {
		t.Error("the refused invite used up Bob's invite")
	}

	// A made-up code changes nothing.
	w = post(srv, "/invite/nope/stop", nil)
	if !strings.Contains(w.Body.String(), "This link doesn't work") {
		t.Errorf("bad stop link:\n%s", w.Body)
	}
}

// Admins add someone who replied to an invite, and can take them off again.
func TestAdminInviteOptouts(t *testing.T) {
	srv, d := inviteServer(t)
	for _, tc := range []struct{ email, want string }{
		{"nope", "Enter a valid email"},
		{" Replied@Example.com ", "success|replied@example.com won't be invited again."},
	} {
		w := post(srv, "/admin/email/optouts", url.Values{"email": {tc.email}})
		if c := cookie(w, "flash"); c == nil || !strings.Contains(c.Value, tc.want) {
			t.Errorf("add %q: flash %v, want %q", tc.email, c, tc.want)
		}
	}
	if body := getPage(t, srv, "/admin/email", "1"); !strings.Contains(body, "replied@example.com") || !strings.Contains(body, "Added by an admin") {
		t.Error("admin email page doesn't list the opt-out")
	}
	if got := invite(t, srv, "1", "replied@example.com"); !strings.Contains(got, "asked not to get invitations") {
		t.Errorf("invite an opted-out address: flash %q", got)
	}

	post(srv, "/admin/email/optouts/delete", url.Values{"email": {"replied@example.com"}})
	var n int
	d.QueryRow("SELECT COUNT(*) FROM invite_optouts").Scan(&n)
	if n != 0 {
		t.Errorf("%d opt-outs after removing, want 0", n)
	}
	if got := invite(t, srv, "1", "replied@example.com"); !strings.HasPrefix(got, "success|") {
		t.Errorf("invite after removing the opt-out: flash %q", got)
	}
}
