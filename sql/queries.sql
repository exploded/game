-- =============================================================================
-- Users
-- =============================================================================

-- name: CreateUser :one
INSERT INTO users (email, name, email_token)
VALUES (?, ?, ?)
RETURNING *;

-- name: TouchLastLogin :exec
UPDATE users SET last_login = datetime('now') WHERE id = ?;

-- name: UpdateUserName :exec
UPDATE users SET name = ? WHERE id = ?;

-- name: GetUserByEmailToken :one
SELECT * FROM users WHERE email_token = ? AND deleted_at IS NULL;

-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: SetAdmin :exec
UPDATE users SET is_admin = ? WHERE id = ?;

-- name: SetLastGame :exec
UPDATE users SET last_game_id = ? WHERE id = ?;

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at DESC;

-- name: SoftDeleteUser :exec
UPDATE users SET
    name = 'Deleted User',
    email = 'deleted-' || CAST(id AS TEXT) || '@deleted.local',
    picture_url = NULL,
    deleted_at = datetime('now')
WHERE id = ?;

-- =============================================================================
-- Sessions
-- =============================================================================

-- name: CreateSession :one
INSERT INTO sessions (id, user_id, expires_at)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions
WHERE id = ? AND expires_at > datetime('now');

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= datetime('now');

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = ?;

-- =============================================================================
-- Sign-in requests
-- =============================================================================

-- name: CreateLoginRequest :exec
INSERT INTO login_requests (id, email, token_hash, code_hash, expires_at)
VALUES (?, ?, ?, ?, ?);

-- name: CountRecentLoginRequests :one
SELECT COUNT(*) FROM login_requests
WHERE email = ? AND created_at > datetime('now', '-1 hour');

-- name: GetLoginRequest :one
SELECT * FROM login_requests
WHERE id = ? AND used_at IS NULL AND expires_at > datetime('now');

-- name: GetLoginRequestByToken :one
SELECT * FROM login_requests
WHERE token_hash = ? AND used_at IS NULL AND expires_at > datetime('now');

-- name: AddLoginAttempt :one
UPDATE login_requests SET attempts = attempts + 1 WHERE id = ?
RETURNING attempts;

-- name: UseLoginRequest :execrows
UPDATE login_requests SET used_at = datetime('now')
WHERE id = ? AND used_at IS NULL;

-- name: DeleteOldLoginRequests :exec
DELETE FROM login_requests WHERE created_at < datetime('now', '-1 day');

-- =============================================================================
-- Email
-- =============================================================================

-- name: ListEmailPrefs :many
SELECT * FROM email_prefs WHERE user_id = ?;

-- name: SetEmailPref :exec
INSERT INTO email_prefs (user_id, category, enabled, updated_at)
VALUES (?, ?, ?, datetime('now'))
ON CONFLICT(user_id, category) DO UPDATE SET
    enabled    = excluded.enabled,
    updated_at = excluded.updated_at
WHERE email_prefs.enabled != excluded.enabled;

-- name: ListNewsSubscribers :many
SELECT u.id FROM users u
JOIN email_prefs p ON p.user_id = u.id AND p.category = 'news' AND p.enabled = 1
WHERE u.deleted_at IS NULL
ORDER BY u.id;

-- name: CreateBroadcast :exec
INSERT INTO email_broadcasts (subject, body, sent_by, recipients) VALUES (?, ?, ?, ?);

-- name: ListBroadcasts :many
SELECT b.*, COALESCE(u.name, '') AS sent_by_name
FROM email_broadcasts b
LEFT JOIN users u ON u.id = b.sent_by
ORDER BY b.id DESC
LIMIT 20;

-- name: QueueEmail :exec
INSERT INTO email_outbox (user_id, to_email, category, template, subject, text_body, html_body, reply_to, unsubscribe_url)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListDueEmails :many
SELECT * FROM email_outbox
WHERE status = 'pending' AND send_after <= datetime('now')
ORDER BY id
LIMIT ?;

-- name: MarkEmailSent :exec
UPDATE email_outbox SET status = 'sent', sent_at = datetime('now'), attempts = attempts + 1, last_error = ''
WHERE id = ?;

-- name: MarkEmailRetry :exec
UPDATE email_outbox SET attempts = attempts + 1, last_error = ?, send_after = ?
WHERE id = ?;

-- name: MarkEmailFailed :exec
UPDATE email_outbox SET status = 'failed', attempts = attempts + 1, last_error = ?
WHERE id = ?;

-- Sign-in emails hold a live link and code, so their bodies are cleared once
-- sent. Other sent mail is kept for 90 days.
-- name: PruneEmailOutbox :exec
UPDATE email_outbox SET text_body = '', html_body = ''
WHERE template = 'login' AND status != 'pending' AND text_body != '';

-- name: DeleteOldEmails :exec
DELETE FROM email_outbox WHERE status != 'pending' AND created_at < datetime('now', '-90 days');

-- =============================================================================
-- Games
-- =============================================================================

-- name: CreateGame :one
INSERT INTO games (created_by, name, description, markets, starting_balance, base_currency, max_participants, start_date, end_date, allow_short, trade_fee, referral_bonus_pct, is_public, portfolio_visibility, credit_interest_rate, leverage_interest_rate, min_stock_price, max_stock_price, margin_trading, limit_orders, stop_loss, fractional_shares, after_hours_trading, quarter)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetGame :one
SELECT * FROM games WHERE id = ?;

-- A month's game. The column is still called quarter: games were quarterly
-- until November 2026.
-- name: GetGameByMonth :one
SELECT * FROM games WHERE quarter = ?;

-- The most recent month's game, whose settings the next one copies.
-- name: GetLatestMonthlyGame :one
SELECT * FROM games WHERE quarter IS NOT NULL ORDER BY start_date DESC LIMIT 1;

-- Who monthly games are created by: the first admin, else the first user.
-- name: GetGameOwner :one
SELECT id FROM users WHERE deleted_at IS NULL ORDER BY is_admin DESC, id LIMIT 1;

-- name: ListGamesWithPlayers :many
SELECT g.*, (SELECT COUNT(*) FROM participants p WHERE p.game_id = g.id) AS players
FROM games g
ORDER BY g.start_date DESC, g.id DESC;

-- name: ListUserGames :many
SELECT g.* FROM games g
JOIN participants p ON p.game_id = g.id
WHERE p.user_id = ?
ORDER BY g.start_date DESC;

-- name: UpdateGameStatus :exec
UPDATE games SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- The settings admins can change. Dates come from the quarter.
-- name: UpdateGameSettings :exec
UPDATE games SET
    name = ?, description = ?, markets = ?, starting_balance = ?,
    base_currency = ?, max_participants = ?, trade_fee = ?, allow_short = ?,
    limit_orders = ?, referral_bonus_pct = ?, credit_interest_rate = ?,
    after_hours_trading = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: CountParticipants :one
SELECT COUNT(*) FROM participants WHERE game_id = ?;

-- name: AdvancePendingGames :exec
UPDATE games SET status = 'active', updated_at = datetime('now')
WHERE status = 'pending' AND start_date <= ?;

-- name: FinishExpiredGames :exec
UPDATE games SET status = 'finished', updated_at = datetime('now')
WHERE status = 'active' AND end_date < ?;

-- =============================================================================
-- Participants
-- =============================================================================

-- name: JoinGame :one
INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetParticipant :one
SELECT * FROM participants WHERE id = ?;

-- name: GetParticipantByGameAndUser :one
SELECT * FROM participants WHERE game_id = ? AND user_id = ?;

-- name: ListParticipantsByGame :many
SELECT p.*, u.name AS user_name, u.picture_url AS user_picture
FROM participants p
JOIN users u ON u.id = p.user_id
WHERE p.game_id = ?
ORDER BY p.portfolio_value DESC;

