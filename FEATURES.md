# StockGame Features

## Trading & Market Mechanics

### 1. Limit Orders
Place buy/sell orders at a target price. Orders are automatically checked against latest prices by the background scheduler and filled when the condition is met. Orders can be cancelled or set to expire on a date.

- **Routes:** `/games/{id}/orders`, `/games/{id}/orders/{sid}` (create), `/games/{id}/orders/{oid}/cancel`
- **Files:** `internal/handler/limitorders.go`, `templates/pages/orders/list.html`, `internal/market/orders.go`

### 2. Watchlist
Bookmark stocks you're tracking without buying. Add/remove via HTMX toggle buttons on stock detail pages. View all watched stocks with latest prices from the watchlist page.

- **Routes:** `/games/{id}/watchlist`, `/games/{id}/watchlist/{sid}` (add/remove)
- **Files:** `internal/handler/watchlist.go`, `templates/pages/watchlist/index.html`

### 3. Short Selling
When a game has `allow_short` enabled, players can sell shares they don't own to open short positions (negative holdings). Short positions are tracked with negative quantities and revalued like normal holdings. A short's cost is the negative of what the sale raised, so its return is up when the price falls.

- **Files:** Updated `internal/handler/trade.go`, updated `templates/pages/trade/form.html`

### 4. Dividend Simulation
Admin can simulate dividend payments for any stock. All participants holding the stock in active games receive cash credited to their balance (converted to game currency if needed). Notifications are sent to each recipient.

- **Routes:** `/admin/dividend` (POST)
- **Files:** `internal/handler/gameevents.go`, updated `templates/pages/admin/index.html`

### 5. Stock Splits
Admin can simulate stock splits (e.g. 2:1, 3:1). All holdings of the stock across active games are adjusted — quantity multiplied by the split ratio, with the total cost unchanged, so the average cost falls to match. Notifications sent to holders.

- **Routes:** `/admin/stock-split` (POST)
- **Files:** `internal/handler/gameevents.go`, updated `templates/pages/admin/index.html`

---

## Social & Competition

### 6. Invite a Friend
Any player can invite a friend by email from the game's home page while the game's referral bonus is above 0. The friend gets a link to `/invite/{code}`, which remembers the invite and shows the sign-up form. If they create an account, they join the game and the inviter gets the referral bonus (`referral_bonus_pct` of the starting cash). Existing accounts earn nothing. Each player gets the bonus once per game (so once a month); each invite pays once, each player can send 10 a day, and each address is invited once per game.

- **Routes:** `POST /games/{id}/invite` (send), `/invite/{code}` (landing, public)
- **Files:** `internal/handler/invite.go`, `internal/mail/templates/invite.tmpl`

### 7. Chat
Per-game discussion thread. Participants can send messages (max 500 chars). HTMX-powered — new messages appear inline without page reload. Messages are displayed newest-first.

- **Routes:** `/games/{id}/chat` (GET/POST)
- **Files:** `internal/handler/chat.go`, `templates/pages/chat/index.html`

### 8. Achievements
11 achievements that are automatically granted after trades:
- **First Trade** — execute your first trade
- **Active Trader** / **Trading Machine** — 10 / 50 trades
- **Rising Star** / **Market Wizard** — 10% / 25% return
- **Diversified** / **Portfolio Manager** — hold 5 / 10 stocks
- **Champion** / **Podium Finish** — 1st place / top 3
- **Social Player** — join 3+ games
- **Market Watcher** — add 10 stocks to watchlist

Achievements are seeded on startup. Players get in-app notifications when earned.

- **Routes:** `/achievements`
- **Files:** `internal/handler/achievements.go`, `templates/pages/achievements/index.html`

### 9. Portfolio Comparison
Side-by-side view of two players' holdings in the same game. Select any public participant to compare against. Includes a Chart.js overlay of both portfolios' value over time.

- **Routes:** `/games/{id}/compare?vs={participantID}`
- **Files:** `internal/handler/analytics.go` (PortfolioComparison), `templates/pages/analytics/compare.html`

### 10. Activity Feed
Public feed of recent trades and joins in a game. Shows player name, action (buy/sell/join), and detail. Respects participant privacy settings.

- **Routes:** `/games/{id}/activity`
- **Files:** `internal/handler/activity.go`, `templates/pages/activity/index.html`

