package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/exploded/game/internal/db"
)

func TestAccountDeleteRemovesPersonalData(t *testing.T) {
	srv, d := siteServer(t)
	for _, stmt := range []string{
		`INSERT INTO login_requests (id, email, token_hash, code_hash, expires_at) VALUES
			('a', 'alice@x.com', 'ta', 'c', datetime('now', '+1 hour')), ('b', 'bob@x.com', 'tb', 'c', datetime('now', '+1 hour'))`,
		`INSERT INTO email_outbox (user_id, to_email, category, template, subject, text_body, html_body, status) VALUES
			(2, 'alice@x.com', 'games', 'game_started', 's', 't', 'h', 'pending'),
			(2, 'alice@x.com', 'account', 'welcome', 's', 't', 'h', 'sent'),
			(NULL, 'alice@x.com', 'account', 'login', 's', 't', 'h', 'pending'),
			(3, 'bob@x.com', 'games', 'game_started', 's', 't', 'h', 'pending')`,
		`INSERT INTO game_invites (game_id, code, created_by, max_uses, email) VALUES (1, 'c1', 3, 1, 'alice@x.com')`,
		`INSERT INTO watchlist (user_id, stock_id) VALUES (2, 1), (3, 1)`,
		`INSERT INTO notifications (user_id, type, title, message) VALUES (2, 'achievement', 'T', 'M'), (3, 'achievement', 'T', 'M')`,
		`INSERT INTO live_events (id, platform, url, title) VALUES (1, 'youtube', 'https://youtu.be/x', 'Live')`,
		`INSERT INTO live_reminders (event_id, user_id) VALUES (1, 2), (1, 3)`,
		`INSERT INTO lobby_messages (user_id, message) VALUES (2, 'Hello from Alice')`,
		`INSERT INTO questions (user_id, title) VALUES (2, 'What is an ETF?')`,
		`INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total) VALUES (1, 1, 'buy', 1, 100, 100, 100)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}

	if w := siteRequest(srv, 2, "POST", "/settings/delete", url.Values{"confirm_delete": {"DELETE"}}, true); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("delete: %d to %q\n%s", w.Code, w.Header().Get("Location"), w.Body)
	}

	var name, email string
	d.QueryRow("SELECT name, email FROM users WHERE id = 2").Scan(&name, &email)
	if name != "Deleted User" || email != "deleted-2@deleted.local" {
		t.Errorf("user 2 is %q <%s>", name, email)
	}
	for _, c := range []struct {
		what, query string
		want        int
	}{
		{"rows anywhere with Alice's address", `SELECT
			(SELECT COUNT(*) FROM login_requests WHERE email = 'alice@x.com') +
			(SELECT COUNT(*) FROM email_outbox WHERE to_email = 'alice@x.com' AND template != 'account_deleted') +
			(SELECT COUNT(*) FROM game_invites WHERE email = 'alice@x.com') +
			(SELECT COUNT(*) FROM users WHERE email = 'alice@x.com')`, 0},
		{"Alice's emails", "SELECT COUNT(*) FROM email_outbox WHERE user_id = 2", 0},
		{"the account_deleted email", "SELECT COUNT(*) FROM email_outbox WHERE to_email = 'alice@x.com' AND template = 'account_deleted' AND status = 'pending'", 1},
		{"Bob's invite, anonymised", "SELECT COUNT(*) FROM game_invites WHERE created_by = 3 AND email = 'deleted-2@deleted.local'", 1},
		{"Alice's watchlist, notifications, reminders and sessions", `SELECT
			(SELECT COUNT(*) FROM watchlist WHERE user_id = 2) + (SELECT COUNT(*) FROM notifications WHERE user_id = 2) +
			(SELECT COUNT(*) FROM live_reminders WHERE user_id = 2) + (SELECT COUNT(*) FROM sessions WHERE user_id = 2)`, 0},
		{"Bob's rows", `SELECT
			(SELECT COUNT(*) FROM login_requests WHERE email = 'bob@x.com') + (SELECT COUNT(*) FROM email_outbox WHERE user_id = 3) +
			(SELECT COUNT(*) FROM watchlist WHERE user_id = 3) + (SELECT COUNT(*) FROM notifications WHERE user_id = 3) +
			(SELECT COUNT(*) FROM live_reminders WHERE user_id = 3) + (SELECT COUNT(*) FROM sessions WHERE user_id = 3)`, 6},
		{"Alice's trades, lobby message and question", `SELECT
			(SELECT COUNT(*) FROM transactions) + (SELECT COUNT(*) FROM lobby_messages WHERE user_id = 2) +
			(SELECT COUNT(*) FROM questions WHERE user_id = 2)`, 3},
	} {
		var got int
		if err := d.QueryRow(c.query).Scan(&got); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		if got != c.want {
			t.Errorf("%s: %d, want %d", c.what, got, c.want)
		}
	}

	// What she wrote now shows as from Deleted User.
	msgs, err := db.New(d).ListLobbyMessages(t.Context(), db.ListLobbyMessagesParams{ShowPosted: 1, Limit: 10})
	if err != nil || len(msgs) != 1 || msgs[0].UserName != "Deleted User" {
		t.Errorf("lobby message: %+v, %v", msgs, err)
	}
	// Her old cookie no longer signs in.
	if w := siteRequest(srv, 2, "GET", "/dashboard", nil, false); w.Code != http.StatusTemporaryRedirect {
		t.Errorf("old session after deleting: %d", w.Code)
	}
}

func TestExportPersonalData(t *testing.T) {
	srv, d := siteServer(t)
	for _, stmt := range []string{
		`INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total) VALUES (1, 1, 'buy', 3, 100, 300, 300)`,
		`INSERT INTO watchlist (user_id, stock_id) VALUES (2, 1)`,
		`INSERT INTO lobby_messages (user_id, message) VALUES (2, 'Hello from Alice'), (3, 'Hello from Bob')`,
		`INSERT INTO questions (user_id, title) VALUES (2, 'What is an ETF?')`,
		`INSERT INTO contact_messages (user_id, name, email, message) VALUES (2, 'Alice', 'alice@x.com', 'A contact message')`,
		`INSERT INTO game_invites (game_id, code, created_by, max_uses, email) VALUES (1, 'c1', 2, 1, 'friend@x.com')`,
		`INSERT INTO notifications (user_id, type, title, message) VALUES (2, 'achievement', 'Badge', 'You earned a badge')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	w := siteRequest(srv, 2, "GET", "/settings/export", nil, false)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d %q", w.Code, w.Header().Get("Content-Disposition"))
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"user":             "alice@x.com",
		"participations":   "Cup",
		"transactions":     "AAPL",
		"watchlist":        "Apple",
		"lobby_messages":   "Hello from Alice",
		"questions":        "What is an ETF?",
		"contact_messages": "A contact message",
		"invites_sent":     "friend@x.com",
		"notifications":    "You earned a badge",
	} {
		if !strings.Contains(string(data[key]), want) {
			t.Errorf("export %s = %s, want it to have %q", key, data[key], want)
		}
	}
	if strings.Contains(w.Body.String(), "Hello from Bob") {
		t.Error("the export has another player's message")
	}
}