-- name: ListPublicParticipantsByGame :many
SELECT p.*, u.name AS user_name, u.picture_url AS user_picture
FROM participants p
JOIN users u ON u.id = p.user_id
WHERE p.game_id = ? AND p.is_public = 1
ORDER BY p.portfolio_value DESC;

-- name: UpdateCashBalance :exec
UPDATE participants SET cash_balance = ? WHERE id = ?;

-- name: UpdatePortfolioValue :exec
UPDATE participants SET portfolio_value = ? WHERE id = ?;

-- name: UpdateParticipantVisibility :exec
UPDATE participants SET is_public = ? WHERE id = ?;

-- =============================================================================
-- Stocks
-- =============================================================================

-- name: InsertStock :exec
INSERT OR IGNORE INTO stocks (symbol, name, market, sector, currency)
VALUES (?, ?, ?, ?, ?);

-- name: GetStock :one
SELECT * FROM stocks WHERE id = ?;

-- name: GetStockBySymbol :one
SELECT * FROM stocks WHERE symbol = ? AND market = ?;

-- name: SearchStocks :many
SELECT * FROM stocks
WHERE is_active = 1
  AND (market = ? OR ? = 'both')
  AND (symbol LIKE ? OR name LIKE ?)
  AND id IN (SELECT DISTINCT stock_id FROM stock_prices)
ORDER BY symbol ASC
LIMIT 20;

-- name: ListStocksByMarket :many
SELECT * FROM stocks
WHERE is_active = 1 AND (market = ? OR ? = 'both')
  AND id IN (SELECT DISTINCT stock_id FROM stock_prices)
ORDER BY symbol ASC;

-- name: ListAllStocksByMarket :many
SELECT * FROM stocks
WHERE is_active = 1 AND (market = ? OR ? = 'both')
ORDER BY symbol ASC;

-- name: ListStocksWithoutHistory :many
-- Stocks in a market with fewer than 30 rows in stock_prices: never backfilled,
-- or only holding the odd row from intraday quotes or a daily fetch that ran
-- before the backfill finished. Used to resume an interrupted backfill without
-- re-fetching stocks we already have.
SELECT * FROM stocks
WHERE is_active = 1 AND market = ?
  AND id NOT IN (
    SELECT stock_id FROM stock_prices GROUP BY stock_id HAVING COUNT(*) >= 30
  )
ORDER BY symbol ASC;

-- name: CountStocks :one
SELECT COUNT(*) FROM stocks WHERE is_active = 1;

-- =============================================================================
-- Stock Prices
-- =============================================================================

-- name: UpsertPrice :exec
INSERT INTO stock_prices (stock_id, date, open, high, low, close, volume)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(stock_id, date) DO UPDATE SET
    open = excluded.open, high = excluded.high, low = excluded.low,
    close = excluded.close, volume = excluded.volume;

-- name: GetLatestPrice :one
SELECT * FROM stock_prices
WHERE stock_id = ?
ORDER BY date DESC LIMIT 1;

-- name: ListLatestPrices :many
-- The newest two bars: today's (or the last session's) and the close before it.
SELECT * FROM stock_prices
WHERE stock_id = ?
ORDER BY date DESC LIMIT 2;

-- name: GetPriceOnDate :one
SELECT * FROM stock_prices
WHERE stock_id = ? AND date = ?;

-- name: ListPriceHistory :many
SELECT * FROM stock_prices
WHERE stock_id = ? AND date >= ? AND date <= ?
ORDER BY date ASC;

-- name: GetLatestPriceDate :one
SELECT MAX(date) AS latest_date FROM stock_prices
WHERE stock_id IN (SELECT id FROM stocks WHERE market = ?);

-- =============================================================================
-- Stock Profiles
-- =============================================================================

-- name: GetStockProfile :one
SELECT * FROM stock_profiles WHERE stock_id = ?;

-- name: CountStockProfiles :one
SELECT COUNT(*) FROM stock_profiles;

-- name: CountStockProfilesWithLogo :one
SELECT COUNT(*) FROM stock_profiles WHERE logo != '';

-- name: ListStocksNeedingProfile :many
-- Active stocks with no profile row, or one last refreshed before the cutoff.
SELECT s.* FROM stocks s
LEFT JOIN stock_profiles p ON p.stock_id = s.id
WHERE s.is_active = 1 AND s.market = ?
  AND (p.stock_id IS NULL OR p.updated_at < ?)
ORDER BY s.symbol ASC;

