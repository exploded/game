-- Triggers. OpenDB (internal/handler/db.go) runs this on every start, after
-- addedColumns: a trigger that names a column an older database doesn't have
-- yet makes SQLite refuse to drop any column, so these can't go in
-- schema.sql, which runs first. Tests that apply schema.sql themselves run
-- this after it.

-- participants.traded and uses_ai, kept by triggers so every way a trade or
-- an order is stored (the ticket, the API and MCP, limit fills, queued market
-- orders, tests) keeps them right. The leaderboard reads them instead of
-- looking through every player's trades (docs/capacity.md). A deleted trade
-- doesn't clear them.
CREATE TRIGGER IF NOT EXISTS participants_traded AFTER INSERT ON transactions
BEGIN
    UPDATE participants SET traded = 1, uses_ai = (uses_ai OR NEW.source != '')
    WHERE id = NEW.participant_id AND (traded = 0 OR (uses_ai = 0 AND NEW.source != ''));
END;

CREATE TRIGGER IF NOT EXISTS participants_ai_order AFTER INSERT ON limit_orders WHEN NEW.source != ''
BEGIN
    UPDATE participants SET uses_ai = 1 WHERE id = NEW.participant_id AND uses_ai = 0;
END;
