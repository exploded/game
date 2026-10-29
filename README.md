# StockGame

A fantasy stock market game at https://game.mchugh.au. Players join a game, get $1,000,000 of virtual cash, and trade real S&P 500 stocks at live prices during the US session (closing prices outside it). The leaderboard ranks players by portfolio value.

[FEATURES.md](FEATURES.md) lists every feature with its routes and files.

## Stack

- Go 1.26, chi, `html/template` and htmx 4
- SQLite through `modernc.org/sqlite` (pure Go, no CGO) and sqlc
- Email sign-in (a link and a 6-digit code, no passwords) and all other email through Amazon SES. [docs/emails.md](docs/emails.md) lists every email.
- Prices, company profiles and statistics from Twelve Data (Venture plan: 610 credits a minute, external display allowed), S&P 500 news from TickerTick (none for ASX: TickerTick only knows US tickers)
- Chart.js and TradingView Lightweight Charts, self-hosted in `static/js/`

Templates, static files and the schema are embedded in the binary (`assets.go`), so the binary is the whole app apart from `.env` and `game.db`.

## Run locally

1. Copy `.env.example` to `.env` and fill in `ADMIN_EMAIL` and `TWELVEDATA_API_KEY`. Without SES credentials, emails print to the console: copy the sign-in link from there.
2. In PowerShell, in `C:\Projects\go\game`, run `build.bat`. It builds `game.exe` and starts it on http://localhost:8888.

Template, CSS and JS changes need a rebuild because they're embedded.

## Test

```
go test ./...
```

The tests cover the SQLite settings and migrations, sign-in, email preferences and the outbox, the CSP rules for templates, static file caching, and `/health`. CI also runs `gofmt` and `go vet`.

## Configuration

All settings come from `.env` (or the environment, which takes priority):

| Variable | Purpose |
|---|---|
| `BASE_URL` | Public URL, used for links in emails |
| `SES_ACCESS_KEY_ID`, `SES_SECRET_ACCESS_KEY` | IAM user with `ses:SendRawEmail`. Without them, dev prints emails to the console and production keeps them queued. |
| `SES_REGION` | Default `ap-southeast-2` |
| `MAIL_FROM` | Sender, such as `StockGame <stockgame@mchugh.au>`. Must be an SES identity. |
| `NEWS_REPLY_TO` | Reply-to address for news from Rob |
| `NEWS_SENDER` | Sender name and postal address for the newsletter footer. Newsletters can't be sent without it (Spam Act). |
| `ADMIN_EMAIL` | Comma-separated emails that get the admin pages |
| `PORT` | Listen port. Default `8888`. |
| `PROD` | `1` in production: HSTS, and listens on `127.0.0.1` only |
| `BIND_ADDR` | Overrides the listen address |
| `TIMEZONE` | IANA zone for game start and end dates. Default `UTC`. |
| `TWELVEDATA_API_KEY` | Price data |
| `TWELVEDATA_SYMBOLS` | Limits the Twelve Data jobs to these symbols, such as `MSFT,AMZN,BRK.B,IBM`. Set it in dev: dev and production share the key's 610 credits a minute. Unset in production. |
| `MONITOR_URL`, `MONITOR_API_KEY` | Ship warnings and errors to monitor.mchugh.au |

## Deploy

Pushing to `master` runs the **Test and Deploy** workflow. It tests, cross-compiles a Linux binary, copies the binary and `scripts/deploy-game` to the Linode, and runs the deploy script. The script installs the binary, enables and restarts the `game` service, and fails the run unless `/health` returns `200`.

The app runs as `www-data` from `/var/www/game` on port 8888, behind Caddy. `scripts/server-setup.sh` provisions a fresh server and prints the Caddy block.

The repo needs these Actions secrets: `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_PORT` and `DEPLOY_SSH_KEY`.

## Monitoring

- Caddy access logs go to monitor through `import access_log`.
- Warnings and errors ship to monitor through `logship`.
- `/health` returns `200 ok` when the app is up and the database answers.
