-- Local administrative user accounts
CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Manual overrides and user-confirmed title mappings
CREATE TABLE IF NOT EXISTS mapping_overrides (
    anilist_id       INTEGER PRIMARY KEY,
    media_type       TEXT NOT NULL CHECK(media_type IN ('MOVIE', 'SERIES')),
    tvdb_id          INTEGER,
    tmdb_id          INTEGER,
    seasons          TEXT DEFAULT '1',
    title_override   TEXT,
    created_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Ambiguous or low-confidence titles requiring manual confirmation
CREATE TABLE IF NOT EXISTS review_queue (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    anilist_id       INTEGER NOT NULL,
    media_type       TEXT NOT NULL CHECK(media_type IN ('MOVIE', 'SERIES')),
    title_romaji     TEXT NOT NULL,
    title_english    TEXT,
    release_year     INTEGER,
    poster_url       TEXT,
    candidates_json  TEXT NOT NULL,
    reason           TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING', 'RESOLVED')),
    resolved_id      INTEGER,
    resolved_season  INTEGER,
    created_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolved_at      DATETIME
);

-- Staged sync operations for interactive preview and first-run dry runs
CREATE TABLE IF NOT EXISTS staged_sync_actions (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id         INTEGER,
    action_type    TEXT NOT NULL CHECK(action_type IN ('ADD_MOVIE', 'ADD_SERIES', 'MONITOR_SEASON', 'UNMONITOR_SEASON', 'UNMONITOR_MOVIE')),
    media_type     TEXT NOT NULL,
    title          TEXT NOT NULL,
    target_service TEXT NOT NULL,
    payload_json   TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING', 'APPLIED', 'REJECTED')),
    executed_at    DATETIME,
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Upstream Fribb dataset cache metadata
CREATE TABLE IF NOT EXISTS fribb_meta (
    key              TEXT PRIMARY KEY,
    etag             TEXT,
    last_modified    TEXT,
    last_checked_at  DATETIME,
    entry_count      INTEGER DEFAULT 0
);

-- Cached mapping records from anime-list-mini.json
CREATE TABLE IF NOT EXISTS fribb_entries (
    anilist_id       INTEGER PRIMARY KEY,
    tvdb_id          INTEGER,
    tmdb_id          INTEGER,
    mal_id           INTEGER,
    media_type       TEXT,
    tvdb_season      INTEGER DEFAULT 1,
    updated_at       DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_fribb_tvdb ON fribb_entries(tvdb_id);
CREATE INDEX IF NOT EXISTS idx_fribb_tmdb ON fribb_entries(tmdb_id);

-- Execution log and historical run records
CREATE TABLE IF NOT EXISTS sync_history (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    run_timestamp      DATETIME DEFAULT CURRENT_TIMESTAMP,
    duration_ms        INTEGER NOT NULL,
    status             TEXT NOT NULL CHECK(status IN ('SUCCESS', 'PARTIAL', 'FAILED')),
    items_scanned      INTEGER NOT NULL DEFAULT 0,
    added_radarr       INTEGER NOT NULL DEFAULT 0,
    monitored_sonarr   INTEGER NOT NULL DEFAULT 0,
    unmonitored_sonarr INTEGER NOT NULL DEFAULT 0,
    unmonitored_radarr INTEGER NOT NULL DEFAULT 0,
    queued_review      INTEGER NOT NULL DEFAULT 0,
    errors_json        TEXT,
    trigger_type       TEXT NOT NULL CHECK(trigger_type IN ('SCHEDULED', 'MANUAL_WEB', 'CLI'))
);

-- Update execution history and circuit breaker tracking
CREATE TABLE IF NOT EXISTS update_history (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    version          TEXT NOT NULL,
    status           TEXT NOT NULL CHECK(status IN ('SUCCESS', 'FAILED')),
    rolled_back      BOOLEAN NOT NULL DEFAULT 0,
    error_message    TEXT,
    cooldown_until   DATETIME,
    created_at       DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Permanent ignore list for skipped titles
CREATE TABLE IF NOT EXISTS ignored_titles (
    anilist_id       INTEGER PRIMARY KEY,
    title_romaji     TEXT NOT NULL,
    reason           TEXT,
    created_at       DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Notification connections
CREATE TABLE IF NOT EXISTS notification_connections (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    provider     TEXT NOT NULL CHECK(provider IN ('DISCORD', 'WEBHOOK')),
    config_json  TEXT NOT NULL,
    on_added     BOOLEAN NOT NULL DEFAULT 1,
    on_review    BOOLEAN NOT NULL DEFAULT 1,
    on_error     BOOLEAN NOT NULL DEFAULT 1,
    on_complete  BOOLEAN NOT NULL DEFAULT 0,
    on_update    BOOLEAN NOT NULL DEFAULT 1,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);