### 11a. Ask Rob (questions)
The Questions tab of Learn (11c). Members ask questions, pick a topic (or Claude picks one) and vote for the ones they want answered. Sorted by votes or newest, filtered by topic and by answered or not. Anyone can read them. Admins (Rob) answer in writing, with a video from a given time, or live on air from a stream page; the asker and everyone who voted get the answer by email (Community email). On-air answers are emailed once the stream ends, with a link to the moment in the recording. Askers can delete a question until it's answered; admins can remove any question. The old `/questions` links redirect.

- **Routes:** `/learn/questions`, `/learn/questions/{qid}`, `.../vote`, `.../delete`, `.../answer` (admin), `.../onair` (admin), `.../hide` (admin)
- **Files:** `internal/handler/questions.go`, `templates/pages/learn/questions.html`, `templates/pages/learn/question.html`

### 11c. Learn: Rob's videos and live streams
A public hub at `/learn` with tabs Watch, Live, Ask Rob and Courses. Built phones first: rows of cards scroll sideways, players load only when tapped.

- **Watch:** Rob's YouTube channel (`YOUTUBE_API_KEY`) and TikTok account (Display API, connected once from `/admin/learn`) sync automatically; Facebook and Instagram videos are added by pasting a link. Claude (`ANTHROPIC_API_KEY`) sorts each video into topics such as Retirement, Superannuation and Buying a home, and notes stocks it discusses, which then show on those stock pages. Rows by topic, Shorts, search, topic pages, video pages with related videos and "Ask Rob about this".
- **Live:** YouTube streams are picked up from the sync and checked every 2 minutes; TikTok, Facebook and Instagram streams are added by an admin, or started with "I'm live now". A "Rob is live" banner shows site-wide. Visitors can add a stream to their calendar (.ics); members can ask to be reminded. Live streams email: L1 when a stream is booked, L2 to reminder users when it starts.
- **Live Q&A queue:** a stream's page lists the most-voted open questions, refreshing every 20 seconds while live. Rob taps **Answered** as he answers each one.
- **Admin:** `/admin/learn` (sources, sync, paste a link, the video list), `/admin/learn/videos/{vid}` (topics, pin, hide), `/admin/learn/topics`, `/admin/learn/live`.
- Every Learn page ends with Rob's licensee's general advice warning (`learnDisclaimer` in `learn.go`; `LEARN_DISCLAIMER` overrides it).
- **Files:** `internal/media/` (sync, classifier, live emails), `internal/handler/learn.go`, `internal/handler/admin_learn.go`, `internal/handler/video.go`, `templates/pages/learn/`, `templates/pages/admin/learn*.html`

### 11b. Courses
Pre-recorded video lessons for signed-in members. Videos are hosted on YouTube (unlisted), Vimeo or Bunny Stream and embedded; the CSP's `frame-src` allows only those players. Members see their progress and mark lessons done. Admins build courses as drafts on `/admin/courses`, preview them, and publish; the first publish can email members (Community email).

- **Routes:** `/courses`, `/courses/{slug}`, `/courses/{slug}/{lid}`, `/admin/courses`, `/admin/courses/{cid}`, `/admin/lessons/{lid}`
- **Files:** `internal/handler/courses.go`, `internal/handler/video.go`, `templates/pages/courses/`, `templates/pages/admin/course*.html`

---

## Analytics & Insights

### 11. Portfolio Performance Chart
Line chart of portfolio value over time using daily snapshot data. Already built into the portfolio page using Chart.js. Shows value trend with filled area.

- **Files:** `templates/pages/portfolio/index.html` (existing)

### 12. Sector Allocation Breakdown
Doughnut chart showing holdings distribution across sectors. Built with Chart.js. Shows sector name and value in a legend alongside the chart.

- **Routes:** `/games/{id}/sectors`
- **Files:** `internal/handler/analytics.go` (SectorBreakdown), `templates/pages/analytics/sectors.html`

### 13. Gain/Loss Per Holding
Table showing unrealised P&L for each holding — current value vs cost basis, with absolute and percentage gain/loss. Displayed on the sectors page alongside the sector chart.

- **Files:** `internal/handler/analytics.go` (SectorBreakdown), `templates/pages/analytics/sectors.html`

### 14. Benchmark Comparison
Line chart overlaying portfolio performance against the starting balance (flat line). Shows how the portfolio has performed relative to holding cash. Uses Chart.js with a dashed benchmark line.

- **Routes:** `/games/{id}/benchmark`
- **Files:** `internal/handler/analytics.go` (BenchmarkComparison), `templates/pages/analytics/benchmark.html`

### 15. Trade Analytics
Dashboard showing trade statistics: total trades, buy/sell counts, win rate, total bought/sold, realised P&L, best/worst trade by stock, and a closed positions table.

