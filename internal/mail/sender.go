package mail

import (
	"context"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// Message is one rendered email, ready to send.
type Message struct {
	To             string
	Subject        string
	Text           string
	HTML           string
	ReplyTo        string
	UnsubscribeURL string // adds the List-Unsubscribe headers when set
}

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SES sends through the Amazon SES v2 API as raw MIME, which is the only way
// to set the List-Unsubscribe headers. The IAM user needs ses:SendRawEmail.
type SES struct {
	client *sesv2.Client
	from   string
}

// NewSES returns an SES sender. from is a verified identity, and may include
// a display name ("The Wealth Lab <stockgame@mchugh.au>").
func NewSES(region, accessKey, secretKey, from string) *SES {
	return &SES{
		client: sesv2.NewFromConfig(aws.Config{
			Region:      region,
			Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		}),
		from: from,
	}
}

func (s *SES) Send(ctx context.Context, m Message) error {
	raw, err := buildMIME(s.from, m, time.Now())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err = s.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(s.from),
		Destination:      &sestypes.Destination{ToAddresses: []string{m.To}},
		Content:          &sestypes.EmailContent{Raw: &sestypes.RawMessage{Data: raw}},
	})
	if err != nil {
		return fmt.Errorf("ses: %w", err)
	}
	return nil
}

// Log prints each email instead of sending it. Dev uses it when SES isn't
// configured, so you can sign in by copying the link from the log.
type Log struct{}

func (Log) Send(_ context.Context, m Message) error {
	slog.Info("email not sent (no SES credentials)", "to", m.To, "subject", m.Subject)
	fmt.Printf("----- email to %s: %s -----\n%s\n----- end -----\n", m.To, m.Subject, m.Text)
	return nil
}

// buildMIME renders a multipart/alternative message with a plain-text and an
// HTML part, both quoted-printable so non-ASCII survives every hop.
func buildMIME(from string, m Message, now time.Time) ([]byte, error) {
	var b strings.Builder
	w := multipart.NewWriter(&b)
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from)
	header("To", m.To)
	if m.ReplyTo != "" {
		header("Reply-To", m.ReplyTo)
	}
	header("Subject", mime.QEncoding.Encode("utf-8", oneLine(m.Subject)))
	header("Date", now.Format(time.RFC1123Z))
	header("MIME-Version", "1.0")
	if m.UnsubscribeURL != "" {
		// RFC 8058 one-click: mail clients POST to the URL.
		header("List-Unsubscribe", "<"+m.UnsubscribeURL+">")
		header("List-Unsubscribe-Post", "List-Unsubscribe=One-Click")
	}
	header("Content-Type", `multipart/alternative; boundary="`+w.Boundary()+`"`)
	b.WriteString("\r\n")

	for _, part := range []struct{ ctype, body string }{
		{"text/plain", m.Text},
		{"text/html", m.HTML},
	} {
		pw, err := w.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.ctype + "; charset=UTF-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(pw)
		if _, err := qp.Write([]byte(part.body)); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// oneLine stops a subject from smuggling in extra headers.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// NormalizeAddress validates a bare address ("user@host.tld", no display
// name) and returns it in lower case. It's strict on purpose: the value goes
// into a To header, so anything that could add a header or a second
// recipient is refused.
func NormalizeAddress(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 254 || strings.ContainsAny(s, " \t\r\n<>,;\"()") {
		return "", false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Name != "" || !strings.EqualFold(addr.Address, s) {
		return "", false
	}
	at := strings.LastIndex(s, "@")
	if at <= 0 || !strings.Contains(s[at+1:], ".") {
		return "", false
	}
	return strings.ToLower(s), true
}
