-- Fantasy Stock Market Game
-- All money values stored as INTEGER cents. Never use floats for money.
-- Exchange rates stored as INTEGER (rate * 1,000,000).
-- Timestamps use TEXT with datetime('now') for SQLite compatibility.

--------------------------------------------------------------------------------
-- Avatars
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS avatars (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    key             TEXT    NOT NULL UNIQUE,
    name            TEXT    NOT NULL,
    image_path      TEXT    NOT NULL,               -- e.g. '/static/avatars/bear.svg'
    category        TEXT    NOT NULL DEFAULT 'free', -- 'free' | 'achievement'
    achievement_key TEXT,                            -- NULL for free, matches achievements.key for locked
    sort_order      INTEGER NOT NULL DEFAULT 0
);

--------------------------------------------------------------------------------
-- Users (Google OAuth)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    google_id   TEXT    NOT NULL UNIQUE,
    email       TEXT    NOT NULL UNIQUE,
    name        TEXT    NOT NULL,
    picture_url TEXT,
    is_admin    INTEGER NOT NULL DEFAULT 0,
    avatar_id   INTEGER REFERENCES avatars(id),
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    last_login  TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at  TEXT                                        -- NULL = active, set = soft-deleted
);

--------------------------------------------------------------------------------
-- Sessions
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_sessions_user    ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

