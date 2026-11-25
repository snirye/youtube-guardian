ALTER TABLE configuration ADD COLUMN block_shorts INTEGER NOT NULL DEFAULT 1 CHECK (block_shorts IN (0, 1));
ALTER TABLE approval_requests ADD COLUMN telegram_notified_at_ms INTEGER;

CREATE TABLE extension_heartbeat (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    received_at_ms INTEGER NOT NULL,
    extension_version TEXT NOT NULL,
    configuration_version INTEGER,
    active_tabs INTEGER CHECK (active_tabs >= 0)
) STRICT;

CREATE TABLE telegram_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    next_update INTEGER NOT NULL CHECK (next_update >= 0)
) STRICT;

INSERT INTO telegram_state (id, next_update) VALUES (1, 0);
