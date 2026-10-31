package mail

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"log/slog"
	"net/url"
	"path"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/exploded/game/internal/db"
)

// all: so the embed includes _layout.tmpl.
//
//go:embed all:templates
var templateFS embed.FS

const (
	sendRate    = 10 // emails a second, under the SES default of 14
	maxAttempts = 6  // then the email is marked failed
	batchSize   = 50
)

// Config holds the site details emails need.
type Config struct {
	BaseURL string // the site's public URL
	ReplyTo string // Reply-To on every email, so replies reach Consortium; optional
	// NewsSender names who sends News emails and how to reach them, such as
	// "Rob Smith, PO Box 1, Melbourne VIC 3000". The Spam Act needs it in
	// every marketing email, so News can't go out without it.
	NewsSender string
}

// Mailer renders emails into the outbox and a background worker sends them.
type Mailer struct {
	q      *db.Queries
	sender Sender
	cfg    Config
	text   map[string]*texttemplate.Template
	html   map[string]*htmltemplate.Template
	wake   chan struct{}
}

// New parses the email templates.
func New(q *db.Queries, sender Sender, cfg Config) (*Mailer, error) {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	m := &Mailer{
		q:      q,
		sender: sender,
		cfg:    cfg,
		text:   map[string]*texttemplate.Template{},
		html:   map[string]*htmltemplate.Template{},
		wake:   make(chan struct{}, 1),
	}
	funcs := map[string]any{"paragraphs": Paragraphs}
	textBase, err := texttemplate.New("").Funcs(funcs).ParseFS(templateFS, "templates/_layout.tmpl")
	if err != nil {
		return nil, err
	}
	htmlBase, err := htmltemplate.New("").Funcs(funcs).ParseFS(templateFS, "templates/_layout.tmpl")
	if err != nil {
		return nil, err
	}
	files, err := fs.Glob(templateFS, "templates/*.tmpl")
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".tmpl")
		if strings.HasPrefix(name, "_") {
			continue
		}
		t, err := texttemplate.Must(textBase.Clone()).ParseFS(templateFS, f)
		if err != nil {
			return nil, fmt.Errorf("email template %s: %w", name, err)
		}
		h, err := htmltemplate.Must(htmlBase.Clone()).ParseFS(templateFS, f)
		if err != nil {
			return nil, fmt.Errorf("email template %s: %w", name, err)
		}
		m.text[name], m.html[name] = t, h
	}
	return m, nil
}

// view is what every email template receives.
type view struct {
	BaseURL        string
	Name           string // the recipient's display name; empty for sign-in email
	UnsubscribeURL string
	CategoryLabel  string
	NewsSender     string // set on News emails
	D              any    // the email's own data
}

// render executes the subject, text and html blocks of one template.
func (m *Mailer) render(name string, v view) (subject, text, html string, err error) {
	t, ok := m.text[name]
	if !ok {
		return "", "", "", fmt.Errorf("no email template %q", name)
	}
	var s, tx, h bytes.Buffer
	if err := t.ExecuteTemplate(&s, "subject", v); err != nil {
		return "", "", "", err
	}
	if err := t.ExecuteTemplate(&tx, "text-layout", v); err != nil {
		return "", "", "", err
	}
	if err := m.html[name].ExecuteTemplate(&h, "html-layout", v); err != nil {
		return "", "", "", err
	}
	return oneLine(s.String()), tx.String(), h.String(), nil
}

// ToAddress queues an Account email to an address that may not belong to a
// user yet, such as a sign-in link.
func (m *Mailer) ToAddress(ctx context.Context, to, template string, data any) error {
	subject, text, html, err := m.render(template, view{BaseURL: m.cfg.BaseURL, D: data})
	if err != nil {
		return err
	}
	if err := m.q.QueueEmail(ctx, db.QueueEmailParams{
		ToEmail: to, Category: string(Account), Template: template,
		Subject: subject, TextBody: text, HtmlBody: html, ReplyTo: m.cfg.ReplyTo,
	}); err != nil {
		return err
	}
	m.Wake()
	return nil
}

// ToUser queues an email to a user, unless they've turned off its category.
// It reports whether the email was queued.
func (m *Mailer) ToUser(ctx context.Context, userID int64, c Category, template string, data any) (bool, error) {
	return m.toUser(ctx, userID, c, template, data, false)
}

// Test queues an email to a user whatever their settings, with "[Test]" in
// the subject: the admin page uses it to preview a newsletter.
func (m *Mailer) Test(ctx context.Context, userID int64, c Category, template string, data any) error {
	_, err := m.toUser(ctx, userID, c, template, data, true)
	return err
}

// ErrNoNewsSender stops News going out without the sender details the Spam
// Act requires.
var ErrNoNewsSender = errors.New("NEWS_SENDER isn't set")

// NewsReady reports whether News emails can be sent.
func (m *Mailer) NewsReady() bool { return m.cfg.NewsSender != "" }

// Sender returns NEWS_SENDER, for other emails that must say who sent them,
// such as an invite to someone who isn't a user.
func (m *Mailer) Sender() string { return m.cfg.NewsSender }