-- name: UpsertStockProfileInfo :exec
-- Company details from Twelve Data /profile. Leaves the statistics columns,
-- logo, ipo_date and peers alone.
INSERT INTO stock_profiles (
    stock_id, exchange, industry, description, type, ceo, employees,
    website, address, city, state, country, phone, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT(stock_id) DO UPDATE SET
    exchange = excluded.exchange, industry = excluded.industry,
    description = excluded.description, type = excluded.type,
    ceo = excluded.ceo, employees = excluded.employees,
    website = excluded.website, address = excluded.address,
    city = excluded.city, state = excluded.state,
    country = excluded.country, phone = excluded.phone,
    updated_at = excluded.updated_at;

-- name: UpsertStockStatistics :exec
-- Metrics from Twelve Data /statistics. Leaves the company details alone.
INSERT INTO stock_profiles (
    stock_id,
    market_cap, enterprise_value, trailing_pe, forward_pe, peg_ratio,
    price_to_book, price_to_sales, ev_to_revenue, ev_to_ebitda,
    revenue, revenue_per_share, quarterly_rev_growth, gross_profit, ebitda,
    net_income, diluted_eps, quarterly_earn_growth,
    total_cash, cash_per_share, total_debt, debt_to_equity, current_ratio,
    book_value_per_share, operating_cash_flow, free_cash_flow,
    gross_margin, operating_margin, profit_margin, return_on_equity, return_on_assets,
    shares_outstanding, float_shares, avg_10_volume, avg_90_volume, shares_short,
    short_ratio, short_pct_outstanding, pct_held_insiders, pct_held_institutions,
    week_52_high, week_52_low, week_52_change, beta, day_50_ma, day_200_ma,
    forward_div_rate, forward_div_yield, trailing_div_rate, trailing_div_yield,
    avg_div_yield_5y, payout_ratio, dividend_frequency, dividend_date,
    ex_dividend_date, last_split_factor, last_split_date,
    fiscal_year_ends, most_recent_quarter, updated_at
) VALUES (
    ?,
    ?, ?, ?, ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?, ?, ?,
    ?, ?, ?,
    ?, ?, ?, ?, ?,
    ?, ?, ?,
    ?, ?, ?, ?, ?,
    ?, ?, ?, ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?, ?, ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?,
    ?, ?, datetime('now')
)
ON CONFLICT(stock_id) DO UPDATE SET
    market_cap = excluded.market_cap, enterprise_value = excluded.enterprise_value,
    trailing_pe = excluded.trailing_pe, forward_pe = excluded.forward_pe,
    peg_ratio = excluded.peg_ratio, price_to_book = excluded.price_to_book,
    price_to_sales = excluded.price_to_sales, ev_to_revenue = excluded.ev_to_revenue,
    ev_to_ebitda = excluded.ev_to_ebitda,
    revenue = excluded.revenue, revenue_per_share = excluded.revenue_per_share,
    quarterly_rev_growth = excluded.quarterly_rev_growth,
    gross_profit = excluded.gross_profit, ebitda = excluded.ebitda,
    net_income = excluded.net_income, diluted_eps = excluded.diluted_eps,
    quarterly_earn_growth = excluded.quarterly_earn_growth,
    total_cash = excluded.total_cash, cash_per_share = excluded.cash_per_share,
    total_debt = excluded.total_debt, debt_to_equity = excluded.debt_to_equity,
    current_ratio = excluded.current_ratio,
    book_value_per_share = excluded.book_value_per_share,
    operating_cash_flow = excluded.operating_cash_flow,
    free_cash_flow = excluded.free_cash_flow,
    gross_margin = excluded.gross_margin, operating_margin = excluded.operating_margin,
    profit_margin = excluded.profit_margin, return_on_equity = excluded.return_on_equity,
    return_on_assets = excluded.return_on_assets,
    shares_outstanding = excluded.shares_outstanding, float_shares = excluded.float_shares,
    avg_10_volume = excluded.avg_10_volume, avg_90_volume = excluded.avg_90_volume,
    shares_short = excluded.shares_short, short_ratio = excluded.short_ratio,
    short_pct_outstanding = excluded.short_pct_outstanding,
    pct_held_insiders = excluded.pct_held_insiders,
    pct_held_institutions = excluded.pct_held_institutions,
    week_52_high = excluded.week_52_high, week_52_low = excluded.week_52_low,
    week_52_change = excluded.week_52_change, beta = excluded.beta,
    day_50_ma = excluded.day_50_ma, day_200_ma = excluded.day_200_ma,
    forward_div_rate = excluded.forward_div_rate,
    forward_div_yield = excluded.forward_div_yield,
    trailing_div_rate = excluded.trailing_div_rate,
    trailing_div_yield = excluded.trailing_div_yield,
    avg_div_yield_5y = excluded.avg_div_yield_5y, payout_ratio = excluded.payout_ratio,
    dividend_frequency = excluded.dividend_frequency,
    dividend_date = excluded.dividend_date, ex_dividend_date = excluded.ex_dividend_date,
    last_split_factor = excluded.last_split_factor,
    last_split_date = excluded.last_split_date,
    fiscal_year_ends = excluded.fiscal_year_ends,
    most_recent_quarter = excluded.most_recent_quarter,
    updated_at = excluded.updated_at;

-- name: UpdateStockLogo :exec
UPDATE stock_profiles SET logo = ? WHERE stock_id = ?;

-- name: DeleteStockNews :exec
DELETE FROM stock_news WHERE stock_id = ?;

-- name: InsertStockNews :exec
INSERT INTO stock_news (stock_id, headline, summary, source, url, published_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListStockNews :many
SELECT * FROM stock_news WHERE stock_id = ?
ORDER BY published_at DESC LIMIT ?;

-- name: GetStockNewsAge :one
-- Returns the created_at of the newest news row for a stock, or NULL if none.
SELECT CAST(MAX(created_at) AS TEXT) FROM stock_news WHERE stock_id = ?;

-- name: ListStockRecommendations :many
SELECT * FROM stock_recommendations WHERE stock_id = ?
ORDER BY period DESC LIMIT ?;

-- =============================================================================
-- Exchange Rates
-- =============================================================================

-- name: UpsertExchangeRate :exec
INSERT INTO exchange_rates (date, rate_aud_usd)
VALUES (?, ?)
ON CONFLICT(date) DO UPDATE SET rate_aud_usd = excluded.rate_aud_usd;

-- name: GetLatestExchangeRate :one
SELECT * FROM exchange_rates ORDER BY date DESC LIMIT 1;

-- name: GetExchangeRateOnDate :one
SELECT * FROM exchange_rates WHERE date = ?;

-- =============================================================================
-- Holdings
-- =============================================================================

-- name: UpsertHolding :exec
INSERT INTO holdings (participant_id, stock_id, quantity, avg_cost, cost_basis, current_value, updated_at)
VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT(participant_id, stock_id) DO UPDATE SET
    quantity = excluded.quantity,
    avg_cost = excluded.avg_cost,
    cost_basis = excluded.cost_basis,
    current_value = excluded.current_value,
    updated_at = datetime('now');

-- name: GetHolding :one
SELECT * FROM holdings WHERE participant_id = ? AND stock_id = ?;

-- name: ListHoldings :many
SELECT h.*, s.symbol, s.name AS stock_name, s.market, s.currency
FROM holdings h
JOIN stocks s ON s.id = h.stock_id
WHERE h.participant_id = ?
ORDER BY h.current_value DESC;

-- name: DeleteHolding :exec
DELETE FROM holdings WHERE participant_id = ? AND stock_id = ?;

-- name: UpdateHoldingValue :exec
UPDATE holdings SET current_value = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: ListAllActiveHoldings :many
SELECT h.*, s.symbol, s.market, s.currency, p.game_id
FROM holdings h
JOIN stocks s ON s.id = h.stock_id
JOIN participants p ON p.id = h.participant_id
JOIN games g ON g.id = p.game_id
WHERE g.status = 'active';

-- =============================================================================
-- Transactions
-- =============================================================================

-- name: InsertTransaction :one
INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total, exchange_rate_used, fee)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListTransactions :many
SELECT t.*, s.symbol, s.name AS stock_name, s.market, s.currency
FROM transactions t
JOIN stocks s ON s.id = t.stock_id
WHERE t.participant_id = ?
ORDER BY t.created_at DESC
LIMIT ? OFFSET ?;

-- name: CountTransactions :one
SELECT COUNT(*) FROM transactions WHERE participant_id = ?;

-- =============================================================================
-- Portfolio Snapshots
-- =============================================================================

-- name: UpsertSnapshot :exec
INSERT INTO portfolio_snapshots (participant_id, date, cash_balance, holdings_value, total_value)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(participant_id, date) DO UPDATE SET
    cash_balance = excluded.cash_balance,
    holdings_value = excluded.holdings_value,
    total_value = excluded.total_value;

-- name: ListSnapshots :many
SELECT * FROM portfolio_snapshots
WHERE participant_id = ? AND date >= ? AND date <= ?
ORDER BY date ASC;

-- =============================================================================
-- Price Fetch Log
-- =============================================================================

-- name: InsertFetchLog :exec
INSERT INTO price_fetch_log (market, status, stocks_updated, error_msg, duration_ms)
VALUES (?, ?, ?, ?, ?);

-- name: GetLatestFetchLog :one
SELECT * FROM price_fetch_log
WHERE market = ?
ORDER BY created_at DESC LIMIT 1;

-- name: ListFetchLogs :many
SELECT * FROM price_fetch_log
ORDER BY created_at DESC LIMIT 50;

-- =============================================================================
-- Active Participants (for revaluation)
-- =============================================================================

-- name: ListActiveParticipants :many
SELECT p.*, g.base_currency
FROM participants p
JOIN games g ON g.id = p.game_id
WHERE g.status = 'active';

-- =============================================================================
-- Interest on cash
-- =============================================================================

-- name: ListInterestDue :many
-- Players owed interest for a day: in a game that ran that day, joined
-- before the day ended, holding cash, and not paid for it yet.
SELECT p.id, p.cash_balance, g.credit_interest_rate
FROM participants p
JOIN games g ON g.id = p.game_id
WHERE g.status = 'active'
  AND g.start_date <= sqlc.arg(day) AND g.end_date >= sqlc.arg(day)
  AND g.credit_interest_rate > 0
  AND p.cash_balance > 0
  AND p.joined_at < sqlc.arg(day_end)
  AND NOT EXISTS (SELECT 1 FROM interest_payments ip WHERE ip.participant_id = p.id AND ip.date = sqlc.arg(day));

-- name: InsertInterestPayment :exec
INSERT INTO interest_payments (participant_id, date, balance, rate, amount)
VALUES (?, ?, ?, ?, ?);

-- name: CreditCash :exec
-- Adds to cash and the portfolio value together, so the leaderboard moves
-- without waiting for a revaluation.
UPDATE participants
SET cash_balance = cash_balance + sqlc.arg(amount), portfolio_value = portfolio_value + sqlc.arg(amount)
WHERE id = sqlc.arg(id);

-- name: TotalInterest :one
SELECT CAST(COALESCE(SUM(amount), 0) AS INTEGER) FROM interest_payments WHERE participant_id = ?;

-- =============================================================================
-- Limit Orders (Feature 1)
-- =============================================================================

-- name: CreateLimitOrder :one
INSERT INTO limit_orders (participant_id, stock_id, type, quantity, limit_price, expires_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: CreateMarketOrder :one
INSERT INTO limit_orders (participant_id, stock_id, type, quantity, limit_price, kind)
VALUES (?, ?, ?, ?, 0, 'market')
RETURNING *;

-- name: GetLimitOrder :one
SELECT * FROM limit_orders WHERE id = ?;

-- name: ListOpenLimitOrders :many
SELECT lo.*, s.symbol, s.name AS stock_name, s.market, s.currency
FROM limit_orders lo
JOIN stocks s ON s.id = lo.stock_id
WHERE lo.participant_id = ? AND lo.status = 'open'
ORDER BY lo.created_at DESC;

-- name: ListAllOpenOrders :many
SELECT lo.*, s.symbol, s.market, s.currency, p.game_id
FROM limit_orders lo
JOIN stocks s ON s.id = lo.stock_id
JOIN participants p ON p.id = lo.participant_id
JOIN games g ON g.id = p.game_id
WHERE lo.status = 'open' AND g.status = 'active';

-- name: FillLimitOrder :exec
UPDATE limit_orders SET status = 'filled', filled_at = datetime('now') WHERE id = ?;

-- name: CancelLimitOrder :exec
UPDATE limit_orders SET status = 'cancelled' WHERE id = ?;

-- name: ExpireOldLimitOrders :many
UPDATE limit_orders SET status = 'expired'
WHERE status = 'open' AND expires_at IS NOT NULL AND expires_at < sqlc.arg(today)
RETURNING participant_id, stock_id, type, quantity, limit_price;

-- name: CountOpenOrders :one
SELECT COUNT(*) FROM limit_orders WHERE participant_id = ? AND status = 'open';

-- =============================================================================
-- Watchlist (Feature 2)
-- =============================================================================

-- name: AddToWatchlist :exec
INSERT OR IGNORE INTO watchlist (participant_id, stock_id) VALUES (?, ?);

-- name: RemoveFromWatchlist :exec
DELETE FROM watchlist WHERE participant_id = ? AND stock_id = ?;

-- name: ListWatchlist :many
SELECT w.*, s.symbol, s.name AS stock_name, s.market, s.sector, s.currency
FROM watchlist w
JOIN stocks s ON s.id = w.stock_id
WHERE w.participant_id = ?
ORDER BY w.created_at DESC;

-- name: IsOnWatchlist :one
SELECT COUNT(*) FROM watchlist WHERE participant_id = ? AND stock_id = ?;

-- =============================================================================
-- Invite Links (Feature 6)
-- =============================================================================

-- name: CreateInvite :one
INSERT INTO game_invites (game_id, code, created_by, max_uses, expires_at, email)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetInviteByCode :one
SELECT * FROM game_invites WHERE code = ?;

-- name: UseInvite :execrows
-- Counts one use, unless the invite is used up.
UPDATE game_invites SET use_count = use_count + 1
WHERE id = ? AND (max_uses IS NULL OR use_count < max_uses);

-- name: InviteSentTo :one
SELECT COUNT(*) FROM game_invites WHERE game_id = ? AND email = ?;

-- name: CountInvitesSentToday :one
-- Invites the user emailed in the last 24 hours, in any game.
SELECT COUNT(*) FROM game_invites
WHERE created_by = ? AND email IS NOT NULL AND created_at > datetime('now', '-1 day');

-- name: ListInvitesSent :many
SELECT gi.email, gi.use_count, gi.created_at
FROM game_invites gi
WHERE gi.game_id = ? AND gi.created_by = ? AND gi.email IS NOT NULL
ORDER BY gi.id DESC
LIMIT 20;

-- =============================================================================
-- Game Chat (Feature 7)
-- =============================================================================

-- name: CreateMessage :one
INSERT INTO game_messages (game_id, user_id, message)
VALUES (?, ?, ?)
RETURNING *;

-- name: ListMessages :many
SELECT gm.*, u.name AS user_name, u.picture_url AS user_picture
FROM game_messages gm
JOIN users u ON u.id = gm.user_id
WHERE gm.game_id = ?
ORDER BY gm.created_at DESC
LIMIT ? OFFSET ?;

-- name: CountMessages :one
SELECT COUNT(*) FROM game_messages WHERE game_id = ?;

-- =============================================================================
-- Achievements (Feature 8)
-- =============================================================================

-- name: UpsertAchievement :exec
-- Keeps each achievement's text in step with the code.
INSERT INTO achievements (key, name, description, icon)
VALUES (?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET name = excluded.name, description = excluded.description, icon = excluded.icon;

-- name: ListAchievements :many
SELECT * FROM achievements ORDER BY id;

-- name: GrantAchievement :exec
INSERT OR IGNORE INTO user_achievements (user_id, achievement_id, game_id)
VALUES (?, ?, ?);

-- name: ListUserAchievements :many
SELECT ua.*, a.key, a.name AS achievement_name, a.description, a.icon
FROM user_achievements ua
JOIN achievements a ON a.id = ua.achievement_id
WHERE ua.user_id = ?
ORDER BY ua.earned_at DESC;

-- name: GetAchievementByKey :one
SELECT * FROM achievements WHERE key = ?;

-- name: HasAchievement :one
SELECT COUNT(*) FROM user_achievements
WHERE user_id = ? AND achievement_id = ? AND (game_id = ? OR game_id IS NULL);

-- =============================================================================
-- Activity Feed (Feature 10)
-- =============================================================================

-- name: InsertActivity :exec
INSERT INTO activity_feed (game_id, user_id, action, detail, is_public)
VALUES (?, ?, ?, ?, ?);

-- name: ListActivityFeed :many
SELECT af.*, u.name AS user_name, u.picture_url AS user_picture
FROM activity_feed af
JOIN users u ON u.id = af.user_id
WHERE af.game_id = ? AND af.is_public = 1
ORDER BY af.created_at DESC
LIMIT ? OFFSET ?;

-- =============================================================================
-- Starting Portfolio (Feature 18)
-- =============================================================================

-- name: AddStartingStock :exec
INSERT OR REPLACE INTO game_starting_stocks (game_id, stock_id, quantity)
VALUES (?, ?, ?);

-- name: ListStartingStocks :many
SELECT gss.*, s.symbol, s.name AS stock_name, s.market, s.currency
FROM game_starting_stocks gss
JOIN stocks s ON s.id = gss.stock_id
WHERE gss.game_id = ?;

-- name: DeleteStartingStocks :exec
DELETE FROM game_starting_stocks WHERE game_id = ?;

-- =============================================================================
-- Notifications (Feature 19)
-- =============================================================================

-- name: CreateNotification :exec
INSERT INTO notifications (user_id, game_id, type, title, message)
VALUES (?, ?, ?, ?, ?);

-- name: ListUnemailedNotifications :many
SELECT n.id, n.user_id, n.game_id, n.type, n.title, n.message,
       COALESCE(g.name, '') AS game_name
FROM notifications n
LEFT JOIN games g ON g.id = n.game_id
WHERE n.emailed = 0
ORDER BY n.user_id, n.id
LIMIT 500;

-- name: MarkNotificationEmailed :exec
UPDATE notifications SET emailed = 1 WHERE id = ?;

-- name: ClaimGameEmail :execrows
INSERT OR IGNORE INTO game_emails (game_id, kind) VALUES (?, ?);

-- name: ListActiveGamesStartedSince :many
SELECT * FROM games WHERE status = 'active' AND start_date >= ?;

-- name: ListActiveGamesEndingOn :many
SELECT * FROM games WHERE status = 'active' AND end_date = ?;

-- name: ListFinishedGamesEndedSince :many
SELECT * FROM games WHERE status = 'finished' AND end_date >= ?;

-- name: ListUnreadNotifications :many
SELECT * FROM notifications
WHERE user_id = ? AND is_read = 0
ORDER BY created_at DESC
LIMIT 20;

-- name: ListNotifications :many
SELECT * FROM notifications
WHERE user_id = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: MarkNotificationRead :exec
UPDATE notifications SET is_read = 1 WHERE id = ? AND user_id = ?;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET is_read = 1 WHERE user_id = ? AND is_read = 0;

-- name: CountUnreadNotifications :one
SELECT COUNT(*) FROM notifications WHERE user_id = ? AND is_read = 0;

-- =============================================================================
-- Game History (Feature 20)
-- =============================================================================

-- name: ListFinishedGames :many
SELECT * FROM games WHERE status IN ('finished', 'cancelled')
ORDER BY end_date DESC
LIMIT ? OFFSET ?;


-- =============================================================================
-- Admin - Price Health (Feature 21)
-- =============================================================================

-- name: CountStocksMissingPrices :one
SELECT COUNT(*) FROM stocks s
WHERE s.is_active = 1
AND s.id NOT IN (
    SELECT sp.stock_id FROM stock_prices sp WHERE sp.date = sqlc.arg(target_date)
);

-- name: ListStocksMissingPrices :many
SELECT s.* FROM stocks s
WHERE s.is_active = 1
AND s.id NOT IN (
    SELECT sp.stock_id FROM stock_prices sp WHERE sp.date = sqlc.arg(target_date)
)
ORDER BY s.market, s.symbol
LIMIT 100;

-- name: GetOldestPriceDate :one
SELECT CAST(COALESCE(MIN(date), '') AS TEXT) AS oldest_date FROM stock_prices;

-- =============================================================================
-- Admin - User Activity (Feature 22)
-- =============================================================================

-- name: GetUserTradeCount :one
SELECT COUNT(*) FROM transactions t
JOIN participants p ON p.id = t.participant_id
WHERE p.user_id = ?;

-- name: ListMostActiveUsers :many
SELECT u.id, u.name, u.email, u.picture_url, u.last_login,
    (SELECT COUNT(*) FROM transactions t JOIN participants p ON p.id = t.participant_id WHERE p.user_id = u.id) AS trade_count,
    (SELECT COUNT(*) FROM participants p2 WHERE p2.user_id = u.id) AS game_count
FROM users u
ORDER BY trade_count DESC
LIMIT 50;

-- =============================================================================
-- Trade Analytics (Feature 15)
-- =============================================================================

-- name: ListAllTransactions :many
SELECT t.*, s.symbol, s.name AS stock_name, s.market, s.currency
FROM transactions t
JOIN stocks s ON s.id = t.stock_id
WHERE t.participant_id = ?
ORDER BY t.created_at ASC;

-- =============================================================================
-- Sector Holdings (Feature 12)
-- =============================================================================

-- name: ListHoldingsBySector :many
SELECT s.sector, SUM(h.current_value) AS total_value
FROM holdings h
JOIN stocks s ON s.id = h.stock_id
WHERE h.participant_id = ?
GROUP BY s.sector
ORDER BY total_value DESC;

-- =============================================================================
-- CSV Export Helpers (Feature 23)
-- =============================================================================

-- name: ListAllTransactionsForExport :many
SELECT t.created_at, t.type, s.symbol, s.name AS stock_name, s.market,
    t.quantity, t.price, t.total, t.converted_total, t.exchange_rate_used, t.fee
FROM transactions t
JOIN stocks s ON s.id = t.stock_id
WHERE t.participant_id = ?
ORDER BY t.created_at ASC;

-- name: ListSnapshotsForExport :many
SELECT date, cash_balance, holdings_value, total_value
FROM portfolio_snapshots
WHERE participant_id = ?
ORDER BY date ASC;

-- =============================================================================
-- Contact Messages
-- =============================================================================

-- name: CreateContactMessage :exec
INSERT INTO contact_messages (user_id, name, email, message)
VALUES (?, ?, ?, ?);

-- name: ListContactMessages :many
SELECT * FROM contact_messages
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: CountContactMessages :one
SELECT COUNT(*) FROM contact_messages;

-- name: CountUnreadContactMessages :one
SELECT COUNT(*) FROM contact_messages WHERE is_read = 0;

-- name: MarkContactMessageRead :exec
UPDATE contact_messages SET is_read = 1 WHERE id = ?;

-- =============================================================================
-- User Data Export
-- =============================================================================

-- name: ListUserChatMessages :many
SELECT gm.message, gm.created_at, g.name AS game_name
FROM game_messages gm
JOIN games g ON g.id = gm.game_id
WHERE gm.user_id = ?
ORDER BY gm.created_at DESC;

-- name: ListUserParticipations :many
SELECT p.joined_at, p.cash_balance, p.portfolio_value,
    g.name AS game_name, g.status AS game_status
FROM participants p
JOIN games g ON g.id = p.game_id
WHERE p.user_id = ?
ORDER BY p.joined_at DESC;

-- =============================================================================
-- Avatars
-- =============================================================================

-- name: UpsertAvatar :exec
INSERT OR IGNORE INTO avatars (key, name, image_path, category, achievement_key, sort_order)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListAvatars :many
SELECT * FROM avatars ORDER BY sort_order, id;

-- name: GetAvatar :one
SELECT * FROM avatars WHERE id = ?;

-- name: UpdateUserAvatar :exec
UPDATE users SET avatar_id = ? WHERE id = ?;

-- name: ClearUserAvatar :exec
UPDATE users SET avatar_id = NULL WHERE id = ?;

-- name: GetUserAvatarPath :one
SELECT COALESCE(a.image_path, '') AS avatar_path
FROM users u
LEFT JOIN avatars a ON a.id = u.avatar_id
WHERE u.id = ?;

-- =============================================================================
-- Lobby Chat
-- =============================================================================

-- name: CreateLobbyMessage :one
INSERT INTO lobby_messages (user_id, message)
VALUES (?, ?)
RETURNING *;

-- name: ListLobbyMessages :many
SELECT lm.*, u.name AS user_name, u.picture_url AS user_picture,
    COALESCE(a.image_path, '') AS avatar_path
FROM lobby_messages lm
JOIN users u ON u.id = lm.user_id
LEFT JOIN avatars a ON a.id = u.avatar_id
ORDER BY lm.created_at DESC
LIMIT ? OFFSET ?;

-- name: CountLobbyMessages :one
SELECT COUNT(*) FROM lobby_messages;

-- =============================================================================
-- Referral Bonuses
-- =============================================================================

-- name: InsertReferralBonus :exec
INSERT OR IGNORE INTO referral_bonuses (invite_id, referrer_id, referred_id, game_id, bonus_amount)
VALUES (?, ?, ?, ?, ?);

-- name: ListReferralBonuses :many
SELECT rb.*, u.name AS referred_name
FROM referral_bonuses rb
JOIN users u ON u.id = rb.referred_id
WHERE rb.referrer_id = ? AND rb.game_id = ?
ORDER BY rb.created_at DESC;

-- name: CountReferralBonuses :one
SELECT COUNT(*) FROM referral_bonuses WHERE referrer_id = ? AND game_id = ?;

-- name: CountAllReferralBonuses :one
SELECT COUNT(*) FROM referral_bonuses WHERE referrer_id = ?;

-- name: TotalReferralBonus :one
SELECT COALESCE(SUM(bonus_amount), 0) FROM referral_bonuses WHERE referrer_id = ? AND game_id = ?;

-- =============================================================================
-- Ask a Question
-- =============================================================================

-- name: CreateQuestion :one
INSERT INTO questions (user_id, title, body, topic_id) VALUES (?, ?, ?, ?)
RETURNING id;

-- name: SetQuestionTopic :exec
UPDATE questions SET topic_id = ? WHERE id = ?;

-- name: CountRecentQuestions :one
SELECT COUNT(*) FROM questions
WHERE user_id = ? AND created_at > datetime('now', '-1 day');

-- Most votes first; the handler re-sorts for newest first. Show is 'all',
-- 'answered' or 'open'; topic 0 means every topic.
-- name: ListQuestions :many
SELECT q.id, q.user_id, q.title, q.body, q.answer, q.answered_at, q.created_at,
       u.name AS user_name,
       COALESCE(t.name, '') AS topic_name, COALESCE(t.slug, '') AS topic_slug,
       q.answer_video_id IS NOT NULL AS has_video,
       (SELECT COUNT(*) FROM question_votes v WHERE v.question_id = q.id) AS votes,
       EXISTS (SELECT 1 FROM question_votes v WHERE v.question_id = q.id AND v.user_id = sqlc.arg(viewer)) AS voted
FROM questions q
JOIN users u ON u.id = q.user_id
LEFT JOIN topics t ON t.id = q.topic_id
WHERE q.hidden = 0
  AND (CAST(sqlc.arg(show) AS TEXT) = 'all'
       OR (sqlc.arg(show) = 'answered' AND q.answer != '')
       OR (sqlc.arg(show) = 'open' AND q.answer = ''))
  AND (CAST(sqlc.arg(topic) AS INTEGER) = 0 OR q.topic_id = sqlc.arg(topic))
ORDER BY votes DESC, q.created_at DESC
LIMIT 100;

-- The live Q&A queue: open questions, most votes first.
-- name: ListLiveQueue :many
SELECT q.id, q.user_id, q.title, q.body, q.created_at,
       u.name AS user_name,
       (SELECT COUNT(*) FROM question_votes v WHERE v.question_id = q.id) AS votes,
       EXISTS (SELECT 1 FROM question_votes v WHERE v.question_id = q.id AND v.user_id = sqlc.arg(viewer)) AS voted
FROM questions q
JOIN users u ON u.id = q.user_id
WHERE q.hidden = 0 AND q.answer = ''
ORDER BY votes DESC, q.created_at
LIMIT 20;

-- Questions answered during a stream.
-- name: ListLiveAnswered :many
SELECT q.id, q.title, q.answer_seconds FROM questions q
WHERE q.live_event_id = ? AND q.hidden = 0
ORDER BY q.answer_seconds;

-- name: GetQuestion :one
SELECT q.id, q.user_id, q.title, q.body, q.answer, q.answered_at, q.created_at,
       u.name AS user_name,
       COALESCE(a.name, '') AS answered_by_name,
       q.topic_id, COALESCE(t.name, '') AS topic_name, COALESCE(t.slug, '') AS topic_slug,
       q.answer_video_id, q.answer_seconds, q.live_event_id,
       COALESCE(av.title, '') AS video_title, COALESCE(av.embed_url, '') AS video_embed_url,
       COALESCE(av.url, '') AS video_url, COALESCE(av.vertical, 0) AS video_vertical,
       COALESCE(av.thumb_url, '') AS video_thumb_url,
       (SELECT COUNT(*) FROM question_votes v WHERE v.question_id = q.id) AS votes,
       EXISTS (SELECT 1 FROM question_votes v WHERE v.question_id = q.id AND v.user_id = sqlc.arg(viewer)) AS voted
FROM questions q
JOIN users u ON u.id = q.user_id
LEFT JOIN users a ON a.id = q.answered_by
LEFT JOIN topics t ON t.id = q.topic_id
LEFT JOIN videos av ON av.id = q.answer_video_id
WHERE q.id = sqlc.arg(id) AND q.hidden = 0;

-- name: AddQuestionVote :exec
INSERT OR IGNORE INTO question_votes (question_id, user_id) VALUES (?, ?);

-- name: RemoveQuestionVote :exec
DELETE FROM question_votes WHERE question_id = ? AND user_id = ?;

-- name: AnswerQuestion :exec
UPDATE questions SET answer = ?, answered_by = ?,
    answered_at = COALESCE(answered_at, datetime('now'))
WHERE id = ?;

-- Points the answer at a video, from a given second.
-- name: SetQuestionVideo :exec
UPDATE questions SET answer_video_id = ?, answer_seconds = ? WHERE id = ?;

-- Marks a question answered during a live stream. Its followers are emailed
-- once the stream is over (answer_mailed = 0 until then).
-- name: AnswerOnAir :exec
UPDATE questions SET live_event_id = sqlc.arg(live_event_id), answer_seconds = sqlc.arg(answer_seconds),
    answer = CASE WHEN answer = '' THEN sqlc.arg(answer) ELSE answer END,
    answered_by = sqlc.arg(answered_by), answered_at = COALESCE(answered_at, datetime('now')),
    answer_mailed = 0
WHERE id = sqlc.arg(id);

-- On-air answers whose stream has ended. A YouTube stream waits for its
-- recording, so the email can link to the moment.
-- name: ListOnAirAnswersToMail :many
SELECT q.id, q.user_id, q.title, q.answer, q.answer_seconds, e.video_id AS event_video_id,
       e.title AS event_title, e.platform AS event_platform,
       COALESCE(a.name, '') AS answered_by_name
FROM questions q
JOIN live_events e ON e.id = q.live_event_id
LEFT JOIN users a ON a.id = q.answered_by
WHERE q.answer_mailed = 0 AND q.hidden = 0 AND e.status = 'ended'
  AND (e.source != 'youtube' OR e.video_id IS NOT NULL);

-- name: MarkAnswerMailed :exec
UPDATE questions SET answer_mailed = 1 WHERE id = ?;

-- name: ListTopicQuestions :many
SELECT q.id, q.title, q.answer != '' AS answered,
       (SELECT COUNT(*) FROM question_votes v WHERE v.question_id = q.id) AS votes
FROM questions q
WHERE q.topic_id = ? AND q.hidden = 0
ORDER BY answered DESC, votes DESC
LIMIT 10;

-- name: HideQuestion :exec
UPDATE questions SET hidden = 1 WHERE id = ?;

-- name: DeleteOwnQuestion :execrows
DELETE FROM questions WHERE id = ? AND user_id = ? AND answer = '';

-- People to tell when a question is answered: the asker and everyone who
-- voted for it.
-- name: ListQuestionFollowers :many
SELECT user_id FROM questions WHERE id = sqlc.arg(id)
UNION
SELECT user_id FROM question_votes WHERE question_id = sqlc.arg(id);

-- =============================================================================
-- Courses
-- =============================================================================

-- name: ListPublishedCourses :many
SELECT c.id, c.slug, c.title, c.summary,
       (SELECT COUNT(*) FROM lessons l WHERE l.course_id = c.id) AS lessons,
       (SELECT COALESCE(SUM(l.minutes), 0) FROM lessons l WHERE l.course_id = c.id) AS minutes,
       (SELECT COUNT(*) FROM lessons l JOIN lesson_progress p ON p.lesson_id = l.id
        WHERE l.course_id = c.id AND p.user_id = sqlc.arg(viewer)) AS completed
FROM courses c
WHERE c.published = 1
ORDER BY c.sort_order, c.id;

-- name: ListAllCourses :many
SELECT c.*, (SELECT COUNT(*) FROM lessons l WHERE l.course_id = c.id) AS lessons
FROM courses c
ORDER BY c.sort_order, c.id;

-- name: GetCourse :one
SELECT * FROM courses WHERE id = ?;

-- name: GetCourseBySlug :one
SELECT * FROM courses WHERE slug = ?;

-- name: CreateCourse :one
INSERT INTO courses (slug, title, summary) VALUES (?, ?, ?)
RETURNING id;

-- name: UpdateCourse :exec
UPDATE courses SET title = ?, summary = ?, sort_order = ? WHERE id = ?;

-- name: SetCoursePublished :exec
UPDATE courses SET published = sqlc.arg(published),
    published_at = CASE WHEN sqlc.arg(published) = 1 THEN COALESCE(published_at, datetime('now')) ELSE published_at END
WHERE id = sqlc.arg(id);

-- name: DeleteCourse :exec
DELETE FROM courses WHERE id = ?;

-- name: ListLessons :many
SELECT l.*,
       EXISTS (SELECT 1 FROM lesson_progress p WHERE p.lesson_id = l.id AND p.user_id = sqlc.arg(viewer)) AS completed
FROM lessons l
WHERE l.course_id = sqlc.arg(course_id)
ORDER BY l.sort_order, l.id;

-- name: GetLesson :one
SELECT * FROM lessons WHERE id = ?;

-- name: CreateLesson :exec
INSERT INTO lessons (course_id, title, description, video_url, embed_url, minutes, sort_order)
VALUES (sqlc.arg(course_id), sqlc.arg(title), sqlc.arg(description), sqlc.arg(video_url), sqlc.arg(embed_url), sqlc.arg(minutes),
        (SELECT COALESCE(MAX(l.sort_order), 0) + 1 FROM lessons l WHERE l.course_id = sqlc.arg(course_id)));

-- name: UpdateLesson :exec
UPDATE lessons SET title = ?, description = ?, video_url = ?, embed_url = ?, minutes = ?, sort_order = ?
WHERE id = ?;

-- name: DeleteLesson :exec
DELETE FROM lessons WHERE id = ?;

-- name: CompleteLesson :exec
INSERT OR IGNORE INTO lesson_progress (user_id, lesson_id) VALUES (?, ?);

-- name: UncompleteLesson :exec
DELETE FROM lesson_progress WHERE user_id = ? AND lesson_id = ?;

-- name: ListActiveUserIDs :many
SELECT id FROM users WHERE deleted_at IS NULL ORDER BY id;

-- =============================================================================
-- Learn: videos, topics and live streams
-- =============================================================================

-- name: GetMediaState :one
SELECT value FROM media_state WHERE key = ?;

-- name: SetMediaState :exec
INSERT INTO media_state (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;

-- A changed title or description sends the video back to Claude.
-- name: UpsertVideo :one
INSERT INTO videos (platform, external_id, url, embed_url, title, description, thumb_url,
                    published_at, duration_seconds, vertical, live_status, was_live, view_count)
VALUES (sqlc.arg(platform), sqlc.arg(external_id), sqlc.arg(url), sqlc.arg(embed_url), sqlc.arg(title),
        sqlc.arg(description), sqlc.arg(thumb_url), sqlc.arg(published_at), sqlc.arg(duration_seconds),
        sqlc.arg(vertical), sqlc.arg(live_status), sqlc.arg(was_live), sqlc.arg(view_count))
ON CONFLICT(platform, external_id) DO UPDATE SET
    url = excluded.url,
    embed_url = excluded.embed_url,
    title = excluded.title,
    description = excluded.description,
    thumb_url = excluded.thumb_url,
    published_at = excluded.published_at,
    duration_seconds = excluded.duration_seconds,
    vertical = excluded.vertical,
    live_status = excluded.live_status,
    was_live = excluded.was_live,
    view_count = excluded.view_count,
    classified_at = CASE WHEN videos.title != excluded.title OR videos.description != excluded.description
                         THEN NULL ELSE videos.classified_at END
RETURNING id;

-- name: GetVideo :one
SELECT * FROM videos WHERE id = ?;

-- name: GetVideoByExternalID :one
SELECT * FROM videos WHERE platform = ? AND external_id = ?;

-- Videos for the Watch page rows. Streams still upcoming or live are on the
-- Live tab instead.
-- name: ListLatestVideos :many
SELECT * FROM videos
WHERE hidden = 0 AND live_status = '' AND vertical = sqlc.arg(vertical)
ORDER BY published_at DESC
LIMIT sqlc.arg(lim);

-- name: ListFeaturedVideos :many
SELECT * FROM videos
WHERE hidden = 0 AND live_status = '' AND featured = 1
ORDER BY published_at DESC
LIMIT 6;

-- name: ListTopicVideos :many
SELECT v.* FROM videos v
JOIN video_topics vt ON vt.video_id = v.id
WHERE vt.topic_id = sqlc.arg(topic_id) AND v.hidden = 0 AND v.live_status = ''
ORDER BY v.published_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: CountTopicVideos :one
SELECT COUNT(*) FROM videos v
JOIN video_topics vt ON vt.video_id = v.id
WHERE vt.topic_id = ? AND v.hidden = 0 AND v.live_status = '';

-- Newest first; the handler moves title matches to the top.
-- name: SearchVideos :many
SELECT * FROM videos
WHERE hidden = 0 AND live_status = ''
  AND (title LIKE '%' || CAST(sqlc.arg(q) AS TEXT) || '%' OR description LIKE '%' || CAST(sqlc.arg(q) AS TEXT) || '%')
ORDER BY published_at DESC
LIMIT 60;

-- Videos sharing the most topics with this one, newest first.
-- name: ListRelatedVideos :many
SELECT v.*, COUNT(*) AS shared FROM videos v
JOIN video_topics vt ON vt.video_id = v.id
WHERE vt.topic_id IN (SELECT x.topic_id FROM video_topics x WHERE x.video_id = sqlc.arg(id))
  AND v.id != sqlc.arg(id) AND v.hidden = 0 AND v.live_status = ''
GROUP BY v.id
ORDER BY shared DESC, v.published_at DESC
LIMIT 8;

-- name: ListTopics :many
SELECT t.*,
       (SELECT COUNT(*) FROM video_topics vt JOIN videos v ON v.id = vt.video_id
        WHERE vt.topic_id = t.id AND v.hidden = 0 AND v.live_status = '') AS videos
FROM topics t
ORDER BY t.sort_order, t.name;

-- name: GetTopicBySlug :one
SELECT * FROM topics WHERE slug = ?;

-- name: GetTopic :one
SELECT * FROM topics WHERE id = ?;

-- name: CreateTopic :exec
INSERT INTO topics (slug, name, description, sort_order)
VALUES (?, ?, ?, (SELECT COALESCE(MAX(t.sort_order), 0) + 1 FROM topics t));

-- name: UpdateTopic :exec
UPDATE topics SET name = ?, description = ?, sort_order = ? WHERE id = ?;

-- name: DeleteTopic :exec
DELETE FROM topics WHERE id = ?;

-- name: ListVideoTopics :many
SELECT t.id, t.slug, t.name, vt.source FROM topics t
JOIN video_topics vt ON vt.topic_id = t.id
WHERE vt.video_id = ?
ORDER BY t.sort_order;

-- name: AddVideoTopic :exec
INSERT INTO video_topics (video_id, topic_id, source) VALUES (?, ?, ?)
ON CONFLICT(video_id, topic_id) DO NOTHING;

-- name: RemoveVideoTopic :exec
DELETE FROM video_topics WHERE video_id = ? AND topic_id = ?;

-- Clears Claude's picks before it picks again. An admin's stay.
-- name: ClearClaudeVideoTopics :exec
DELETE FROM video_topics WHERE video_id = ? AND source = 'claude';

-- name: ListUnclassifiedVideos :many
SELECT * FROM videos
WHERE classified_at IS NULL AND hidden = 0 AND live_status = ''
ORDER BY published_at DESC
LIMIT ?;

-- name: SetVideoClassified :exec
UPDATE videos SET classified_at = datetime('now') WHERE id = ?;

-- name: ResetVideoClassified :exec
UPDATE videos SET classified_at = NULL WHERE id = ?;

-- name: ClearVideoStocks :exec
DELETE FROM video_stocks WHERE video_id = ?;

-- name: AddVideoStockBySymbol :exec
INSERT OR IGNORE INTO video_stocks (video_id, stock_id)
SELECT sqlc.arg(video_id), s.id FROM stocks s WHERE s.symbol = sqlc.arg(symbol);

-- name: ListStockVideos :many
SELECT v.* FROM videos v
JOIN video_stocks vs ON vs.video_id = v.id
WHERE vs.stock_id = ? AND v.hidden = 0 AND v.live_status = ''
ORDER BY v.published_at DESC
LIMIT 6;

-- The admin list. Platform '' means every platform.
-- name: ListVideosAdmin :many
SELECT v.id, v.platform, v.title, v.url, v.thumb_url, v.published_at, v.hidden, v.featured,
       v.vertical, v.live_status, v.classified_at,
       CAST(COALESCE((SELECT GROUP_CONCAT(t.name, ', ') FROM video_topics vt JOIN topics t ON t.id = vt.topic_id
                      WHERE vt.video_id = v.id), '') AS TEXT) AS topic_names
FROM videos v
WHERE (CAST(sqlc.arg(platform) AS TEXT) = '' OR v.platform = sqlc.arg(platform))
  AND (CAST(sqlc.arg(q) AS TEXT) = '' OR v.title LIKE '%' || sqlc.arg(q) || '%')
ORDER BY v.published_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: CountVideosByPlatform :many
SELECT platform, COUNT(*) AS videos, CAST(SUM(classified_at IS NULL) AS INTEGER) AS unclassified
FROM videos GROUP BY platform ORDER BY platform;

-- name: SetVideoHidden :exec
UPDATE videos SET hidden = ? WHERE id = ?;

-- name: SetVideoFeatured :exec
UPDATE videos SET featured = ? WHERE id = ?;

-- name: DeleteVideo :exec
DELETE FROM videos WHERE id = ?;

-- name: DeleteVideoByExternalID :exec
DELETE FROM videos WHERE platform = ? AND external_id = ?;

-- name: ListPlatformVideoIDs :many
SELECT id, external_id FROM videos WHERE platform = ?;

-- name: UpsertVideoThumb :exec
INSERT INTO video_thumbs (video_id, content_type, data) VALUES (?, ?, ?)
ON CONFLICT(video_id) DO UPDATE SET content_type = excluded.content_type, data = excluded.data;

-- name: GetVideoThumb :one
SELECT content_type, data FROM video_thumbs WHERE video_id = ?;

-- YouTube streams still upcoming or live, for the sync to re-check.
-- name: ListLiveYouTubeVideoIDs :many
SELECT external_id FROM videos WHERE platform = 'youtube' AND live_status != '';

-- Videos to pick from when answering a question with one.
-- name: PickVideos :many
SELECT id, platform, title, published_at FROM videos
WHERE hidden = 0 AND live_status = '' AND (CAST(sqlc.arg(q) AS TEXT) = '' OR title LIKE '%' || sqlc.arg(q) || '%')
ORDER BY published_at DESC
LIMIT 30;

-- name: UpsertYouTubeLiveEvent :exec
INSERT INTO live_events (platform, url, embed_url, video_id, title, description, scheduled_at,
                         started_at, ended_at, status, source)
VALUES ('youtube', sqlc.arg(url), sqlc.arg(embed_url), sqlc.arg(video_id), sqlc.arg(title), sqlc.arg(description),
        sqlc.arg(scheduled_at), sqlc.arg(started_at), sqlc.arg(ended_at), sqlc.arg(status), 'youtube')
ON CONFLICT(video_id) DO UPDATE SET
    url = excluded.url,
    embed_url = excluded.embed_url,
    title = excluded.title,
    description = excluded.description,
    scheduled_at = excluded.scheduled_at,
    started_at = COALESCE(excluded.started_at, live_events.started_at),
    ended_at = COALESCE(excluded.ended_at, live_events.ended_at),
    status = CASE WHEN live_events.status = 'cancelled' THEN 'cancelled' ELSE excluded.status END;

-- name: CreateLiveEvent :one
INSERT INTO live_events (platform, url, embed_url, title, description, scheduled_at, status, source)
VALUES (?, ?, ?, ?, ?, ?, ?, 'admin')
RETURNING id;

-- name: UpdateLiveEvent :exec
UPDATE live_events SET platform = ?, url = ?, embed_url = ?, title = ?, description = ?, scheduled_at = ?
WHERE id = ?;

-- name: GoLive :exec
UPDATE live_events SET status = 'live', started_at = COALESCE(started_at, datetime('now')) WHERE id = ?;

-- name: EndLive :exec
UPDATE live_events SET status = 'ended', ended_at = COALESCE(ended_at, datetime('now')) WHERE id = ?;

-- name: CancelLive :exec
UPDATE live_events SET status = 'cancelled' WHERE id = ?;

-- name: DeleteLiveEvent :exec
DELETE FROM live_events WHERE id = ?;

-- name: GetLiveEvent :one
SELECT * FROM live_events WHERE id = ?;

-- name: ListLiveNow :many
SELECT * FROM live_events WHERE status = 'live' ORDER BY started_at DESC;

-- Upcoming streams. One an hour overdue without starting has probably been
-- abandoned, so it drops off.
-- name: ListUpcomingLive :many
SELECT * FROM live_events
WHERE status = 'upcoming' AND (scheduled_at IS NULL OR scheduled_at > datetime('now', '-1 hour'))
ORDER BY scheduled_at
LIMIT 20;

-- name: ListPastLive :many
SELECT * FROM live_events WHERE status = 'ended'
ORDER BY COALESCE(started_at, scheduled_at) DESC
LIMIT ?;

-- name: ListAllLiveEvents :many
SELECT * FROM live_events ORDER BY COALESCE(scheduled_at, created_at) DESC LIMIT 100;

-- name: ListLiveToAnnounce :many
SELECT * FROM live_events WHERE status = 'upcoming' AND announced = 0;

-- name: MarkLiveAnnounced :exec
UPDATE live_events SET announced = 1 WHERE id = ?;

-- name: ListLiveToNotifyStart :many
SELECT * FROM live_events WHERE status = 'live' AND start_notified = 0;

-- name: MarkLiveStartNotified :exec
UPDATE live_events SET start_notified = 1, announced = 1 WHERE id = ?;

-- name: AddLiveReminder :exec
INSERT OR IGNORE INTO live_reminders (event_id, user_id) VALUES (?, ?);

-- name: RemoveLiveReminder :exec
DELETE FROM live_reminders WHERE event_id = ? AND user_id = ?;

-- name: ListUserLiveReminders :many
SELECT event_id FROM live_reminders WHERE user_id = ?;

-- name: ListLiveReminderUserIDs :many
SELECT user_id FROM live_reminders WHERE event_id = ?;

-- name: GetOAuthToken :one
SELECT * FROM oauth_tokens WHERE provider = ?;

-- name: SaveOAuthToken :exec
INSERT INTO oauth_tokens (provider, account, access_token, refresh_token, access_expires_at, refresh_expires_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(provider) DO UPDATE SET
    account = excluded.account,
    access_token = excluded.access_token,
    refresh_token = excluded.refresh_token,
    access_expires_at = excluded.access_expires_at,
    refresh_expires_at = excluded.refresh_expires_at,
    updated_at = datetime('now');

-- name: DeleteOAuthToken :exec
DELETE FROM oauth_tokens WHERE provider = ?;
