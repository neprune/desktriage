CREATE TABLE IF NOT EXISTS account_manager (
    company_id  INTEGER NOT NULL,
    agent_id    INTEGER NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (company_id, agent_id)
);

INSERT OR IGNORE INTO migrations (migration_number, migration_name) VALUES (5, '005-account-managers');