- **Routes:** `/games/{id}/analytics`
- **Files:** `internal/handler/analytics.go` (TradeAnalytics), `templates/pages/analytics/index.html`

---

## Game Enhancements

### 16. One game per month
There's one game each calendar month (quarterly until 31 October 2026). It starts at midnight on the 1st and ends at midnight after the month's last day, in `TIMEZONE`. Players can't create games, and they don't join this month's game: signing in takes them straight into it with their starting cash. The scheduler creates this month's game and the next one, named like "November 2026", each copying the settings of the game before it, so players can join the next game early. Admins change a game's settings at `/admin/games`: name, description, markets, player limit, trade fee, referral bonus, limit orders and short selling. Starting cash and currency are fixed once someone has joined. Finished games are listed on `/games` with their final standings.

- **Schema:** `games.quarter` holds the month (`2026-11`, unique; NULL for games from before quarterly games)
- **Routes:** `/games`, `/admin/games`, `/admin/games/{gid}` (GET/POST), `/admin/games/{gid}/cancel`
- **Files:** `internal/market/monthly.go`, `internal/handler/games.go`, `internal/handler/admin_games.go`, `templates/pages/admin/game*.html`

### 17. Game templates and recurring games (removed)
Replaced by quarterly games on 2026-10-30. The `game_templates` table and the `recurring_interval`, `parent_game_id` and `template_id` columns are still in the schema but unused.

### 18. Starting Portfolio
Game creators can configure a set of stocks that every player starts with when joining. Starting stocks are granted with zero cost basis (free). Managed via `game_starting_stocks` table.

- **Schema:** `game_starting_stocks` table
- **Files:** Updated `internal/handler/games.go` (GameJoin)

### 19. Notifications
In-app notification system. Users are notified about:
- Achievements earned
- Limit orders filled
- Dividends received
- Stock splits applied

Unread count shown as a badge in the nav bar (HTMX polled on page load). Mark individual or all notifications as read.

- **Routes:** `/notifications`, `/notifications/count`, `/notifications/{nid}/read`, `/notifications/read-all`
- **Files:** `internal/handler/notifications.go`, `templates/pages/notifications/index.html`

### 20. Past games
Finished and cancelled games are listed under Past games on `/games`. `/history` redirects there.

---

## Accounts & Email

### Email sign-in
No passwords and no Google account. `/login` emails a link and a 6-digit code that work once for 15 minutes; an unknown address creates the account, and `/welcome` asks for a display name and opt-in to news from Rob.

- **Routes:** `/login`, `/login/check`, `/login/code`, `/login/verify`, `/welcome`
- **Files:** `internal/handler/login.go`, `internal/auth/session.go`, `templates/pages/auth/`

### Email
Every email goes through an outbox (`email_outbox`) and Amazon SES. People choose categories on Settings, and every non-account email has a one-click unsubscribe link. Trade and game emails come from `internal/notify`, which the scheduler runs each minute. Admins send newsletters from `/admin/email`. `docs/emails.md` lists every email.

- **Routes:** `/settings/email`, `/email/unsubscribe`, `/admin/email`
- **Files:** `internal/mail/`, `internal/notify/`, `internal/handler/email.go`, `internal/handler/admin_email.go`

---

## Admin & Data

### 21. Price Data Health Dashboard
Admin page showing price data gaps: how many stocks are missing prices for today, which specific stocks, oldest price data date, and total active stock count.

- **Routes:** `/admin/prices/health`
- **Files:** Updated `internal/handler/admin.go`, `templates/pages/admin/pricehealth.html`

### 22. User Activity Metrics
Admin page showing user engagement: most active users ranked by trade count, with game count and last login. Helps identify power users and inactive accounts.

- **Routes:** `/admin/activity`
- **Files:** Updated `internal/handler/admin.go`, `templates/pages/admin/activity.html`

### 23. Export to CSV
Download transaction history or portfolio snapshots as CSV files. Available from the game detail page for any game the user has joined.

- **Routes:** `/games/{id}/export/transactions`, `/games/{id}/export/snapshots`
- **Files:** `internal/handler/export.go`

---

## UI Enhancements

- **Responsive nav** — hamburger menu on mobile with all new links
- **Notification badge** — HTMX-polled unread count in nav
- **Game detail** — action buttons for all new features (orders, watchlist, analytics, chat, activity, CSV export, invite)
- **Trade form** — limit order section, short selling indicator
- **Stock detail** — HTMX watch/unwatch toggle button
- **Admin dashboard** — dividend/split simulation forms, links to price health and user activity
