## Project: game

**Path:** `C:\Projects\go\game`
**Purpose:** Fantasy stock market game platform. Users get $1M to invest in ASX 200 and S&P 500 stocks.

### Stack
- Go 1.26 / Chi router / htmx 4 / SQLite (modernc.org/sqlite) / sqlc / Go html/template

### Dev server
- **NEVER start the server.** It runs on port 8888 (`PORT` in `.env`). The user always starts and stops it themselves. Claude should only build (`go build`) and run `go test`, never run the binary.

### Key conventions
- **Money**: All monetary values stored as INTEGER cents. Never use floats for money.
- **Exchange rates**: Stored as INTEGER (rate * 1,000,000) for precision.
- **SQLite**: WAL mode, foreign_keys on, busy_timeout=5000, SetMaxOpenConns(1). The DSN in `internal/handler/db.go` must use modernc's `_pragma=name(value)` form; the mattn form (`_journal_mode=WAL`) is silently ignored. `TestOpenDBAppliesPragmas` guards this.
- **Migrations** (`internal/handler/db.go`): new columns go in `addedColumns` (and in `sql/schema.sql`); one-off data changes go in `onceMigrations`, recorded in `schema_migrations`.
- **Embedded assets**: `assets.go` embeds `templates/`, `static/` and `sql/schema.sql` into the binary. Template, CSS or JS changes need a rebuild. Link static files with `{{asset "css/app.css"}}`, which adds a content hash so they can be cached for a year.
- **Templates**: layouts/base.html cloned per page. Partials prefixed with `_`. HTMX fragments via isHTMX(r).
- **Game pages**: every `/games/{id}/...` route sits in one `r.Route` in `main.go` with the `GameContext` middleware, which loads the game bar (`partials/_gamebar.html`: game name, cash, tabs, switcher) and sets `users.last_game_id`. `render()` fills `PageData.GameBar` from the context, so handlers don't. Trading happens in the order ticket on the stock page (`stocks/_ticket.html`); `/trade/{sid}` GET redirects there.
- **htmx 4**: use htmx 4 spellings (`hx-disable`, `:inherited`, `htmx:after:request`). The CSRF header is on `<body hx-headers:inherited=...>`. Error responses (4xx/5xx) don't swap (`noSwap` in base.html); `app.js` shows a toast instead.
- **Auth**: Google OAuth via `internal/auth/`. Sessions in SQLite, 30-day expiry.
- **Price API**: Twelve Data (twelvedata.com). ASX symbols use `:ASX` suffix.
- **Build for Linux**: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o game ./cmd/server/`

### Content Security Policy
The CSP in `internal/handler/security.go` uses a per-request nonce for scripts. `TestTemplatesAreCSPSafe` enforces the rules:
- Inline `<script>` blocks need `nonce="{{.Nonce}}"`.
- No inline event handlers (`onclick=`, `onchange=`, `onsubmit=`). Use a `data-` attribute handled in `static/js/app.js` (`data-confirm`, `data-autosubmit`, `data-nav-toggle`, `data-reset-on-success`).
- Scripts are self-hosted under `static/js/` with the version in the filename. No CDN script URLs.
- Inline `style="..."` attributes are fine (`'unsafe-inline'` stays in `style-src`).
- External images and fonts must be listed in `img-src` / `font-src`.

A wrong CSP silently breaks pages in production.

### Tests
`go test ./...` covers the DB settings and migrations, the CSP and template rules, static caching and `/health`. CI also runs `gofmt` and `go vet`.

### sqlc
- Config: `sqlc.yaml`
- Schema: `sql/schema.sql`
- Queries: `sql/queries.sql`
- Generated code: `internal/db/` (never edit generated files)
