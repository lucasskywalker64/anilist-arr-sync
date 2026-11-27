ALTER TABLE review_queue ADD COLUMN country_of_origin TEXT NOT NULL DEFAULT '';
ALTER TABLE synced_media ADD COLUMN country_of_origin TEXT NOT NULL DEFAULT '';
