-- Synced media library tracking titles, watch status, and Servarr mappings
CREATE TABLE IF NOT EXISTS synced_media (
    anilist_id       INTEGER PRIMARY KEY,
    media_type       TEXT NOT NULL CHECK(media_type IN ('SERIES', 'MOVIE')),
    format           TEXT,
    title_romaji     TEXT NOT NULL,
    title_english    TEXT,
    release_year     INTEGER,
    poster_url       TEXT,
    anilist_status   TEXT NOT NULL,
    target_service   TEXT CHECK(target_service IN ('SONARR', 'RADARR', 'NONE')),
    tvdb_id          INTEGER,
    tmdb_id          INTEGER,
    arr_title        TEXT,
    monitored_status TEXT NOT NULL DEFAULT 'UNMONITORED' CHECK(monitored_status IN ('MONITORED', 'UNMONITORED', 'NOT_FOUND', 'PENDING_REVIEW', 'IGNORED')),
    seasons          TEXT,
    last_synced_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_synced_media_status ON synced_media(anilist_status);
CREATE INDEX IF NOT EXISTS idx_synced_media_service ON synced_media(target_service);
CREATE INDEX IF NOT EXISTS idx_synced_media_monitored ON synced_media(monitored_status);
