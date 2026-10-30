package handler

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// OpenDB opens (or creates) the SQLite database, applies the schema and runs
// migrations.
//
// PRAGMAs must use modernc.org/sqlite's `_pragma=name(value)` form. The
// mattn/go-sqlite3 style (`_journal_mode=WAL`, `_foreign_keys=on`, ...) is
// silently discarded by this driver, which left production on a rollback
// journal with foreign keys off and no busy timeout.
func OpenDB(dbPath, schema string) (*sql.DB, error) {
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("resolving db path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return nil, err
	}
	dsn := absPath +
		"?_pragma=busy_timeout(5000)" + // wait rather than failing on a locked db
		"&_pragma=journal_mode(WAL)" + // readers don't block the writer
		"&_pragma=synchronous(NORMAL)" + // safe in WAL mode; far fewer fsyncs
		"&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	// A wrong DSN dialect fails silently, so check the PRAGMAs landed.
	var journalMode string
	var foreignKeys int
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		db.Close()
		return nil, fmt.Errorf("reading journal_mode: %w", err)
	}
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		db.Close()
		return nil, fmt.Errorf("reading foreign_keys: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") || foreignKeys != 1 {
		slog.Warn("sqlite pragmas not applied - check the DSN syntax",
			"journal_mode", journalMode, "foreign_keys", foreignKeys)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// addedColumns are columns added after their table was first created.
// schema.sql already has them, so they only matter for older databases.
var addedColumns = []struct{ table, column, ddl string }{
	{"games", "recurring_interval", "TEXT DEFAULT NULL"},
	{"games", "parent_game_id", "INTEGER DEFAULT NULL REFERENCES games(id)"},
	{"games", "template_id", "INTEGER DEFAULT NULL REFERENCES game_templates(id)"},
	{"users", "deleted_at", "TEXT DEFAULT NULL"},
	{"users", "avatar_id", "INTEGER DEFAULT NULL REFERENCES avatars(id)"},
	{"games", "referral_bonus_pct", "INTEGER NOT NULL DEFAULT 1"},
	{"games", "is_public", "INTEGER NOT NULL DEFAULT 1"},
	{"games", "portfolio_visibility", "TEXT NOT NULL DEFAULT 'public'"},
	{"games", "credit_interest_rate", "INTEGER NOT NULL DEFAULT 525"},
	{"games", "leverage_interest_rate", "INTEGER NOT NULL DEFAULT 500"},
	{"games", "min_stock_price", "INTEGER DEFAULT NULL"},
	{"games", "max_stock_price", "INTEGER DEFAULT NULL"},
	{"games", "margin_trading", "INTEGER NOT NULL DEFAULT 0"},
	{"games", "limit_orders", "INTEGER NOT NULL DEFAULT 0"},
	{"games", "stop_loss", "INTEGER NOT NULL DEFAULT 0"},
	{"games", "fractional_shares", "INTEGER NOT NULL DEFAULT 0"},
	{"stock_profiles", "logo", "TEXT NOT NULL DEFAULT ''"},
	{"stock_profiles", "ipo_date", "TEXT NOT NULL DEFAULT ''"},
	{"stock_profiles", "peers", "TEXT NOT NULL DEFAULT ''"},
	{"users", "last_game_id", "INTEGER DEFAULT NULL REFERENCES games(id) ON DELETE SET NULL"},
	{"limit_orders", "kind", "TEXT NOT NULL DEFAULT 'limit'"},
	{"notifications", "emailed", "INTEGER NOT NULL DEFAULT 0"},
	{"games", "quarter", "TEXT DEFAULT NULL"},
	{"questions", "topic_id", "INTEGER DEFAULT NULL REFERENCES topics(id) ON DELETE SET NULL"},
	{"questions", "answer_video_id", "INTEGER DEFAULT NULL REFERENCES videos(id) ON DELETE SET NULL"},
	{"questions", "answer_seconds", "INTEGER NOT NULL DEFAULT 0"},
	{"questions", "live_event_id", "INTEGER DEFAULT NULL REFERENCES live_events(id) ON DELETE SET NULL"},
	{"questions", "answer_mailed", "INTEGER NOT NULL DEFAULT 1"},
	{"holdings", "cost_basis", "INTEGER NOT NULL DEFAULT 0"},
	{"games", "after_hours_trading", "INTEGER NOT NULL DEFAULT 1"},
	{"game_invites", "email", "TEXT DEFAULT NULL"},
}

// onceMigrations run exactly once per database, in order. Each is recorded in
// schema_migrations in the same transaction as its statement, so a crash
// midway leaves it unrecorded and it retries on the next start.
var onceMigrations = []struct{ name, stmt string }{
	// Old Finnhub news, cleared so TickerTick can repopulate.
	{"clear-finnhub-news", "DELETE FROM stock_news"},
	// Duplicated the UNIQUE(stock_id, date) index, which SQLite also scans
	// backwards for ORDER BY date DESC. Only cost space and insert time.
	{"drop-idx-prices-stock-date", "DROP INDEX IF EXISTS idx_prices_stock_date"},
	// TickerTick matched ASX codes to unrelated US companies. ASX news is no
	// longer fetched, so clear what it stored.
	{"clear-asx-news", "DELETE FROM stock_news WHERE stock_id IN (SELECT id FROM stocks WHERE market = 'asx')"},
	// Notifications from before email existed mustn't all be emailed at once.
	{"mark-old-notifications-emailed", "UPDATE notifications SET emailed = 1"},
	// Not in schema.sql, which runs before addedColumns adds emailed.
	{"idx-notifications-emailed", "CREATE INDEX IF NOT EXISTS idx_notifications_emailed ON notifications(emailed) WHERE emailed = 0"},
	// One game per quarter. Not in schema.sql, which runs before addedColumns
	// adds quarter.
	{"idx-games-quarter", "CREATE UNIQUE INDEX IF NOT EXISTS idx_games_quarter ON games(quarter) WHERE quarter IS NOT NULL"},
	// Games went monthly on 1 November 2026. The Oct–Dec game ends on 31
	// October as October's game, and the Jan–Mar game goes (with anyone who
	// joined it early); the scheduler creates November's and December's.
	{"monthly-games-october", `UPDATE games SET
		quarter = '2026-10',
		end_date = '2026-10-31',
		name = CASE WHEN name = 'Oct–Dec 2026' THEN 'October 2026' ELSE name END,
		description = CASE WHEN description = 'Three months to grow $1,000,000. The biggest portfolio when the game ends wins.'
			THEN 'One month to grow $1,000,000. The biggest portfolio when the game ends wins.' ELSE description END
		WHERE quarter = '2026-Q4'`},
	{"monthly-games-drop-2027-q1", "DELETE FROM games WHERE quarter = '2027-Q1' AND status = 'pending'"},
	// The Learn topics. Admins rename or add them on /admin/learn/topics;
	// Claude reads each description when it sorts a video.
	{"seed-topics", `INSERT OR IGNORE INTO topics (slug, name, description, sort_order) VALUES
		('retirement', 'Retirement', 'Planning for and living in retirement: how much is enough, drawing an income, the Age Pension, when to retire.', 1),
		('super', 'Superannuation', 'Super funds, contributions, salary sacrifice, transfer balance caps, SMSFs, choosing a fund.', 2),
		('investing', 'Investing basics', 'Getting started, risk and return, diversification, balanced vs growth portfolios, compounding, investor behaviour.', 3),
		('shares', 'Shares and ETFs', 'Individual stocks, ETFs, managed funds, dividends, the ASX and overseas markets.', 4),
		('property', 'Property and real estate', 'Investment property, negative gearing, property vs shares, REITs.', 5),
		('first-home', 'Buying a home', 'Saving a deposit, first home buyer schemes, borrowing, renting vs buying.', 6),
		('saving', 'Saving and budgeting', 'Budgets, emergency funds, saving habits, money for young people.', 7),
		('debt', 'Debt and mortgages', 'Paying off the mortgage vs investing, offset accounts, credit cards, personal debt.', 8),
		('estate', 'Inheritance and estate planning', 'Wills, inheritances, passing on wealth, death benefits, powers of attorney.', 9),
		('tax', 'Tax', 'Capital gains, tax on super and investments, franking credits, structures.', 10),
		('insurance', 'Insurance', 'Life, TPD, income protection, insurance inside super.', 11),
		('markets', 'Markets and the economy', 'Market volatility and crashes, interest rates, inflation, market news.', 12),
		('aged-care', 'Aged care', 'Aged care costs, accommodation deposits, helping ageing parents.', 13),
		('advice', 'Financial advice', 'Working with an adviser, fees, DIY vs professional advice.', 14)`},
	// Not in schema.sql, which runs before addedColumns adds them.
	{"idx-questions-topic", "CREATE INDEX IF NOT EXISTS idx_questions_topic ON questions(topic_id) WHERE topic_id IS NOT NULL"},
	// Holdings kept only a rounded-down average price. Start each total
	// from it; trades from now on keep it exact.
	{"holdings-cost-basis", "UPDATE holdings SET cost_basis = quantity * avg_cost"},
	// Interest on cash started at the 1% default. Match Macquarie's 5.25% in
	// games still to come or running, unless an admin chose a rate.
	{"interest-5-25", "UPDATE games SET credit_interest_rate = 525 WHERE status IN ('pending', 'active') AND credit_interest_rate = 100"},
	// Until 30 October 2026 production ran the dev-only fake price feed, which
	// walked AUD/USD from 0.66 down to 0.11. The scheduler refetches real
	// rates once the table is empty.
	{"real-exchange-rates", "DELETE FROM exchange_rates"},
	// The one trade that used the fake rate: 3 IBM at US$255.07 cost
	// A$7,175.57 at 0.106641 instead of A$1,088.98 at 0.70268. Refund the
	// difference, then fix the trade. Both match only the bad trade.
	{"fix-fake-rate-trade-cash", `UPDATE participants SET cash_balance = cash_balance + 608659
		WHERE id = (SELECT participant_id FROM transactions
			WHERE id = 21 AND exchange_rate_used = 106641 AND converted_total = 717557)`},
	{"fix-fake-rate-trade", `UPDATE transactions SET converted_total = 108898, exchange_rate_used = 702680
		WHERE id = 21 AND exchange_rate_used = 106641 AND converted_total = 717557`},
	// Orders are now cancelled when their game ends. Cancel those left open
	// in games that ended before that, quietly.
	{"cancel-orders-in-ended-games", `UPDATE limit_orders SET status = 'cancelled'
		WHERE status = 'open' AND participant_id IN (
			SELECT p.id FROM participants p JOIN games g ON g.id = p.game_id
			WHERE g.status IN ('finished', 'cancelled'))`},
}

func runMigrations(db *sql.DB) error {
	for _, c := range addedColumns {
		var n int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", c.table, c.column,
		).Scan(&n); err != nil {
			return fmt.Errorf("checking %s.%s: %w", c.table, c.column, err)
		}
		if n > 0 {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.table, c.column, c.ddl)); err != nil {
			return fmt.Errorf("adding %s.%s: %w", c.table, c.column, err)
		}
		slog.Info("added column", "table", c.table, "column", c.column)
	}
	// After addedColumns, so the old users table has every column it copies.
	if err := rebuildUsersTable(db); err != nil {
		return err
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	// Before schema_migrations existed, clear-finnhub-news marked itself done
	// by creating a guard table. Carry that over so it doesn't run again.
	var guard int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = '_m_clear_finnhub_news'",
	).Scan(&guard); err != nil {
		return fmt.Errorf("checking legacy guard table: %w", err)
	}
	if guard > 0 {
		if _, err := db.Exec("INSERT OR IGNORE INTO schema_migrations (name) VALUES ('clear-finnhub-news')"); err != nil {
			return fmt.Errorf("recording legacy migration: %w", err)
		}
		if _, err := db.Exec("DROP TABLE _m_clear_finnhub_news"); err != nil {
			return fmt.Errorf("dropping legacy guard table: %w", err)
		}
	}

	for _, m := range onceMigrations {
		var done int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE name = ?", m.name).Scan(&done); err != nil {
			return fmt.Errorf("checking migration %s: %w", m.name, err)
		}
		if done > 0 {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", m.name, err)
		}
		res, err := tx.Exec(m.stmt)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (name) VALUES (?)", m.name); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording migration %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.name, err)
		}
		rows, _ := res.RowsAffected()
		slog.Info("applied migration", "name", m.name, "rows", rows)
	}
	return nil
}

