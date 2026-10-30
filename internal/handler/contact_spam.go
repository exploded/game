package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/exploded/game/internal/auth"
)

// The contact form is open to anyone, and until 30 October 2026 everything
// sent through it was spam. A signed-out sender has to get past three checks:
//
//   - website, a field people can't see, must be empty. Bots fill every field.
//   - t, a signed token the form page carries, must be there and at least
//     contactMinAge old. Bots that post straight to /contact have none, and
//     the ones that load the page first usually post at once.
//   - The message mustn't be a sales pitch (looksLikeSpam).
//
// A message that fails is dropped, but the sender sees the usual thanks, so a
// bot learns nothing. The log keeps the start of it in case a real one is
// caught. Signed-in players skip the checks.

const (
	contactMinAge = 3 * time.Second
	contactMaxAge = 24 * time.Hour

	errContactExpired = "This form was open for more than a day. Please send it again."
)

// contactKey is the key the form tokens are signed with: the CSRF secret,
// which survives restarts, so a form open across a deploy still works.
func (h *Handler) contactKey() []byte {
	h.contactKeyOnce.Do(func() {
		key, err := auth.LoadCSRFSecret(h.rawDB)
		if err != nil {
			slog.Error("contact form key", "error", err)
		}
		h.contactKeyVal = key
	})
	return h.contactKeyVal
}

func contactSign(key []byte, ts string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("contact:" + ts))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// contactToken is the form's t field: when the page was made, and a signature.
func (h *Handler) contactToken(now time.Time) string {
	ts := strconv.FormatInt(now.Unix(), 10)
	return ts + "." + contactSign(h.contactKey(), ts)
}

// contactTokenAge is how long ago the token was made, or false if it's
// missing or forged.
func (h *Handler) contactTokenAge(token string, now time.Time) (time.Duration, bool) {
	ts, sig, ok := strings.Cut(token, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(contactSign(h.contactKey(), ts))) {
		return 0, false
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return 0, false
	}
	return now.Sub(time.Unix(sec, 0)), true
}

// spamPhrases are what the SEO and web design pitches say. Players asking
// about the share game don't.
var spamPhrases = regexp.MustCompile(`(?i)\bseo\b|search engine optimi|lead generation|qualified leads|` +
	`organic (traffic|clients|leads)|website traffic|traffic to your (site|website)|low traffic|` +
	`(more|targeted) traffic|visitors into (leads|customers)|` +
	`(first|1st) page (of|on) google|google (first|1st) page|web ?design|website (design|development)|backlinks`)

// looksLikeSpam says whether a message is a pitch. The pitches also greet the
// site by its domain ("Hello thewealthlab.au,"), which a person wouldn't.
func looksLikeSpam(message, host string) bool {
	if spamPhrases.MatchString(message) {
		return true
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	return host != "" && strings.Contains(strings.ToLower(message), host)
}

// truncate cuts s to at most n characters.
func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
