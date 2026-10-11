CREATE TABLE IF NOT EXISTS relationships (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    source        TEXT NOT NULL,
    target        TEXT NOT NULL,
    relationship  TEXT NOT NULL,
    evidence      TEXT,
    created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_relationships_source ON relationships (source);
CREATE INDEX IF NOT EXISTS idx_relationships_target ON relationships (target);
CREATE INDEX IF NOT EXISTS idx_relationships_source_target ON relationships (source, target);
