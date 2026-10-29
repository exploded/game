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
	m, err := mail.New(q, f, mail.Config{BaseURL: "https://game.example/", NewsReplyTo: "rob@example.com", NewsSender: "Rob, PO Box 1, Melbourne VIC 3000"})
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
	if login.Subject != "Your StockGame sign-in code: 123456" {
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
	if news.ReplyTo != "rob@example.com" {
		t.Errorf("News reply-to = %q, want Rob's address", news.ReplyTo)
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
