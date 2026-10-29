package handler

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/exploded/game"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := OpenDB(filepath.Join(t.TempDir(), "test.db"), game.Schema)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// TestOpenDBUpgradesDeployedSchema opens a database made by the schema that
// was last deployed (testdata/schema-2026-08-23.sql, from d9e0098). Every
// other test starts from an empty database, so none caught schema.sql
// indexing a column that only the migrations add, which stopped production
// starting on 2026-10-30. Refresh the testdata file after each deploy.
func TestOpenDBUpgradesDeployedSchema(t *testing.T) {
	old, err := os.ReadFile("testdata/schema-2026-08-23.sql")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "test.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		string(old),
		"INSERT INTO users (id, google_id, email, name) VALUES (3, 'g3', 'Ann@Example.com', 'Ann')",
		"INSERT INTO notifications (user_id, type, title, message) VALUES (3, 'order_filled', 'Filled', 'Bought 10 AAPL')",
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("old database: %v", err)
		}
	}
	raw.Close()

	d := openTestDBAt(t, path)
	var email, token string
	if err := d.QueryRow("SELECT email, email_token FROM users WHERE id = 3").Scan(&email, &token); err != nil || email != "ann@example.com" || token == "" {
		t.Errorf("user 3 = (%q, %q, %v), want the lower-cased email and a token", email, token, err)
	}
	var emailed int
	d.QueryRow("SELECT emailed FROM notifications").Scan(&emailed)
	if emailed != 1 {
		t.Error("an old notification would be emailed")
	}
}

// TestOpenDBAppliesPragmas guards against the DSN using mattn/go-sqlite3
// syntax, which modernc silently ignores: production ran for months with WAL,
// foreign keys and the busy timeout all off.
func TestOpenDBAppliesPragmas(t *testing.T) {
	d := openTestDB(t)
	for _, tc := range []struct{ pragma, want string }{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
		{"synchronous", "1"}, // NORMAL
	} {
		var got string
		if err := d.QueryRow("PRAGMA " + tc.pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", tc.pragma, err)
		}
		if got != tc.want {
			t.Errorf("PRAGMA %s = %q, want %q", tc.pragma, got, tc.want)
		}
	}
}

func TestOpenDBEnforcesForeignKeys(t *testing.T) {
	d := openTestDB(t)
	_, err := d.Exec("INSERT INTO sessions (id, user_id, expires_at) VALUES ('s', 999999, datetime('now'))")
	if err == nil {
		t.Fatal("inserting a session for a missing user succeeded; foreign keys are off")
	}
}

// TestOpenDBIsIdempotent: a second open must not fail on columns that already
// exist or re-run once-migrations.
func TestOpenDBIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for i := range 2 {
		d, err := OpenDB(path, game.Schema)
		if err != nil {
			t.Fatalf("open %d: %v", i+1, err)
		}
		var n int
		if err := d.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != len(onceMigrations) {
			t.Errorf("open %d: %d migrations recorded, want %d", i+1, n, len(onceMigrations))
		}
		d.Close()
	}
}

// TestOpenDBAddsMissingColumns simulates a database created before a column
// existed.
func TestOpenDBAddsMissingColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := OpenDB(path, game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec("ALTER TABLE stock_profiles DROP COLUMN peers"); err != nil {
		t.Fatal(err)
	}
	d.Close()

	d = openTestDBAt(t, path)
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM pragma_table_info('stock_profiles') WHERE name = 'peers'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Error("stock_profiles.peers was not re-added")
	}
}

// TestOpenDBCarriesOverLegacyGuard: databases that already ran the Finnhub
// clear-out have a guard table instead of a schema_migrations row. Reopening
// must not delete the news TickerTick has fetched since.
func TestOpenDBCarriesOverLegacyGuard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := OpenDB(path, game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"DELETE FROM schema_migrations",
		"CREATE TABLE _m_clear_finnhub_news (id INTEGER PRIMARY KEY)",
		"INSERT INTO stocks (symbol, name, market, currency) VALUES ('AAPL', 'Apple', 'sp500', 'USD')",
		"INSERT INTO stock_news (stock_id, headline, url, published_at) VALUES (1, 'h', 'u', 0)",
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	d.Close()

	d = openTestDBAt(t, path)
	var news, guard int
	d.QueryRow("SELECT COUNT(*) FROM stock_news").Scan(&news)
	d.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = '_m_clear_finnhub_news'").Scan(&guard)
	if news != 1 {
		t.Errorf("stock_news has %d rows, want 1: the migration re-ran", news)
	}
	if guard != 0 {
		t.Error("legacy guard table was not dropped")
	}
}

