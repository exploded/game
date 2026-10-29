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
- **Game pages**: every `/games/{id}/...` route sits in one `r.Route` in `main.go` with the `GameContext` middleware, which loads the game bar (`partials/_gamebar.html`: game name, cash, tabs, switcher) and sets `users.last_game_id`. `render()` fills `PageData.GameBar` from the context, so handlers don't. `/games/{id}` is the player's home (portfolio, rank, holdings with today and total moves); non-players get the rules there instead. Rules live at `/games/{id}/rules`, and `/games/{id}/portfolio` redirects home. Trading happens in the order ticket on the stock page (`stocks/_ticket.html`); `/trade/{sid}` GET redirects there.
- **Phones first**: most players are on a phone. Under 768px the game tabs are replaced by a bottom tab bar (`game-dock` in `_gamebar.html`). List tables use `class="rtable"`, which turns each row into a card: `.c-main` (stock) and `.c-end` (value) on the first line, other cells wrap below with their `data-label`, `a.stretch` makes the card tappable, and `.hide-sm`/`.show-sm` hide or show things on a phone. Other tables scroll sideways inside themselves. Keep inputs at 16px on phones, or iOS zooms in.
- **After-hours orders**: while `market.QuoteStatus()` says the US market is closed, an S&P 500 market order is stored in `limit_orders` with `kind = 'market'` instead of filling at the stale price. `ProcessLimitOrders` fills it at the first quote dated today once the market opens. `market.QueuesMarketOrder` holds the rule; handler tests override `queuesMarketOrder`.
- **htmx 4**: use htmx 4 spellings (`hx-disable`, `:inherited`, `htmx:after:request`). The CSRF header is on `<body hx-headers:inherited=...>`. Error responses (4xx/5xx) don't swap (`noSwap` in base.html); `app.js` shows a toast instead.
- **Auth**: email sign-in, no passwords (`internal/handler/login.go`). `/login` emails a link and a 6-digit code (15 minutes, single use); an unknown address creates the account. Sessions in SQLite, 30-day expiry (`internal/auth/`).
- **Email**: `internal/mail` renders templates from `internal/mail/templates/` into the `email_outbox` table; a worker sends them through SES. Send with `mailer.ToUser(ctx, userID, category, template, data)`, which skips users who turned the category off. `docs/emails.md` is the plan for every email: update it when you add one.
- **Price API**: Twelve Data (twelvedata.com). ASX symbols use `:ASX` suffix.
- **Build for Linux**: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o game ./cmd/server/`

### Content Security Policy
The CSP in `internal/handler/security.go` uses a per-request nonce for scripts. `TestTemplatesAreCSPSafe` enforces the rules:
- Inline `<script>` blocks need `nonce="{{.Nonce}}"`.
- No inline event handlers (`onclick=`, `onchange=`, `onsubmit=`). Use a `data-` attribute handled in `static/js/app.js` (`data-confirm`, `data-autosubmit`, `data-nav-toggle`, `data-reset-on-success`).
- Scripts are self-hosted under `static/js/` with the version in the filename. No CDN script URLs.
- Inline `style="..."` attributes are fine (`'unsafe-inline'` stays in `style-src`).
- External images and fonts must be listed in `img-src` / `font-src`.
- Embedded players are limited to `videoFrameSrc` in `internal/handler/video.go` (YouTube, Vimeo, Bunny Stream). Add a provider there and in `embedURL` together.

A wrong CSP silently breaks pages in production.

### Tests
`go test ./...` covers the DB settings and migrations, the CSP and template rules, static caching and `/health`. CI also runs `gofmt` and `go vet`.

### sqlc
- Config: `sqlc.yaml`
- Schema: `sql/schema.sql`
- Queries: `sql/queries.sql`
- Generated code: `internal/db/` (never edit generated files)
