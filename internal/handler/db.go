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
	}
	for _, q := range alters {
		if _, err := db.Exec(q); err != nil {
			slog.Debug("migration skipped (likely exists)", "sql", q, "error", err)
		}
	}
}
