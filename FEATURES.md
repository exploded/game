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
When a game has `allow_short` enabled, players can sell shares they don't own to open short positions (negative holdings). Short positions are tracked with negative quantities and revalued like normal holdings.

- **Files:** Updated `internal/handler/trade.go`, updated `templates/pages/trade/form.html`

### 4. Dividend Simulation
Admin can simulate dividend payments for any stock. All participants holding the stock in active games receive cash credited to their balance (converted to game currency if needed). Notifications are sent to each recipient.

- **Routes:** `/admin/dividend` (POST)
- **Files:** `internal/handler/gameevents.go`, updated `templates/pages/admin/index.html`

### 5. Stock Splits
Admin can simulate stock splits (e.g. 2:1, 3:1). All holdings of the stock across active games are adjusted — quantity multiplied and average cost divided by the split ratio. Notifications sent to holders.

- **Routes:** `/admin/stock-split` (POST)
- **Files:** `internal/handler/gameevents.go`, updated `templates/pages/admin/index.html`

---

## Social & Competition

### 6. Invite Links
Game creators can generate shareable invite codes. Anyone with the link can join the game directly (respecting max participants and game status). Invite codes track usage count and can have a max uses limit.

- **Routes:** `/games/{id}/invite` (create), `/invite/{code}` (join)
- **Files:** `internal/handler/invite.go`

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

### 16. Game Templates
Preset game configurations for quick creation. Users can create custom templates with name, markets, currency, duration, starting balance, trade fee, and short selling settings. Create a game from any template with one click.

- **Routes:** `/templates` (GET/POST), `/templates/{tid}/create-game` (POST)
- **Files:** `internal/handler/gametemplates.go`, `templates/pages/templates/list.html`

### 17. Recurring Games
Games can be set to recurring (weekly or monthly). When a recurring game finishes, the scheduler automatically creates a new game with the same settings and the next date range. The creator is auto-joined and notified.

- **Schema:** `games.recurring_interval` column (weekly/monthly/NULL)
- **Files:** `internal/market/recurring.go`, updated game create form

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
- Recurring games created

Unread count shown as a badge in the nav bar (HTMX polled on page load). Mark individual or all notifications as read.

- **Routes:** `/notifications`, `/notifications/count`, `/notifications/{nid}/read`, `/notifications/read-all`
- **Files:** `internal/handler/notifications.go`, `templates/pages/notifications/index.html`

### 20. Game History/Archive
Browse finished and cancelled games with pagination. Accessible from the nav bar.

- **Routes:** `/history`
- **Files:** `internal/handler/history.go`, `templates/pages/history/index.html`

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
