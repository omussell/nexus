CREATE TABLE IF NOT EXISTS datasets (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    source       TEXT    NOT NULL,
    version      TEXT    NOT NULL,
    collected_at TEXT    NOT NULL,
    initial_input TEXT   NOT NULL,
    output       TEXT    NOT NULL,
    UNIQUE (source, version)
);

CREATE TABLE IF NOT EXISTS latest (
    source     TEXT    PRIMARY KEY,
    version    TEXT    NOT NULL,
    dataset_id INTEGER NOT NULL REFERENCES datasets (id)
);