func (m *Mailer) toUser(ctx context.Context, userID int64, c Category, template string, data any, test bool) (bool, error) {
	if c == News && !m.NewsReady() {
		return false, ErrNoNewsSender
	}
	user, err := m.q.GetUser(ctx, userID)
	if err != nil {
		return false, err
	}
	if user.DeletedAt.Valid || user.BlockedAt.Valid {
		return false, nil
	}
	v := view{BaseURL: m.cfg.BaseURL, Name: user.Name, D: data}
	if c != Account {
		if !test {
			on, err := m.Enabled(ctx, userID, c)
			if err != nil || !on {
				return false, err
			}
		}
		info, _ := Info(c)
		v.CategoryLabel = info.Label
		v.UnsubscribeURL = m.UnsubscribeURL(user.EmailToken, c)
		if c == News {
			v.NewsSender = m.cfg.NewsSender
		}
	}
	subject, text, html, err := m.render(template, v)
	if err != nil {
		return false, err
	}
	if test {
		subject = "[Test] " + subject
	}
	if err := m.q.QueueEmail(ctx, db.QueueEmailParams{
		UserID: sql.NullInt64{Int64: userID, Valid: true}, ToEmail: user.Email,
		Category: string(c), Template: template,
		Subject: subject, TextBody: text, HtmlBody: html,
		ReplyTo: m.cfg.ReplyTo, UnsubscribeUrl: v.UnsubscribeURL,
	}); err != nil {
		return false, err
	}
	m.Wake()
	return true, nil
}

// Enabled reports whether the user gets email in category c.
func (m *Mailer) Enabled(ctx context.Context, userID int64, c Category) (bool, error) {
	prefs, err := Prefs(ctx, m.q, userID)
	if err != nil {
		return false, err
	}
	return prefs[c], nil
}

// Prefs returns the user's setting for every optional category, filling in
// defaults for categories they've never changed.
func Prefs(ctx context.Context, q *db.Queries, userID int64) (map[Category]bool, error) {
	rows, err := q.ListEmailPrefs(ctx, userID)
	if err != nil {
		return nil, err
	}
	prefs := map[Category]bool{Account: true}
	for _, info := range Optional {
		prefs[info.Key] = info.Default
	}
	for _, r := range rows {
		if _, ok := Info(Category(r.Category)); ok {
			prefs[Category(r.Category)] = r.Enabled == 1
		}
	}
	return prefs, nil
}

// UnsubscribeURL is the one-click link for a user's category.
func (m *Mailer) UnsubscribeURL(emailToken string, c Category) string {
	return m.cfg.BaseURL + "/email/unsubscribe?" + url.Values{"t": {emailToken}, "c": {string(c)}}.Encode()
}

// Wake asks the worker to send now rather than at its next poll.
func (m *Mailer) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Run sends queued email until ctx is cancelled. It polls every 30 seconds
// and whenever something is queued.
func (m *Mailer) Run(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	for {
		m.sendDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
		case <-tick.C:
		case <-prune.C:
			if err := m.q.PruneEmailOutbox(ctx); err != nil {
				slog.Error("prune email outbox", "error", err)
			}
			if err := m.q.DeleteOldEmails(ctx); err != nil {
				slog.Error("delete old emails", "error", err)
			}
		}
	}
}

// sendDue sends every email that's due, a batch at a time.
func (m *Mailer) sendDue(ctx context.Context) {
	limit := time.NewTicker(time.Second / sendRate)
	defer limit.Stop()
	for {
		due, err := m.q.ListDueEmails(ctx, batchSize)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("list due emails", "error", err)
			}
			return
		}
		for _, e := range due {
			select {
			case <-ctx.Done():
				return
			case <-limit.C:
			}
			m.sendOne(ctx, e)
		}
		if len(due) < batchSize {
			return
		}
	}
}

func (m *Mailer) sendOne(ctx context.Context, e db.EmailOutbox) {
	err := m.sender.Send(ctx, Message{
		To: e.ToEmail, Subject: e.Subject, Text: e.TextBody, HTML: e.HtmlBody,
		ReplyTo: e.ReplyTo, UnsubscribeURL: e.UnsubscribeUrl,
	})
	if err == nil {
		if err := m.q.MarkEmailSent(ctx, e.ID); err != nil {
			slog.Error("mark email sent", "id", e.ID, "error", err)
		}
		return
	}
	if e.Attempts+1 >= maxAttempts {
		slog.Error("email failed, giving up", "id", e.ID, "template", e.Template, "error", err)
		_ = m.q.MarkEmailFailed(ctx, db.MarkEmailFailedParams{LastError: err.Error(), ID: e.ID})
		return
	}
	// 1, 4, 16, 64 and 256 minutes.
	wait := time.Minute << (2 * e.Attempts)
	slog.Warn("email send failed, will retry", "id", e.ID, "template", e.Template, "in", wait, "error", err)
	_ = m.q.MarkEmailRetry(ctx, db.MarkEmailRetryParams{
		LastError: err.Error(),
		SendAfter: time.Now().Add(wait).UTC().Format(time.DateTime),
		ID:        e.ID,
	})
}

// Unavailable is the production sender when SES isn't configured: it fails
// every send, so email waits in the outbox instead of a sign-in link landing
// in the server log.
type Unavailable struct{}

func (Unavailable) Send(context.Context, Message) error {
	return errors.New("email isn't configured: set SES_ACCESS_KEY_ID, SES_SECRET_ACCESS_KEY and MAIL_FROM")
}