// rebuildUsersTable moves a database from Google sign-in to email sign-in.
// SQLite can't drop a UNIQUE column, so it copies users into a new table
// without google_id, keeping each id so every foreign key still points at the
// same person, and gives each user an email_token. It does nothing once
// google_id is gone.
func rebuildUsersTable(db *sql.DB) error {
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'google_id'",
	).Scan(&n); err != nil {
		return fmt.Errorf("checking users.google_id: %w", err)
	}
	if n == 0 {
		return nil
	}

	// Dropping users with foreign keys on would cascade or fail, and the
	// PRAGMA is ignored inside a transaction, so pin one connection and turn
	// them off around it.
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`CREATE TABLE users_new (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			email       TEXT    NOT NULL UNIQUE,
			name        TEXT    NOT NULL,
			picture_url TEXT,
			is_admin    INTEGER NOT NULL DEFAULT 0,
			avatar_id   INTEGER REFERENCES avatars(id),
			created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
			last_login  TEXT    NOT NULL DEFAULT (datetime('now')),
			deleted_at  TEXT,
			last_game_id INTEGER REFERENCES games(id) ON DELETE SET NULL,
			email_token TEXT    NOT NULL UNIQUE
		)`,
		`INSERT INTO users_new (id, email, name, picture_url, is_admin, avatar_id,
			created_at, last_login, deleted_at, last_game_id, email_token)
		 SELECT id, lower(email), name, picture_url, is_admin, avatar_id,
			created_at, last_login, deleted_at, last_game_id, lower(hex(randomblob(16)))
		 FROM users`,
		`DROP TABLE users`,
		`ALTER TABLE users_new RENAME TO users`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("rebuilding users: %w", err)
		}
	}
	// Production once ran with foreign keys off, so older rows may already
	// point at missing users. Report them rather than refusing to start.
	var broken int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&broken); err != nil {
		return err
	}
	if broken > 0 {
		slog.Warn("foreign_key_check found broken references after rebuilding users", "rows", broken)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	slog.Info("rebuilt users table for email sign-in")
	return nil
}