// TestClearASXNews: the clear-asx-news migration removes ASX news and keeps
// S&P 500 news.
func TestClearASXNews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := OpenDB(path, game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"DELETE FROM schema_migrations WHERE name = 'clear-asx-news'",
		"INSERT INTO stocks (symbol, name, market, currency) VALUES ('AAPL', 'Apple', 'sp500', 'USD')",
		"INSERT INTO stocks (symbol, name, market, currency) VALUES ('WES', 'Wesfarmers', 'asx', 'AUD')",
		"INSERT INTO stock_news (stock_id, headline, url, published_at) VALUES (1, 'apple', 'u', 0)",
		"INSERT INTO stock_news (stock_id, headline, url, published_at) VALUES (2, 'western midstream', 'u', 0)",
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	d.Close()

	d = openTestDBAt(t, path)
	var headline string
	var n int
	d.QueryRow("SELECT COUNT(*), MAX(headline) FROM stock_news").Scan(&n, &headline)
	if n != 1 || headline != "apple" {
		t.Errorf("stock_news has %d rows (last %q), want only the S&P 500 story", n, headline)
	}
}

func openTestDBAt(t *testing.T, path string) *sql.DB {
	t.Helper()
	d, err := OpenDB(path, game.Schema)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// TestRebuildUsersForEmailSignIn: a database from the Google sign-in days
// loses google_id, keeps every user's id and the rows pointing at them, and
// gives each user an email_token.
func TestRebuildUsersForEmailSignIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := OpenDB(path, game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"PRAGMA foreign_keys = OFF",
		"DROP TABLE users",
		`CREATE TABLE users (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			google_id   TEXT    NOT NULL UNIQUE,
			email       TEXT    NOT NULL UNIQUE,
			name        TEXT    NOT NULL,
			picture_url TEXT,
			is_admin    INTEGER NOT NULL DEFAULT 0,
			avatar_id   INTEGER REFERENCES avatars(id),
			created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
			last_login  TEXT    NOT NULL DEFAULT (datetime('now')),
			deleted_at  TEXT,
			last_game_id INTEGER REFERENCES games(id) ON DELETE SET NULL
		)`,
		"INSERT INTO users (id, google_id, email, name, is_admin) VALUES (7, 'g7', 'James@Example.com', 'James', 1), (9, 'g9', 'rob@example.com', 'Rob', 0)",
		"INSERT INTO sessions (id, user_id, expires_at) VALUES ('s', 9, datetime('now', '+1 day'))",
		"INSERT INTO games (id, created_by, name, status, base_currency, start_date, end_date) VALUES (1, 7, 'Cup', 'active', 'USD', '2026-01-01', '2026-12-31')",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	d.Close()

	d = openTestDBAt(t, path)
	var n int
	d.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'google_id'").Scan(&n)
	if n != 0 {
		t.Error("users.google_id is still there")
	}
	var email, token string
	var admin int
	if err := d.QueryRow("SELECT email, is_admin, email_token FROM users WHERE id = 7").Scan(&email, &admin, &token); err != nil {
		t.Fatalf("user 7: %v", err)
	}
	if email != "james@example.com" || admin != 1 || len(token) != 32 {
		t.Errorf("user 7 = (%q, admin %d, token %q), want lower-cased email, admin kept, a 32-char token", email, admin, token)
	}
	var owner int64
	d.QueryRow("SELECT user_id FROM sessions WHERE id = 's'").Scan(&owner)
	if owner != 9 {
		t.Errorf("session belongs to user %d, want 9", owner)
	}
	if err := d.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&n); err != nil || n != 0 {
		t.Errorf("foreign_key_check: %d broken rows, err %v", n, err)
	}
	var fk int
	d.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if fk != 1 {
		t.Error("foreign keys left off after the rebuild")
	}
	// Deleting a user still cascades to their sessions.
	if _, err := d.Exec("DELETE FROM users WHERE id = 9"); err != nil {
		t.Fatal(err)
	}
	d.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n)
	if n != 0 {
		t.Error("deleting a user didn't cascade to sessions")
	}
}