--------------------------------------------------------------------------------
-- Games
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS games (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    created_by       INTEGER NOT NULL REFERENCES users(id),
    name             TEXT    NOT NULL,
    description      TEXT    NOT NULL DEFAULT '',
    status           TEXT    NOT NULL DEFAULT 'pending',    -- pending | active | finished | cancelled
    markets          TEXT    NOT NULL DEFAULT 'both',       -- asx | sp500 | both
    starting_balance INTEGER NOT NULL DEFAULT 100000000,    -- $1,000,000.00 in cents
    base_currency    TEXT    NOT NULL DEFAULT 'AUD',        -- AUD | USD
    max_participants INTEGER,                               -- NULL = unlimited
    start_date       TEXT    NOT NULL,                      -- YYYY-MM-DD
    end_date         TEXT    NOT NULL,                      -- YYYY-MM-DD
    allow_short          INTEGER NOT NULL DEFAULT 0,
    trade_fee            INTEGER NOT NULL DEFAULT 0,            -- per-trade fee in cents
    referral_bonus_pct   INTEGER NOT NULL DEFAULT 1,            -- % of starting_balance as referral reward
    recurring_interval   TEXT,                                   -- 'weekly' | 'monthly' | NULL
    parent_game_id       INTEGER REFERENCES games(id),
    template_id          INTEGER REFERENCES game_templates(id),
    is_public            INTEGER NOT NULL DEFAULT 1,            -- 1 = public, 0 = private (invite only)
    portfolio_visibility TEXT    NOT NULL DEFAULT 'public',      -- 'public' | 'private' | 'user_selectable'
    credit_interest_rate INTEGER NOT NULL DEFAULT 100,           -- basis points (100 = 1.00%)
    leverage_interest_rate INTEGER NOT NULL DEFAULT 500,         -- basis points (500 = 5.00%)
    min_stock_price      INTEGER,                                -- minimum stock price in cents, NULL = no limit
    max_stock_price      INTEGER,                                -- maximum stock price in cents, NULL = no limit
    margin_trading       INTEGER NOT NULL DEFAULT 0,             -- 1 = enabled
    limit_orders         INTEGER NOT NULL DEFAULT 0,             -- 1 = enabled
    stop_loss            INTEGER NOT NULL DEFAULT 0,             -- 1 = enabled
    fractional_shares    INTEGER NOT NULL DEFAULT 0,             -- 1 = enabled
    created_at           TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at           TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_games_status ON games(status);
CREATE INDEX IF NOT EXISTS idx_games_created_by ON games(created_by);

--------------------------------------------------------------------------------
-- Game Participants
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS participants (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id         INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cash_balance    INTEGER NOT NULL,
    portfolio_value INTEGER NOT NULL DEFAULT 0,
    is_public       INTEGER NOT NULL DEFAULT 1,
    joined_at       TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(game_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_participants_game  ON participants(game_id);
CREATE INDEX IF NOT EXISTS idx_participants_user  ON participants(user_id);
CREATE INDEX IF NOT EXISTS idx_participants_value ON participants(game_id, portfolio_value DESC);

--------------------------------------------------------------------------------
-- Stocks
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS stocks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol     TEXT    NOT NULL,
    name       TEXT    NOT NULL,
    market     TEXT    NOT NULL,                -- 'asx' or 'sp500'
    sector     TEXT    NOT NULL DEFAULT '',
    currency   TEXT    NOT NULL DEFAULT 'AUD',  -- 'AUD' or 'USD'
    is_active  INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(symbol, market)
);
CREATE INDEX IF NOT EXISTS idx_stocks_market ON stocks(market, is_active);
CREATE INDEX IF NOT EXISTS idx_stocks_symbol ON stocks(symbol);

--------------------------------------------------------------------------------
-- Daily Stock Prices (cents in native currency)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS stock_prices (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    stock_id   INTEGER NOT NULL REFERENCES stocks(id) ON DELETE CASCADE,
    date       TEXT    NOT NULL,                -- YYYY-MM-DD
    open       INTEGER NOT NULL,
    high       INTEGER NOT NULL,
    low        INTEGER NOT NULL,
    close      INTEGER NOT NULL,
    volume     INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(stock_id, date)
);
CREATE INDEX IF NOT EXISTS idx_prices_stock_date ON stock_prices(stock_id, date DESC);
CREATE INDEX IF NOT EXISTS idx_prices_date       ON stock_prices(date);

--------------------------------------------------------------------------------
-- Exchange Rates (AUD/USD)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS exchange_rates (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    date         TEXT    NOT NULL UNIQUE,       -- YYYY-MM-DD
    rate_aud_usd INTEGER NOT NULL,              -- rate * 1,000,000 (e.g. 0.652 = 652000)
    created_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);

--------------------------------------------------------------------------------
-- Holdings
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS holdings (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    participant_id INTEGER NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    stock_id       INTEGER NOT NULL REFERENCES stocks(id),
    quantity       INTEGER NOT NULL,
    avg_cost       INTEGER NOT NULL,            -- per-share in stock's native currency cents
    current_value  INTEGER NOT NULL DEFAULT 0,  -- quantity * latest close in native currency cents
    updated_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(participant_id, stock_id)
);
CREATE INDEX IF NOT EXISTS idx_holdings_participant ON holdings(participant_id);

--------------------------------------------------------------------------------
-- Transactions
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS transactions (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    participant_id     INTEGER NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    stock_id           INTEGER NOT NULL REFERENCES stocks(id),
    type               TEXT    NOT NULL,         -- 'buy' or 'sell'
    quantity           INTEGER NOT NULL,
    price              INTEGER NOT NULL,         -- per-share in native currency cents
    total              INTEGER NOT NULL,         -- quantity * price in native currency cents
    converted_total    INTEGER NOT NULL,         -- total in game's base currency cents
    exchange_rate_used INTEGER NOT NULL DEFAULT 1000000, -- rate * 1,000,000 at time of trade
    fee                INTEGER NOT NULL DEFAULT 0,
    created_at         TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_transactions_participant ON transactions(participant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_transactions_stock       ON transactions(stock_id);

--------------------------------------------------------------------------------
-- Portfolio Snapshots (daily, in game's base currency)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS portfolio_snapshots (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    participant_id INTEGER NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    date           TEXT    NOT NULL,
    cash_balance   INTEGER NOT NULL,
    holdings_value INTEGER NOT NULL,
    total_value    INTEGER NOT NULL,
    UNIQUE(participant_id, date)
);
CREATE INDEX IF NOT EXISTS idx_snapshots_participant ON portfolio_snapshots(participant_id, date DESC);

--------------------------------------------------------------------------------
-- Price Fetch Log
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS price_fetch_log (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    market         TEXT    NOT NULL,             -- 'asx', 'sp500', or 'forex'
    status         TEXT    NOT NULL,             -- 'success' | 'partial' | 'error'
    stocks_updated INTEGER NOT NULL DEFAULT 0,
    error_msg      TEXT,
    duration_ms    INTEGER,
    created_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

--------------------------------------------------------------------------------
-- Limit Orders (Feature 1)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS limit_orders (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    participant_id  INTEGER NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    stock_id        INTEGER NOT NULL REFERENCES stocks(id),
    type            TEXT    NOT NULL,              -- 'buy' or 'sell'
    quantity        INTEGER NOT NULL,
    limit_price     INTEGER NOT NULL,              -- cents in stock's native currency
    status          TEXT    NOT NULL DEFAULT 'open', -- open | filled | cancelled | expired
    created_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    expires_at      TEXT,                           -- optional expiry date YYYY-MM-DD
    filled_at       TEXT
);
CREATE INDEX IF NOT EXISTS idx_limit_orders_participant ON limit_orders(participant_id, status);
CREATE INDEX IF NOT EXISTS idx_limit_orders_stock ON limit_orders(stock_id, status);

--------------------------------------------------------------------------------
-- Watchlist (Feature 2)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS watchlist (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    participant_id  INTEGER NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    stock_id        INTEGER NOT NULL REFERENCES stocks(id),
    created_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(participant_id, stock_id)
);
CREATE INDEX IF NOT EXISTS idx_watchlist_participant ON watchlist(participant_id);

--------------------------------------------------------------------------------
-- Invite Links (Feature 6)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS game_invites (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id    INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    code       TEXT    NOT NULL UNIQUE,
    created_by INTEGER NOT NULL REFERENCES users(id),
    max_uses   INTEGER,                            -- NULL = unlimited
    use_count  INTEGER NOT NULL DEFAULT 0,
    expires_at TEXT,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_invites_code ON game_invites(code);

--------------------------------------------------------------------------------
-- Game Chat (Feature 7)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS game_messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id    INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users(id),
    message    TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_messages_game ON game_messages(game_id, created_at DESC);

--------------------------------------------------------------------------------
-- Achievements (Feature 8)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS achievements (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    key         TEXT    NOT NULL UNIQUE,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL,
    icon        TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS user_achievements (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    achievement_id INTEGER NOT NULL REFERENCES achievements(id),
    game_id        INTEGER REFERENCES games(id) ON DELETE SET NULL,
    earned_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id, achievement_id, game_id)
);
CREATE INDEX IF NOT EXISTS idx_user_achievements ON user_achievements(user_id);

--------------------------------------------------------------------------------
-- Activity Feed (Feature 10)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS activity_feed (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id        INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    user_id        INTEGER NOT NULL REFERENCES users(id),
    action         TEXT    NOT NULL,                -- 'buy' | 'sell' | 'join' | 'achievement'
    detail         TEXT    NOT NULL DEFAULT '',     -- e.g. "bought 100 shares of BHP"
    is_public      INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_activity_game ON activity_feed(game_id, created_at DESC);

--------------------------------------------------------------------------------
-- Game Templates (Feature 16)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS game_templates (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT    NOT NULL,
    description      TEXT    NOT NULL DEFAULT '',
    markets          TEXT    NOT NULL DEFAULT 'both',
    starting_balance INTEGER NOT NULL DEFAULT 100000000,
    base_currency    TEXT    NOT NULL DEFAULT 'AUD',
    duration_days    INTEGER NOT NULL DEFAULT 30,
    allow_short      INTEGER NOT NULL DEFAULT 0,
    trade_fee        INTEGER NOT NULL DEFAULT 0,
    is_builtin       INTEGER NOT NULL DEFAULT 0,
    created_by       INTEGER REFERENCES users(id),
    created_at       TEXT    NOT NULL DEFAULT (datetime('now'))
);

--------------------------------------------------------------------------------
-- Starting Portfolio (Feature 18)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS game_starting_stocks (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id  INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    stock_id INTEGER NOT NULL REFERENCES stocks(id),
    quantity INTEGER NOT NULL,
    UNIQUE(game_id, stock_id)
);

--------------------------------------------------------------------------------
-- Notifications (Feature 19)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notifications (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    game_id    INTEGER REFERENCES games(id) ON DELETE CASCADE,
    type       TEXT    NOT NULL,                    -- 'rank_change' | 'game_start' | 'game_end' | 'order_filled' | 'achievement'
    title      TEXT    NOT NULL,
    message    TEXT    NOT NULL,
    is_read    INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, is_read, created_at DESC);

--------------------------------------------------------------------------------
-- Contact Messages
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS contact_messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER REFERENCES users(id),
    name       TEXT    NOT NULL,
    email      TEXT    NOT NULL,
    message    TEXT    NOT NULL,
    is_read    INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_contact_messages_read ON contact_messages(is_read, created_at DESC);

--------------------------------------------------------------------------------
-- Lobby Chat
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS lobby_messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id),
    message    TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_lobby_messages_created ON lobby_messages(created_at DESC);

--------------------------------------------------------------------------------
-- Referral Bonuses
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS referral_bonuses (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    invite_id      INTEGER NOT NULL REFERENCES game_invites(id),
    referrer_id    INTEGER NOT NULL REFERENCES users(id),
    referred_id    INTEGER NOT NULL REFERENCES users(id),
    game_id        INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    bonus_amount   INTEGER NOT NULL,                -- cents added to referrer
    created_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(invite_id, referred_id)
);
CREATE INDEX IF NOT EXISTS idx_referral_bonuses_referrer ON referral_bonuses(referrer_id);
CREATE INDEX IF NOT EXISTS idx_referral_bonuses_game ON referral_bonuses(game_id);
