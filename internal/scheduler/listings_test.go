package scheduler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/handler"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/market"
)

// TestTellAdmins: each admin gets one email naming every new S&P-size
// listing, and a notification; players get neither.
func TestTellAdmins(t *testing.T) {
	d, err := handler.OpenDB(filepath.Join(t.TempDir(), "test.db"), game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO users (id, email, name, email_token, is_admin) VALUES
		(1, 'rob@x.com', 'Rob', 't1', 1), (2, 'pat@x.com', 'Pat', 't2', 0)`); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	m, err := mail.New(q, mail.Log{}, mail.Config{BaseURL: "https://game.example"})
	if err != nil {
		t.Fatal(err)
	}

	tellAdmins(context.Background(), q, m, []market.NewListing{
		{Symbol: "ANT", Exchange: "NASDAQ", Name: "Anthropic, PBC", MarketCap: 61_500_000_000},
	})

	rows, err := d.Query(`SELECT to_email, subject, text_body FROM email_outbox`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var to, subject, text string
		rows.Scan(&to, &subject, &text)
		n++
		if to != "rob@x.com" {
			t.Errorf("emailed %s, who isn't an admin", to)
		}
		if subject != "New listing at S&P 500 size: Anthropic, PBC (ANT)" {
			t.Errorf("subject %q", subject)
		}
		for _, want := range []string{"NASDAQ", "US$61.5 billion", "https://game.example/admin/stocks"} {
			if !strings.Contains(text, want) {
				t.Errorf("email doesn't say %q:\n%s", want, text)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d emails, want 1", n)
	}
	var notes int
	d.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = 1 AND type = 'new_listing'`).Scan(&notes)
	if notes != 1 {
		t.Errorf("%d notifications for the admin, want 1", notes)
	}
}
