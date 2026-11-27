CREATE TABLE channels (
    channel_id TEXT PRIMARY KEY NOT NULL,
    channel_name TEXT NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('approved', 'blocked')),
    updated_at_ms INTEGER NOT NULL
) STRICT;

CREATE TABLE approval_requests (
    id INTEGER PRIMARY KEY,
    channel_id TEXT NOT NULL,
    channel_name TEXT NOT NULL,
    video_url TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    requested_at_ms INTEGER NOT NULL,
    resolved_at_ms INTEGER,
    CHECK (
        (status = 'pending' AND resolved_at_ms IS NULL)
        OR (status IN ('approved', 'rejected') AND resolved_at_ms IS NOT NULL)
    )
) STRICT;

CREATE UNIQUE INDEX approval_requests_one_pending_per_channel
    ON approval_requests(channel_id) WHERE status = 'pending';

CREATE TABLE configuration (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    version INTEGER NOT NULL CHECK (version >= 1)
) STRICT;

INSERT INTO configuration (id, version) VALUES (1, 1);

CREATE TABLE activity_events (
    id INTEGER PRIMARY KEY,
    occurred_at_ms INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    channel_id TEXT,
    channel_name TEXT,
    video_id TEXT,
    permission_decision TEXT CHECK (permission_decision IN ('approved', 'denied')),
    approval_request_status TEXT CHECK (approval_request_status IN ('pending', 'approved', 'rejected')),
    configuration_version INTEGER CHECK (configuration_version >= 1)
) STRICT;

CREATE INDEX activity_events_by_time ON activity_events(occurred_at_ms);
