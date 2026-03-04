ALTER TABLE ticket_state ADD COLUMN deferred_at TEXT;
UPDATE ticket_state SET deferred_at = updated_at WHERE review_after IS NOT NULL;

INSERT OR IGNORE INTO migrations (migration_number, migration_name) VALUES (4, '004-deferred-at');
