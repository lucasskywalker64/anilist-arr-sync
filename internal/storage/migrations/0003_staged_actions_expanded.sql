-- Expand staged_sync_actions action_type CHECK constraint to support file deletion and watched episode unmonitoring/pruning
CREATE TABLE IF NOT EXISTS staged_sync_actions_new (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id         INTEGER,
    action_type    TEXT NOT NULL CHECK(action_type IN ('ADD_MOVIE', 'ADD_SERIES', 'MONITOR_SEASON', 'UNMONITOR_SEASON', 'UNMONITOR_MOVIE', 'DELETE_FILE', 'UNMONITOR_EPISODE', 'DELETE_EPISODE_FILE')),
    media_type     TEXT NOT NULL,
    title          TEXT NOT NULL,
    target_service TEXT NOT NULL,
    payload_json   TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING', 'APPLIED', 'REJECTED')),
    executed_at    DATETIME,
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO staged_sync_actions_new (id, run_id, action_type, media_type, title, target_service, payload_json, status, executed_at, created_at)
SELECT id, run_id, action_type, media_type, title, target_service, payload_json, status, executed_at, created_at
FROM staged_sync_actions;

DROP TABLE staged_sync_actions;

ALTER TABLE staged_sync_actions_new RENAME TO staged_sync_actions;
