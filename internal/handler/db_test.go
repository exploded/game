package handler

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/auth"
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

// TestOpenDBUpgradesLatestDeploy opens a database made by the schema
// deployed on 2026-10-30 (6ef8b66), which already has questions without
// the Learn columns. Refresh the file after each deploy.
func TestOpenDBUpgradesLatestDeploy(t *testing.T) {
	old, err := os.ReadFile("testdata/schema-2026-10-30.sql")
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
		"INSERT INTO users (id, email, name, email_token) VALUES (3, 'ann@example.com', 'Ann', 't3')",
		"INSERT INTO questions (id, user_id, title, answer, answered_at) VALUES (1, 3, 'Old question', 'Old answer', datetime('now'))",
		"INSERT INTO holdings (participant_id, stock_id, quantity, avg_cost) VALUES (1, 1, 200, 4567)",
		`INSERT INTO games (id, created_by, name, description, status, start_date, end_date, quarter) VALUES
			(1, 3, 'Oct–Dec 2026', 'Three months to grow $1,000,000. The biggest portfolio when the game ends wins.', 'active', '2026-10-01', '2026-12-31', '2026-Q4'),
			(2, 3, 'Jan–Mar 2027', 'Three months to grow $1,000,000. The biggest portfolio when the game ends wins.', 'pending', '2027-01-01', '2027-03-31', '2027-Q1')`,
		"INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 3, 100000000, 100000000), (2, 2, 3, 100000000, 100000000)",
		"INSERT INTO watchlist (participant_id, stock_id) VALUES (1, 1)",
		"INSERT INTO achievements (id, key, name, description) VALUES (1, 'first_trade', 'First Trade', ''), (2, 'ten_trades', 'Active Trader', '')",
		`INSERT INTO user_achievements (user_id, achievement_id, game_id, earned_at) VALUES
			(3, 1, 2, '2026-11-01 09:00:00'), (3, 1, 1, '2026-10-05 09:00:00'), (3, 2, 1, '2026-10-20 09:00:00')`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("old database: %v", err)
		}
	}
	raw.Close()

	d := openTestDBAt(t, path)
	var mailed int
	var topic sql.NullInt64
	if err := d.QueryRow("SELECT answer_mailed, topic_id FROM questions WHERE id = 1").Scan(&mailed, &topic); err != nil {
		t.Fatal(err)
	}
	if mailed != 1 || topic.Valid {
		t.Errorf("old question: answer_mailed = %d, topic = %v; want 1 and none", mailed, topic)
	}
	// The per-game watchlist is dropped and made again per player.
	var byUser, watched int
	d.QueryRow("SELECT COUNT(*) FROM pragma_table_info('watchlist') WHERE name = 'user_id'").Scan(&byUser)
	d.QueryRow("SELECT COUNT(*) FROM watchlist").Scan(&watched)
	if byUser != 1 || watched != 0 {
		t.Errorf("watchlist: user_id columns = %d, rows = %d; want 1 and 0", byUser, watched)
	}
	// Badges earned in two games collapse to one each, from the first game.
	var badges, gameCols int
	var firstTrade string
	d.QueryRow("SELECT COUNT(*) FROM user_achievements").Scan(&badges)
	d.QueryRow("SELECT earned_at FROM user_achievements WHERE achievement_id = 1").Scan(&firstTrade)
	d.QueryRow("SELECT COUNT(*) FROM pragma_table_info('user_achievements') WHERE name = 'game_id'").Scan(&gameCols)
	if badges != 2 || firstTrade != "2026-10-05 09:00:00" || gameCols != 0 {
		t.Errorf("user_achievements: %d rows, First Trade earned %q, game_id columns %d; want 2, 2026-10-05 09:00:00, 0",
			badges, firstTrade, gameCols)
	}
	var topics int
	d.QueryRow("SELECT COUNT(*) FROM topics").Scan(&topics)
	if topics == 0 {
		t.Error("the topics weren't seeded")
	}
	var cost int64
	d.QueryRow("SELECT cost_basis FROM holdings").Scan(&cost)
	if cost != 200*4567 {
		t.Errorf("old holding's cost_basis = %d, want %d", cost, 200*4567)
	}
	for _, c := range droppedColumns {
		var n int
		d.QueryRow("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", c.table, c.column).Scan(&n)
		if n != 0 {
			t.Errorf("%s.%s wasn't dropped", c.table, c.column)
		}
	}
	for _, table := range []string{"game_templates", "game_starting_stocks"} {
		var n int
		d.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&n)
		if n != 0 {
			t.Errorf("table %s wasn't dropped", table)
		}
	}

	// Games went monthly: Oct–Dec ends on 31 October, and Jan–Mar goes.
	var key, name, end, desc string
	if err := d.QueryRow("SELECT quarter, name, end_date, description FROM games WHERE id = 1").Scan(&key, &name, &end, &desc); err != nil {
		t.Fatal(err)
	}
	if key != "2026-10" || name != "October 2026" || end != "2026-10-31" || !strings.HasPrefix(desc, "One month") {
		t.Errorf("Oct–Dec game is now %s %q ending %s, %q; want 2026-10 \"October 2026\" ending 2026-10-31, one month", key, name, end, desc)
	}
	var games, players int
	d.QueryRow("SELECT COUNT(*) FROM games WHERE id = 2").Scan(&games)
	d.QueryRow("SELECT COUNT(*) FROM participants WHERE game_id = 2").Scan(&players)
	if games != 0 || players != 0 {
		t.Errorf("Jan–Mar game: %d games and %d players left, want none", games, players)
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

// TestDropASXStocks: the drop-asx migrations delete the ended games that
// traded ASX stocks, then every ASX stock nothing still trades. A running
// game with an ASX holding keeps it, and so does the stock.
func TestDropASXStocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := OpenDB(path, game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"DELETE FROM schema_migrations WHERE name LIKE 'drop-asx-%'",
		"INSERT INTO users (id, email, name, email_token) VALUES (1, 'ann@example.com', 'Ann', 't1')",
		`INSERT INTO stocks (id, symbol, name, market, currency) VALUES
			(1, 'AAPL', 'Apple', 'sp500', 'USD'), (2, 'CBA', 'Commonwealth Bank', 'asx', 'AUD'),
			(3, 'WES', 'Wesfarmers', 'asx', 'AUD'), (4, 'TLS', 'Telstra', 'asx', 'AUD')`,
		`INSERT INTO games (id, created_by, name, status, start_date, end_date) VALUES
			(1, 1, 'Old', 'finished', '2026-03-01', '2026-03-31'),
			(2, 1, 'Running', 'active', '2026-10-01', '2026-10-31'),
			(3, 1, 'Other old', 'finished', '2026-04-01', '2026-04-30')`,
		"INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 1, 0, 0), (2, 2, 1, 0, 0), (3, 3, 1, 0, 0)",
		"INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total) VALUES (1, 2, 'buy', 1, 100, 100, 100), (2, 3, 'buy', 1, 100, 100, 100), (3, 1, 'buy', 1, 100, 100, 100)",
		"INSERT INTO holdings (participant_id, stock_id, quantity, avg_cost) VALUES (1, 2, 1, 100), (2, 3, 1, 100)",
		"INSERT INTO watchlist (user_id, stock_id) VALUES (1, 4)",
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	d.Close()

	d = openTestDBAt(t, path)
	var gameIDs, symbols string
	var watched int
	d.QueryRow("SELECT GROUP_CONCAT(id) FROM (SELECT id FROM games ORDER BY id)").Scan(&gameIDs)
	d.QueryRow("SELECT GROUP_CONCAT(symbol) FROM (SELECT symbol FROM stocks ORDER BY id)").Scan(&symbols)
	d.QueryRow("SELECT COUNT(*) FROM watchlist").Scan(&watched)
	if gameIDs != "2,3" {
		t.Errorf("games left = %s, want 2,3: only the ended game with ASX trades goes", gameIDs)
	}
	if symbols != "AAPL,WES" {
		t.Errorf("stocks left = %s, want AAPL,WES: WES is still held in a running game", symbols)
	}
	if watched != 0 {
		t.Errorf("%d watchlist rows left, want none", watched)
	}
}

// TestRenameSQToXYZ: Block's old SQ row becomes XYZ and keeps its id, so its
// holdings and prices follow it.
func TestRenameSQToXYZ(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := OpenDB(path, game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"DELETE FROM schema_migrations WHERE name = 'rename-sq-xyz'",
		"INSERT INTO stocks (id, symbol, name, market, currency) VALUES (7, 'SQ', 'Block Inc.', 'sp500', 'USD')",
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	d.Close()

	d = openTestDBAt(t, path)
	var symbols string
	d.QueryRow("SELECT GROUP_CONCAT(id || ':' || symbol) FROM stocks").Scan(&symbols)
	if symbols != "7:XYZ" {
		t.Errorf("stocks = %s, want 7:XYZ", symbols)
	}
}

// TestCSRFSecretSurvivesRestart: a new secret on every start made every page
// left open across a restart fail its next POST with "invalid CSRF token".
func TestCSRFSecretSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := OpenDB(path, game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	first, err := auth.LoadCSRFSecret(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()

	second, err := auth.LoadCSRFSecret(openTestDBAt(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 || !bytes.Equal(first, second) {
		t.Errorf("secret after reopening = %x, want the first one %x", second, first)
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
