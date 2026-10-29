package handler

import (
	"database/sql"
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
