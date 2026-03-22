-- =============================================================================
-- Users
-- =============================================================================

-- name: UpsertUser :one
INSERT INTO users (google_id, email, name, picture_url, last_login)
VALUES (?, ?, ?, ?, datetime('now'))
ON CONFLICT(google_id) DO UPDATE SET
    email      = excluded.email,
    name       = excluded.name,
    picture_url = excluded.picture_url,
    last_login = datetime('now')
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: SetAdmin :exec
UPDATE users SET is_admin = ? WHERE id = ?;

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at DESC;

-- name: SoftDeleteUser :exec
UPDATE users SET
    name = 'Deleted User',
    email = 'deleted-' || CAST(id AS TEXT) || '@deleted.local',
    google_id = 'deleted-' || CAST(id AS TEXT),
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
-- Games
-- =============================================================================

-- name: CreateGame :one
INSERT INTO games (created_by, name, description, markets, starting_balance, base_currency, max_participants, start_date, end_date, allow_short, trade_fee, recurring_interval, referral_bonus_pct, is_public, portfolio_visibility, credit_interest_rate, leverage_interest_rate, min_stock_price, max_stock_price, margin_trading, limit_orders, stop_loss, fractional_shares)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetGame :one
SELECT * FROM games WHERE id = ?;

-- name: ListActiveGames :many
SELECT * FROM games WHERE status IN ('pending', 'active') ORDER BY start_date ASC;

-- name: ListUserGames :many
SELECT g.* FROM games g
JOIN participants p ON p.game_id = g.id
WHERE p.user_id = ?
ORDER BY g.start_date DESC;

-- name: ListGamesByCreator :many
SELECT * FROM games WHERE created_by = ? ORDER BY created_at DESC;

-- name: UpdateGameStatus :exec
UPDATE games SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: UpdateGameSettings :exec
UPDATE games SET
    name = ?, description = ?, markets = ?, starting_balance = ?,
    base_currency = ?, max_participants = ?, start_date = ?, end_date = ?,
    allow_short = ?, trade_fee = ?, is_public = ?, portfolio_visibility = ?,
    credit_interest_rate = ?, leverage_interest_rate = ?,
    min_stock_price = ?, max_stock_price = ?,
    margin_trading = ?, limit_orders = ?, stop_loss = ?, fractional_shares = ?,
    updated_at = datetime('now')
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
ORDER BY symbol ASC
LIMIT 20;

-- name: ListStocksByMarket :many
SELECT * FROM stocks
WHERE is_active = 1 AND (market = ? OR ? = 'both')
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
INSERT INTO holdings (participant_id, stock_id, quantity, avg_cost, current_value, updated_at)
VALUES (?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT(participant_id, stock_id) DO UPDATE SET
    quantity = excluded.quantity,
    avg_cost = excluded.avg_cost,
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
-- Limit Orders (Feature 1)
-- =============================================================================

-- name: CreateLimitOrder :one
INSERT INTO limit_orders (participant_id, stock_id, type, quantity, limit_price, expires_at)
VALUES (?, ?, ?, ?, ?, ?)
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

-- name: ExpireOldLimitOrders :exec
UPDATE limit_orders SET status = 'expired'
WHERE status = 'open' AND expires_at IS NOT NULL AND expires_at < sqlc.arg(today);

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
INSERT INTO game_invites (game_id, code, created_by, max_uses, expires_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetInviteByCode :one
SELECT * FROM game_invites WHERE code = ?;

-- name: IncrementInviteUse :exec
UPDATE game_invites SET use_count = use_count + 1 WHERE id = ?;

-- name: ListGameInvites :many
SELECT * FROM game_invites WHERE game_id = ? ORDER BY created_at DESC;

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
INSERT OR IGNORE INTO achievements (key, name, description, icon)
VALUES (?, ?, ?, ?);

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
-- Game Templates (Feature 16)
-- =============================================================================

-- name: CreateGameTemplate :one
INSERT INTO game_templates (name, description, markets, starting_balance, base_currency, duration_days, allow_short, trade_fee, is_builtin, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListGameTemplates :many
SELECT * FROM game_templates ORDER BY is_builtin DESC, name ASC;

-- name: GetGameTemplate :one
SELECT * FROM game_templates WHERE id = ?;

-- name: DeleteGameTemplate :exec
DELETE FROM game_templates WHERE id = ? AND is_builtin = 0;

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

-- name: CountFinishedGames :one
SELECT COUNT(*) FROM games WHERE status IN ('finished', 'cancelled');

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
-- Recurring Games (Feature 17)
-- =============================================================================

-- name: ListFinishedRecurringGames :many
SELECT * FROM games
WHERE status = 'finished' AND recurring_interval IS NOT NULL
ORDER BY end_date DESC;

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

-- name: TotalReferralBonus :one
SELECT COALESCE(SUM(bonus_amount), 0) FROM referral_bonuses WHERE referrer_id = ? AND game_id = ?;
