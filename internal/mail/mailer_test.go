package mail_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/handler"
	"github.com/exploded/game/internal/mail"
)

// fakeSender records messages, failing while fail is set.
type fakeSender struct {
	mu   sync.Mutex
	sent []mail.Message
	fail error
}

func (f *fakeSender) Send(_ context.Context, m mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, m)
	return nil
}

func newMailer(t *testing.T) (*mail.Mailer, *db.Queries, *fakeSender) {
	t.Helper()
	d, err := handler.OpenDB(filepath.Join(t.TempDir(), "test.db"), game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x.com', 'Alice', 'tok1')`); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	f := &fakeSender{}
	m, err := mail.New(q, f, mail.Config{BaseURL: "https://game.example/", ReplyTo: "info@example.com", NewsSender: "Rob, PO Box 1, Melbourne VIC 3000"})
	if err != nil {
		t.Fatal(err)
	}
	return m, q, f
}

// runOnce starts the worker, waits until nothing is due, and stops it.
func runOnce(t *testing.T, m *mail.Mailer, q *db.Queries) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		due, err := q.ListDueEmails(context.Background(), 1)
		if err == nil && len(due) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestEveryTemplateRenders(t *testing.T) {
	m, q, f := newMailer(t)
	ctx := context.Background()
	if err := m.ToAddress(ctx, "new@x.com", "login", map[string]any{"Token": "abc", "Code": "123456", "Minutes": 15}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"welcome", "account_deleted"} {
		if _, err := m.ToUser(ctx, 1, mail.Account, name, nil); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	runOnce(t, m, q)
	if len(f.sent) != 3 {
		t.Fatalf("sent %d emails, want 3", len(f.sent))
	}
	login := f.sent[0]
	if login.Subject != "Your Investor Project sign-in code: 123456" {
		t.Errorf("login subject = %q", login.Subject)
	}
	for _, body := range []string{login.Text, login.HTML} {
		if !strings.Contains(body, "https://game.example/login/verify?t=abc") {
			t.Errorf("login email has no sign-in link:\n%s", body)
		}
	}
	if login.UnsubscribeURL != "" {
		t.Error("an account email has an unsubscribe link")
	}
	for _, e := range f.sent {
		if e.ReplyTo != "info@example.com" {
			t.Errorf("%q reply-to = %q, want Consortium's address", e.Subject, e.ReplyTo)
		}
	}
	if !strings.Contains(f.sent[1].Text, "Hi Alice,") {
		t.Errorf("welcome email doesn't greet by name:\n%s", f.sent[1].Text)
	}
}

func TestToUserRespectsPrefs(t *testing.T) {
	m, q, f := newMailer(t)
	ctx := context.Background()

	// News is opt-in, so nothing is queued until the user turns it on.
	if queued, err := m.ToUser(ctx, 1, mail.News, "welcome", nil); err != nil || queued {
		t.Fatalf("News before opting in: queued %v, err %v", queued, err)
	}
	if err := q.SetEmailPref(ctx, db.SetEmailPrefParams{UserID: 1, Category: "news", Enabled: 1}); err != nil {
		t.Fatal(err)
	}
	if queued, err := m.ToUser(ctx, 1, mail.News, "welcome", nil); err != nil || !queued {
		t.Fatalf("News after opting in: queued %v, err %v", queued, err)
	}
	// Games is on by default; turning it off stops it.
	if err := q.SetEmailPref(ctx, db.SetEmailPrefParams{UserID: 1, Category: "games", Enabled: 0}); err != nil {
		t.Fatal(err)
	}
	if queued, _ := m.ToUser(ctx, 1, mail.Games, "welcome", nil); queued {
		t.Error("Games email queued after turning Games off")
	}

	runOnce(t, m, q)
	if len(f.sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(f.sent))
	}
	news := f.sent[0]
	if news.ReplyTo != "info@example.com" {
		t.Errorf("News reply-to = %q, want Consortium's address", news.ReplyTo)
	}
	want := "https://game.example/email/unsubscribe?c=news&t=tok1"
	if news.UnsubscribeURL != want || !strings.Contains(news.Text, want) {
		t.Errorf("unsubscribe URL = %q, want %q in the header and the text", news.UnsubscribeURL, want)
	}
}

func TestFailedSendRetriesLater(t *testing.T) {
	m, q, f := newMailer(t)
	ctx := context.Background()
	f.fail = errors.New("throttled")
	if _, err := m.ToUser(ctx, 1, mail.Account, "welcome", nil); err != nil {
		t.Fatal(err)
	}
	runOnce(t, m, q) // returns once the failed email is no longer due

	if len(f.sent) != 0 {
		t.Errorf("sent %d emails while SES was failing", len(f.sent))
	}
	// Make it due again: the retry goes out once SES recovers.
	f.fail = nil
	if err := q.MarkEmailRetry(ctx, db.MarkEmailRetryParams{LastError: "throttled", SendAfter: "2000-01-01 00:00:00", ID: 1}); err != nil {
		t.Fatal(err)
	}
	runOnce(t, m, q)
	if len(f.sent) != 1 {
		t.Errorf("sent %d emails after SES recovered, want 1", len(f.sent))
	}
}

// A sign-in email goes ahead of bulk email already queued, or a game email
// to every player would hold up links that expire in 15 minutes.
func TestAccountEmailGoesFirst(t *testing.T) {
	m, q, _ := newMailer(t)
	ctx := context.Background()
	for range 3 {
		if _, err := m.ToUser(ctx, 1, mail.Games, "welcome", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.ToAddress(ctx, "new@x.com", "login", map[string]any{"Token": "abc", "Code": "123456", "Minutes": 15}); err != nil {
		t.Fatal(err)
	}
	due, err := q.ListDueEmails(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 3 || due[0].Template != "login" {
		t.Fatalf("first due: %d emails, first %q; want the login first", len(due), due[0].Template)
	}
	if due[1].Category != "games" || due[1].ID > due[2].ID {
		t.Errorf("bulk email after it isn't oldest first: ids %d, %d", due[1].ID, due[2].ID)
	}
}

// TestQueuedEmailRecheckedAtSend: what changed after an email was queued
// counts when it's sent. Unsubscribing or being blocked stops everything
// but Account email; deleting the account stops everything but its
// confirmation.
func TestQueuedEmailRecheckedAtSend(t *testing.T) {
	d, err := handler.OpenDB(filepath.Join(t.TempDir(), "test.db"), game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO users (id, email, name, email_token) VALUES
		(1, 'a@x.com', 'Alice', 't1'), (2, 'b@x.com', 'Bob', 't2'), (3, 'c@x.com', 'Cat', 't3')`); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	f := &fakeSender{}
	m, err := mail.New(q, f, mail.Config{BaseURL: "https://game.example"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for id := int64(1); id <= 3; id++ {
		for _, c := range []mail.Category{mail.Games, mail.Account} {
			if _, err := m.ToUser(ctx, id, c, "welcome", nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Queued while Cat still had an account, as AccountDelete does.
	if _, err := m.ToUser(ctx, 3, mail.Account, "account_deleted", nil); err != nil {
		t.Fatal(err)
	}
	q.SetEmailPref(ctx, db.SetEmailPrefParams{UserID: 1, Category: "games", Enabled: 0})
	q.BlockUser(ctx, 2)
	q.SoftDeleteUser(ctx, 3)

	runOnce(t, m, q)
	var got []string
	for _, s := range f.sent {
		got = append(got, s.To+" "+s.Subject)
	}
	rows, err := d.Query(`SELECT to_email, category, template, status FROM email_outbox ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var statuses []string
	for rows.Next() {
		var to, cat, tpl, status string
		rows.Scan(&to, &cat, &tpl, &status)
		statuses = append(statuses, to+" "+cat+" "+tpl+" "+status)
	}
	want := []string{
		"a@x.com games welcome cancelled",
		"a@x.com account welcome sent",
		"b@x.com games welcome cancelled",
		"b@x.com account welcome sent",
		"c@x.com games welcome cancelled",
		"c@x.com account welcome cancelled",
		"c@x.com account account_deleted sent",
	}
	if strings.Join(statuses, "\n") != strings.Join(want, "\n") {
		t.Errorf("outbox:\n%s\nwant:\n%s\n(sent %v)", strings.Join(statuses, "\n"), strings.Join(want, "\n"), got)
	}
}

// cancellingSender stands in for SIGTERM arriving while SES is sending: it
// cancels the worker's context, then fails if its own context was
// cancelled with it.
type cancellingSender struct {
	cancel context.CancelFunc
	sent   int
}

func (c *cancellingSender) Send(ctx context.Context, _ mail.Message) error {
	c.cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.sent++
	return nil
}

// TestShutdownMidSendMarksSent: an email handed to SES as shutdown starts
// is still marked sent, so it isn't sent again after the restart, and the
// worker stops before the next one.
func TestShutdownMidSendMarksSent(t *testing.T) {
	_, q, _ := newMailer(t)
	ctx, cancel := context.WithCancel(context.Background())
	s := &cancellingSender{cancel: cancel}
	m, err := mail.New(q, s, mail.Config{BaseURL: "https://game.example"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := m.ToUser(context.Background(), 1, mail.Account, "welcome", nil); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker didn't stop")
	}
	if s.sent != 1 {
		t.Fatalf("sent %d, want 1", s.sent)
	}
	due, err := q.ListDueEmails(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Attempts != 0 {
		t.Errorf("after shutdown: %d due (want 1, the unsent one, untried)", len(due))
	}
}
