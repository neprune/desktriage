CREATE TABLE IF NOT EXISTS migrations (
    migration_number INTEGER PRIMARY KEY,
    migration_name TEXT NOT NULL,
    executed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ticket_state (
    ticket_id INTEGER PRIMARY KEY,
    priority INTEGER NOT NULL DEFAULT 0,
    blocked INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    review_after TEXT,
    today INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS api_cache (
    cache_key TEXT PRIMARY KEY,
    response_body TEXT NOT NULL,
    etag TEXT,
    cached_at TEXT NOT NULL,
    max_age_secs INTEGER NOT NULL
);

INSERT OR IGNORE INTO migrations (migration_number, migration_name) VALUES (1, '001-base');
