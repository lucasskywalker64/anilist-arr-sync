CREATE TABLE review_preparations (
    review_id INTEGER PRIMARY KEY REFERENCES review_queue(id),
    endpoint TEXT NOT NULL,
    tvdb_id INTEGER NOT NULL,
    series_id INTEGER NOT NULL DEFAULT 0,
    tag_id INTEGER NOT NULL DEFAULT 0,
    token TEXT NOT NULL,
    created INTEGER NOT NULL DEFAULT 0,
    cancelled INTEGER NOT NULL DEFAULT 0,
    confirmed INTEGER NOT NULL DEFAULT 0,
    baseline TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(endpoint, tvdb_id)
);
