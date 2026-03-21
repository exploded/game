## Project: game

**Path:** `C:\Users\jmcat\OneDrive\Documents\go\projects\game`
**Purpose:** Fantasy stock market game platform. Users get $1M to invest in ASX 200 and S&P 500 stocks.

### Stack
- Go 1.25+ / Chi router / HTMX / SQLite (modernc.org/sqlite) / sqlc / Go html/template

### Dev server
- **NEVER start the server on port 8080.** The user will always start and stop the server themselves. Claude should only build (`go build`) and never run the binary.

### Key conventions
- **Money**: All monetary values stored as INTEGER cents. Never use floats for money.
- **Exchange rates**: Stored as INTEGER (rate * 1,000,000) for precision.
- **SQLite**: WAL mode, foreign_keys=on, busy_timeout=5000, SetMaxOpenConns(1).
- **Templates**: layouts/base.html cloned per page. Partials prefixed with `_`. HTMX fragments via isHTMX(r).
- **Auth**: Google OAuth via `internal/auth/`. Sessions in SQLite, 30-day expiry.
- **Price API**: Twelve Data (twelvedata.com). ASX symbols use `:ASX` suffix.
- **Build for Linux**: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o game ./cmd/server/`

### sqlc
- Config: `sqlc.yaml`
- Schema: `sql/schema.sql`
- Queries: `sql/queries.sql`
- Generated code: `internal/db/` (never edit generated files)
