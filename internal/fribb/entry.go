// Package fribb provides streaming ingestion and caching for the community Fribb mapping dataset.
package fribb

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when a requested Fribb entry does not exist in storage.
	ErrNotFound = errors.New("fribb: entry not found")

	// ErrMetaNotFound is returned when Fribb cache metadata does not exist.
	ErrMetaNotFound = errors.New("fribb: meta not found")

	// ErrSyncInProgress is returned when Sync is called while another sync is actively running.
	ErrSyncInProgress = errors.New("fribb: sync already in progress")
)

// Entry represents a cached mapping record from the Fribb dataset.
type Entry struct {
	AnilistID  int       `json:"anilist_id"`
	TVDBID     *int      `json:"tvdb_id,omitempty"`
	TMDBID     *int      `json:"tmdb_id,omitempty"`
	MALID      *int      `json:"mal_id,omitempty"`
	MediaType  string    `json:"media_type,omitempty"`
	TVDBSeason int       `json:"tvdb_season"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Meta holds caching metadata for the upstream Fribb dataset.
type Meta struct {
	Key           string    `json:"key"`
	ETag          string    `json:"etag"`
	LastModified  string    `json:"last_modified"`
	LastCheckedAt time.Time `json:"last_checked_at"`
	EntryCount    int       `json:"entry_count"`
}

// Result summarizes the outcome of a dataset synchronization run.
type Result struct {
	NotModified bool   `json:"not_modified"`
	ETag        string `json:"etag"`
	EntryCount  int    `json:"entry_count"`
}

// rawEntry models the raw incoming JSON entry from anime-list-mini.json.
type rawEntry struct {
	Type         *string         `json:"type"`
	AnilistID    *int            `json:"anilist_id"`
	MALID        *int            `json:"mal_id"`
	TVDBID       *int            `json:"tvdb_id"`
	TheMovieDBID json.RawMessage `json:"themoviedb_id"`
	Season       json.RawMessage `json:"season"`
}

// parseTMDBID parses the TMDb ID from multiple possible incoming formats:
// 1. Direct integer (e.g. 12345)
// 2. Object with "tv" int (e.g. {"tv": 26209})
// 3. Object with "movie" int or []int (e.g. {"movie": [128]})
func parseTMDBID(raw json.RawMessage) *int {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	// Direct integer format
	var direct int
	if err := json.Unmarshal(raw, &direct); err == nil && direct > 0 {
		return &direct
	}

	// Object format with tv or movie
	var obj struct {
		TV    *int            `json:"tv"`
		Movie json.RawMessage `json:"movie"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if obj.TV != nil && *obj.TV > 0 {
			return obj.TV
		}
		if len(obj.Movie) > 0 && string(obj.Movie) != "null" {
			var movieList []int
			if err := json.Unmarshal(obj.Movie, &movieList); err == nil && len(movieList) > 0 {
				if movieList[0] > 0 {
					return &movieList[0]
				}
			}
			var singleMovie int
			if err := json.Unmarshal(obj.Movie, &singleMovie); err == nil && singleMovie > 0 {
				return &singleMovie
			}
		}
	}

	return nil
}

// parseTVDBSeason parses the TVDB broadcast season from incoming JSON.
// Defaults to 1 if unspecified, null, or empty.
func parseTVDBSeason(raw json.RawMessage) int {
	const defaultSeason = 1
	if len(raw) == 0 || string(raw) == "null" {
		return defaultSeason
	}

	// Direct integer format
	var direct int
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct
	}

	// Object format with tvdb key
	var obj struct {
		TVDB *int `json:"tvdb"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.TVDB != nil {
		return *obj.TVDB
	}

	return defaultSeason
}
