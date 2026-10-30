package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/go-chi/chi/v5"
)

func TestLooksLikeSpam(t *testing.T) {
	for msg, want := range map[string]bool{
		"Hello thewealthlab.au, are you open to growing your traffic?":                true,
		"Hi https://thewealthlab.au/, I can help.":                                    true,
		"Several SEO steps on your site are incomplete.":                              true,
		"We can put your website on Google 1st page.":                                 true,
		"Are you struggling with low traffic and turning visitors into leads?":        true,
		"Attract 10 to 20 qualified leads using proven tactics.":                      true,
		"I can't sell my Apple shares, the button does nothing.":                      false,
		"Will the share game have ASX stocks? I'd like to trade BHP.":                 false,
		"Rob, loved the video on super. Could you do one on franking credits?":        false,
		"My portfolio value hasn't changed since Friday, is the market data working?": false,
	} {
		if got := looksLikeSpam(msg, "thewealthlab.au:443"); got != want {
			t.Errorf("looksLikeSpam(%q) = %v, want %v", msg, got, want)
		}
	}
}

// contactServer serves /contact, as user 1 when signedIn.
func contactServer(t *testing.T, signedIn bool) (http.Handler, *Handler, *db.Queries) {
	t.Helper()
	d := openTestDB(t)
	if _, err := d.Exec(`INSERT INTO users (id, email, name, email_token) VALUES (1, 'amy@x.com', 'Amy', 't1')`); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	r := chi.NewRouter()
	if signedIn {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				u, _ := q.GetUser(r.Context(), 1)
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), &u)))
			})
		})
	}
	r.Get("/contact", h.ContactPage)
	r.Post("/contact", h.ContactSubmit)
	return r, h, q
}

func TestContactSpamIsDropped(t *testing.T) {
	srv, h, q := contactServer(t, false)
	now := time.Now()
	old := h.contactToken(now.Add(-time.Minute))
	form := func(token, website, message string) url.Values {
		return url.Values{"name": {"Sam"}, "email": {"sam@x.com"}, "message": {message}, "t": {token}, "website": {website}}
	}
	for name, f := range map[string]url.Values{
		"no token":     form("", "", "Hi"),
		"forged token": form("1700000000.0123456789abcdef0123456789abcdef", "", "Hi"),
		"too fast":     form(h.contactToken(now), "", "Hi"),
		"hidden field": form(old, "https://spam.example", "Hi"),
		"sales pitch":  form(old, "", "Hello example.com, want more website traffic?"),
	} {
		w := post(srv, "/contact", f)
		if w.Code != http.StatusSeeOther || !strings.Contains(cookie(w, "flash").Value, "has been sent") {
			t.Errorf("%s: got %d, want the usual thanks", name, w.Code)
		}
	}
	if n, _ := q.CountContactMessages(t.Context()); n != 0 {
		t.Errorf("%d spam messages were stored", n)
	}

	// A real message from someone who took a minute to write it is kept.
	post(srv, "/contact", form(old, "", "How do I reset my portfolio?"))
	if n, _ := q.CountContactMessages(t.Context()); n != 1 {
		t.Errorf("the real message wasn't stored (%d messages)", n)
	}

	// A form left open for days is shown again, with the message kept.
	w := post(srv, "/contact", form(h.contactToken(now.Add(-48*time.Hour)), "", "Still there?"))
	if body := w.Body.String(); w.Code != http.StatusOK || !strings.Contains(body, "more than a day") || !strings.Contains(body, "Still there?") {
		t.Errorf("an expired form wasn't shown again: %d", w.Code)
	}
}

func TestContactFormCarriesToken(t *testing.T) {
	srv, h, _ := contactServer(t, false)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/contact", nil))
	m := regexp.MustCompile(`name="t" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatal("the contact form has no token")
	}
	if _, ok := h.contactTokenAge(m[1], time.Now()); !ok {
		t.Errorf("the form's token %q doesn't verify", m[1])
	}
	if !strings.Contains(w.Body.String(), `name="website"`) {
		t.Error("the contact form has no hidden trap field")
	}
}

func TestContactSignedInSkipsChecks(t *testing.T) {
	srv, _, q := contactServer(t, true)
	post(srv, "/contact", url.Values{"name": {"Amy"}, "email": {"amy@x.com"}, "message": {"Is SEO a stock?"}})
	if n, _ := q.CountContactMessages(t.Context()); n != 1 {
		t.Errorf("a signed-in player's message wasn't stored (%d messages)", n)
	}
}
