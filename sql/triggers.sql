-- Triggers. OpenDB (internal/handler/db.go) runs this on every start, after
-- addedColumns: a trigger that names a column an older database doesn't have
-- yet makes SQLite refuse to drop any column, so these can't go in
-- schema.sql, which runs first. Tests that apply schema.sql themselves run
-- this after it. A trigger taken out goes in droppedTriggers (db.go).

-- participants.traded, kept by a trigger so every way a trade is stored (the
-- ticket, limit fills, queued market orders, tests) keeps it right. The
-- leaderboard reads it instead of looking through every player's trades
-- (docs/capacity.md). A deleted trade doesn't clear it.
CREATE TRIGGER IF NOT EXISTS participants_first_trade AFTER INSERT ON transactions
BEGIN
    UPDATE participants SET traded = 1 WHERE id = NEW.participant_id AND traded = 0;
END;
