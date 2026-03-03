CREATE TABLE IF NOT EXISTS sprint_anchor (
    sprint_number INTEGER NOT NULL,  -- 1–26
    year          INTEGER NOT NULL,  -- e.g. 2026
    start_date    TEXT NOT NULL,      -- YYYY-MM-DD
    PRIMARY KEY (year, sprint_number)
);

INSERT OR IGNORE INTO migrations (migration_number, migration_name) VALUES (3, '003-sprint-anchors');
