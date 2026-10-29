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
-- Users (email sign-in). rebuildUsersTable in internal/handler/db.go copies
-- this definition for databases created when sign-in used Google.
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    email       TEXT    NOT NULL UNIQUE,                    -- lower case
    name        TEXT    NOT NULL,
    picture_url TEXT,
    is_admin    INTEGER NOT NULL DEFAULT 0,
    avatar_id   INTEGER REFERENCES avatars(id),
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    last_login  TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at  TEXT,                                       -- NULL = active, set = soft-deleted
    last_game_id INTEGER REFERENCES games(id) ON DELETE SET NULL, -- the game the game bar last showed
    email_token TEXT    NOT NULL UNIQUE                     -- random; identifies the user in unsubscribe links
);

--------------------------------------------------------------------------------
-- Sign-in requests. Each emailed link and code works once, for 15 minutes.
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS login_requests (
    id         TEXT    PRIMARY KEY,                 -- random; the login_req cookie holds it for the code form
    email      TEXT    NOT NULL,
    token_hash TEXT    NOT NULL UNIQUE,             -- SHA-256 of the link token
    code_hash  TEXT    NOT NULL,                    -- SHA-256 of id + ":" + the 6-digit code
    attempts   INTEGER NOT NULL DEFAULT 0,          -- wrong codes entered
    expires_at TEXT    NOT NULL,
    used_at    TEXT,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_login_requests_email ON login_requests(email, created_at);

--------------------------------------------------------------------------------
-- Email. Categories are listed in internal/mail/categories.go; a missing
-- email_prefs row means the category's default. docs/emails.md has the plan.
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS email_prefs (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category   TEXT    NOT NULL,
    enabled    INTEGER NOT NULL,
    updated_at TEXT    NOT NULL DEFAULT (datetime('now')), -- when they opted in or out (Spam Act consent record)
    PRIMARY KEY (user_id, category)
);

CREATE TABLE IF NOT EXISTS email_outbox (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
    to_email        TEXT    NOT NULL,
    category        TEXT    NOT NULL,
    template        TEXT    NOT NULL,               -- e.g. 'login'
    subject         TEXT    NOT NULL,
    text_body       TEXT    NOT NULL,
    html_body       TEXT    NOT NULL,
    reply_to        TEXT    NOT NULL DEFAULT '',
    unsubscribe_url TEXT    NOT NULL DEFAULT '',
    status          TEXT    NOT NULL DEFAULT 'pending', -- 'pending' | 'sent' | 'failed'
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT    NOT NULL DEFAULT '',
    send_after      TEXT    NOT NULL DEFAULT (datetime('now')),
    sent_at         TEXT,
    created_at      TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_email_outbox_pending ON email_outbox(status, send_after);

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
    limit_price     INTEGER NOT NULL,              -- cents in stock's native currency; 0 for market orders
    status          TEXT    NOT NULL DEFAULT 'open', -- open | filled | cancelled | expired
    created_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    expires_at      TEXT,                           -- optional expiry date YYYY-MM-DD
    filled_at       TEXT,
    kind            TEXT    NOT NULL DEFAULT 'limit' -- limit | market (placed while the market was closed; fills at the open)
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
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    emailed    INTEGER NOT NULL DEFAULT 0                -- 1 once internal/notify has handled it
);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, is_read, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_emailed ON notifications(emailed) WHERE emailed = 0;

-- Game emails already sent, so each goes once per game.
CREATE TABLE IF NOT EXISTS game_emails (
    game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    kind    TEXT    NOT NULL,                          -- 'started' | 'ending_3' | 'ending_1' | 'results'
    sent_at TEXT    NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (game_id, kind)
);

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

--------------------------------------------------------------------------------
-- Stock Profiles (from Twelve Data /profile + /statistics endpoints)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS stock_profiles (
    stock_id              INTEGER PRIMARY KEY REFERENCES stocks(id) ON DELETE CASCADE,
    -- Profile fields
    exchange              TEXT    NOT NULL DEFAULT '',
    industry              TEXT    NOT NULL DEFAULT '',
    description           TEXT    NOT NULL DEFAULT '',
    type                  TEXT    NOT NULL DEFAULT '',  -- e.g. 'Common Stock'
    ceo                   TEXT    NOT NULL DEFAULT '',
    employees             INTEGER NOT NULL DEFAULT 0,
    website               TEXT    NOT NULL DEFAULT '',
    address               TEXT    NOT NULL DEFAULT '',
    city                  TEXT    NOT NULL DEFAULT '',
    state                 TEXT    NOT NULL DEFAULT '',
    country               TEXT    NOT NULL DEFAULT '',
    phone                 TEXT    NOT NULL DEFAULT '',
    -- Valuation metrics (ratios * 100)
    market_cap            INTEGER NOT NULL DEFAULT 0,   -- raw dollars
    enterprise_value      INTEGER NOT NULL DEFAULT 0,   -- raw dollars
    trailing_pe           INTEGER NOT NULL DEFAULT 0,   -- * 100
    forward_pe            INTEGER NOT NULL DEFAULT 0,   -- * 100
    peg_ratio             INTEGER NOT NULL DEFAULT 0,   -- * 100
    price_to_book         INTEGER NOT NULL DEFAULT 0,   -- * 100
    price_to_sales        INTEGER NOT NULL DEFAULT 0,   -- * 100
    ev_to_revenue         INTEGER NOT NULL DEFAULT 0,   -- * 100
    ev_to_ebitda          INTEGER NOT NULL DEFAULT 0,   -- * 100
    -- Income statement
    revenue               INTEGER NOT NULL DEFAULT 0,   -- raw dollars
    revenue_per_share     INTEGER NOT NULL DEFAULT 0,   -- * 100 (cents)
    quarterly_rev_growth  INTEGER NOT NULL DEFAULT 0,   -- * 10000 (bps, e.g. 15.7% = 1570)
    gross_profit          INTEGER NOT NULL DEFAULT 0,   -- raw dollars
    ebitda                INTEGER NOT NULL DEFAULT 0,   -- raw dollars
    net_income            INTEGER NOT NULL DEFAULT 0,   -- raw dollars
    diluted_eps           INTEGER NOT NULL DEFAULT 0,   -- * 100 (cents)
    quarterly_earn_growth INTEGER NOT NULL DEFAULT 0,   -- * 10000 (bps)
    -- Balance sheet
    total_cash            INTEGER NOT NULL DEFAULT 0,   -- raw dollars
    cash_per_share        INTEGER NOT NULL DEFAULT 0,   -- * 100 (cents)
    total_debt            INTEGER NOT NULL DEFAULT 0,   -- raw dollars
    debt_to_equity        INTEGER NOT NULL DEFAULT 0,   -- * 100
    current_ratio         INTEGER NOT NULL DEFAULT 0,   -- * 100
    book_value_per_share  INTEGER NOT NULL DEFAULT 0,   -- * 100 (cents)
    -- Cash flow
    operating_cash_flow   INTEGER NOT NULL DEFAULT 0,   -- raw dollars
    free_cash_flow        INTEGER NOT NULL DEFAULT 0,   -- raw dollars
    -- Margins & returns (* 10000 = bps, e.g. 48.16% = 4816)
    gross_margin          INTEGER NOT NULL DEFAULT 0,
    operating_margin      INTEGER NOT NULL DEFAULT 0,
    profit_margin         INTEGER NOT NULL DEFAULT 0,
    return_on_equity      INTEGER NOT NULL DEFAULT 0,
    return_on_assets      INTEGER NOT NULL DEFAULT 0,
    -- Stock statistics
    shares_outstanding    INTEGER NOT NULL DEFAULT 0,
    float_shares          INTEGER NOT NULL DEFAULT 0,
    avg_10_volume         INTEGER NOT NULL DEFAULT 0,
    avg_90_volume         INTEGER NOT NULL DEFAULT 0,
    shares_short          INTEGER NOT NULL DEFAULT 0,
    short_ratio           INTEGER NOT NULL DEFAULT 0,   -- * 100
    short_pct_outstanding INTEGER NOT NULL DEFAULT 0,   -- * 10000
    pct_held_insiders     INTEGER NOT NULL DEFAULT 0,   -- * 10000
    pct_held_institutions INTEGER NOT NULL DEFAULT 0,   -- * 10000
    -- Price summary
    week_52_high          INTEGER NOT NULL DEFAULT 0,   -- cents
    week_52_low           INTEGER NOT NULL DEFAULT 0,   -- cents
    week_52_change        INTEGER NOT NULL DEFAULT 0,   -- * 10000
    beta                  INTEGER NOT NULL DEFAULT 0,   -- * 100
    day_50_ma             INTEGER NOT NULL DEFAULT 0,   -- cents
    day_200_ma            INTEGER NOT NULL DEFAULT 0,   -- cents
    -- Dividends & splits
    forward_div_rate      INTEGER NOT NULL DEFAULT 0,   -- * 100 (cents)
    forward_div_yield     INTEGER NOT NULL DEFAULT 0,   -- * 10000
    trailing_div_rate     INTEGER NOT NULL DEFAULT 0,   -- * 100 (cents)
    trailing_div_yield    INTEGER NOT NULL DEFAULT 0,   -- * 10000
    avg_div_yield_5y      INTEGER NOT NULL DEFAULT 0,   -- * 100
    payout_ratio          INTEGER NOT NULL DEFAULT 0,   -- * 10000
    dividend_frequency    TEXT    NOT NULL DEFAULT '',
    dividend_date         TEXT    NOT NULL DEFAULT '',
    ex_dividend_date      TEXT    NOT NULL DEFAULT '',
    last_split_factor     TEXT    NOT NULL DEFAULT '',
    last_split_date       TEXT    NOT NULL DEFAULT '',
    -- Fiscal dates
    fiscal_year_ends      TEXT    NOT NULL DEFAULT '',
    most_recent_quarter   TEXT    NOT NULL DEFAULT '',
    -- Supplementary fields
    logo                  TEXT    NOT NULL DEFAULT '',  -- URL to company logo
    ipo_date              TEXT    NOT NULL DEFAULT '',  -- IPO date YYYY-MM-DD
    peers                 TEXT    NOT NULL DEFAULT '',  -- comma-separated peer tickers
    --
    updated_at            TEXT    NOT NULL DEFAULT (datetime('now'))
);

--------------------------------------------------------------------------------
-- Stock News (TickerTick API — replaced on each refresh)
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS stock_news (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    stock_id     INTEGER NOT NULL REFERENCES stocks(id) ON DELETE CASCADE,
    headline     TEXT    NOT NULL,
    summary      TEXT    NOT NULL DEFAULT '',
    source       TEXT    NOT NULL DEFAULT '',
    url          TEXT    NOT NULL,
    published_at INTEGER NOT NULL,              -- unix timestamp
    created_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_stock_news_stock ON stock_news(stock_id, published_at DESC);

--------------------------------------------------------------------------------
-- Analyst Recommendations
--------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS stock_recommendations (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    stock_id    INTEGER NOT NULL REFERENCES stocks(id) ON DELETE CASCADE,
    period      TEXT    NOT NULL,              -- YYYY-MM-DD (month of the recommendation)
    strong_buy  INTEGER NOT NULL DEFAULT 0,
    buy         INTEGER NOT NULL DEFAULT 0,
    hold        INTEGER NOT NULL DEFAULT 0,
    sell        INTEGER NOT NULL DEFAULT 0,
    strong_sell INTEGER NOT NULL DEFAULT 0,
    UNIQUE(stock_id, period)
);
CREATE INDEX IF NOT EXISTS idx_stock_recs_stock ON stock_recommendations(stock_id, period DESC);
