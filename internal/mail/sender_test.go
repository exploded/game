package mail

import (
	"strings"
	"testing"
	"time"
)

func TestBuildMIME(t *testing.T) {
	raw, err := buildMIME("StockGame <stockgame@mchugh.au>", Message{
		To:             "a@example.com",
		Subject:        "Hello\r\nBcc: evil@example.com",
		Text:           "plain",
		HTML:           "<p>html</p>",
		ReplyTo:        "rob@example.com",
		UnsubscribeURL: "https://game.mchugh.au/email/unsubscribe?c=news&t=abc",
	}, time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	msg := string(raw)
	head, _, _ := strings.Cut(msg, "\r\n\r\n")
	for _, want := range []string{
		"From: StockGame <stockgame@mchugh.au>\r\n",
		"To: a@example.com\r\n",
		"Reply-To: rob@example.com\r\n",
		"Subject: Hello Bcc: evil@example.com\r\n",
		"List-Unsubscribe: <https://game.mchugh.au/email/unsubscribe?c=news&t=abc>\r\n",
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n",
		"Content-Type: multipart/alternative;",
	} {
		if !strings.Contains(head+"\r\n", want) {
			t.Errorf("headers missing %q:\n%s", want, head)
		}
	}
	if strings.Contains(head, "\r\nBcc:") {
		t.Error("the subject injected a Bcc header")
	}
	for _, want := range []string{"text/plain; charset=UTF-8", "text/html; charset=UTF-8", "plain", "<p>html</p>"} {
		if !strings.Contains(msg, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestNormalizeAddress(t *testing.T) {
	for in, want := range map[string]string{
		" Rob@Example.COM ":      "rob@example.com",
		"a.b+tag@sub.example.au": "a.b+tag@sub.example.au",
		"":                       "",
		"no-at-sign":             "",
		"a@localhost":            "",
		"Rob <rob@example.com>":  "",
		"a@b.com, c@d.com":       "",
		"a@b.com\r\nBcc: x@y.z":  "",
	} {
		got, ok := NormalizeAddress(in)
		if got != want || ok != (want != "") {
			t.Errorf("NormalizeAddress(%q) = (%q, %v), want %q", in, got, ok, want)
		}
	}
}
