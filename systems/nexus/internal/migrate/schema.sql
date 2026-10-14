-- CROIDs table: every CROID gets a single auto-increment integer identity.
CREATE TABLE IF NOT EXISTS cro_ids (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    croid      TEXT NOT NULL UNIQUE,
    system     TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_croid_croid ON cro_ids (croid);

-- Relationship types table: human-readable relationship names (about 100 rows).
CREATE TABLE IF NOT EXISTS relationship_types (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    relationship    TEXT NOT NULL UNIQUE,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_rel_type_type ON relationship_types (relationship);

-- Relationships: integer foreign keys into cro_ids and relationship_types.
CREATE TABLE IF NOT EXISTS relationships (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    source_croid_id        INTEGER NOT NULL REFERENCES cro_ids(id),
    target_croid_id        INTEGER NOT NULL REFERENCES cro_ids(id),
    relationship_type_id   INTEGER NOT NULL REFERENCES relationship_types(id),
    created_at             TEXT NOT NULL,
    UNIQUE (source_croid_id, target_croid_id, relationship_type_id)
);

CREATE INDEX IF NOT EXISTS idx_rel_source ON relationships (source_croid_id);
CREATE INDEX IF NOT EXISTS idx_rel_target ON relationships (target_croid_id);
CREATE INDEX IF NOT EXISTS idx_rel_type ON relationships (relationship_type_id);
CREATE INDEX IF NOT EXISTS idx_rel_created ON relationships (created_at);

-- Optional evidence stored separately to keep the main table compact.
CREATE TABLE IF NOT EXISTS relationship_evidence (
    relationship_id   INTEGER PRIMARY KEY REFERENCES relationships(id),
    evidence          TEXT
);
