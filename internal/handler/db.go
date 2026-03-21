package handler

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// OpenDB opens (or creates) the SQLite database and applies the schema.
func OpenDB(dbPath, schemaPath string) (*sql.DB, error) {
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("resolving db path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", absPath+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("reading schema: %w", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	runMigrations(db)
	return db, nil
}

// runMigrations applies safe ALTER TABLE additions that may already exist.
func runMigrations(db *sql.DB) {
	alters := []string{
		"ALTER TABLE games ADD COLUMN recurring_interval TEXT DEFAULT NULL",
		"ALTER TABLE games ADD COLUMN parent_game_id INTEGER DEFAULT NULL REFERENCES games(id)",
		"ALTER TABLE games ADD COLUMN template_id INTEGER DEFAULT NULL REFERENCES game_templates(id)",
		"ALTER TABLE users ADD COLUMN deleted_at TEXT DEFAULT NULL",
		"ALTER TABLE users ADD COLUMN avatar_id INTEGER DEFAULT NULL REFERENCES avatars(id)",
		"ALTER TABLE games ADD COLUMN referral_bonus_pct INTEGER NOT NULL DEFAULT 1",
		"ALTER TABLE games ADD COLUMN is_public INTEGER NOT NULL DEFAULT 1",
		"ALTER TABLE games ADD COLUMN portfolio_visibility TEXT NOT NULL DEFAULT 'public'",
		"ALTER TABLE games ADD COLUMN credit_interest_rate INTEGER NOT NULL DEFAULT 100",
		"ALTER TABLE games ADD COLUMN leverage_interest_rate INTEGER NOT NULL DEFAULT 500",
		"ALTER TABLE games ADD COLUMN min_stock_price INTEGER DEFAULT NULL",
		"ALTER TABLE games ADD COLUMN max_stock_price INTEGER DEFAULT NULL",
		"ALTER TABLE games ADD COLUMN margin_trading INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE games ADD COLUMN limit_orders INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE games ADD COLUMN stop_loss INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE games ADD COLUMN fractional_shares INTEGER NOT NULL DEFAULT 0",
	}
	for _, q := range alters {
		if _, err := db.Exec(q); err != nil {
			slog.Debug("migration skipped (likely exists)", "sql", q, "error", err)
		}
	}
}
