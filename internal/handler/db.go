package handler

import (
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
	{"games", "credit_interest_rate", "INTEGER NOT NULL DEFAULT 100"},
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
