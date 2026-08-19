CREATE TABLE IF NOT EXISTS cro_ids (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    croid       TEXT    NOT NULL UNIQUE,
    cro_type    TEXT    NOT NULL,
    cro_value   TEXT    NOT NULL,
    system      TEXT    NOT NULL,
    created_at  TEXT    NOT NULL,
    UNIQUE (cro_type, cro_value, system)
);
