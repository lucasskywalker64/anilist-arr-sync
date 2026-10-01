package fribb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

const (
	// DefaultURL is the upstream GitHub Raw URL for the community Fribb mapping dataset.
	DefaultURL = "https://raw.githubusercontent.com/Fribb/anime-lists/master/anime-list-mini.json"

	// DefaultMetaKey is the primary key used in the fribb_meta table.
	DefaultMetaKey = "anime-list-mini.json"

	// DefaultBatchSize is the number of rows inserted per SQLite transaction.
	DefaultBatchSize = 1000

	// DefaultTimeout is the HTTP request timeout.
	DefaultTimeout = 60 * time.Second
)

// Option configures a Loader instance.
type Option func(*Loader)

// WithURL sets a custom dataset download URL.
func WithURL(u string) Option {
	return func(l *Loader) {
		l.url = u
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(l *Loader) {
		l.httpClient = client
	}
}

// WithBatchSize sets the database batch insertion chunk size.
func WithBatchSize(size int) Option {
	return func(l *Loader) {
		if size > 0 {
			l.batchSize = size
		}
	}
}

// WithMetaKey sets the metadata primary key for fribb_meta.
func WithMetaKey(key string) Option {
	return func(l *Loader) {
		l.metaKey = key
	}
}

// Loader manages streaming ingestion of the community Fribb dataset into SQLite.
type Loader struct {
	db         *storage.DB
	httpClient *http.Client
	url        string
	batchSize  int
	metaKey    string
}

// NewLoader creates a Loader instance with configured options.
func NewLoader(db *storage.DB, opts ...Option) *Loader {
	l := &Loader{
		db:         db,
		httpClient: &http.Client{Timeout: DefaultTimeout},
		url:        DefaultURL,
		batchSize:  DefaultBatchSize,
		metaKey:    DefaultMetaKey,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Sync performs conditional HTTP fetch and streaming database ingestion.
func (l *Loader) Sync(ctx context.Context) (*Result, error) {
	var existingMeta *Meta
	m, err := l.GetMeta(ctx)
	if err == nil {
		existingMeta = m
	} else if !errors.Is(err, ErrMetaNotFound) {
		return nil, fmt.Errorf("fribb: failed to query metadata: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.url, nil)
	if err != nil {
		return nil, fmt.Errorf("fribb: failed to create request: %w", err)
	}

	if existingMeta != nil && existingMeta.ETag != "" {
		req.Header.Set("If-None-Match", existingMeta.ETag)
	}

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fribb: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		err = l.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE fribb_meta SET last_checked_at = CURRENT_TIMESTAMP WHERE key = ?;`, l.metaKey)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("fribb: failed to update last_checked_at on 304: %w", err)
		}

		entryCount := 0
		etag := ""
		if existingMeta != nil {
			entryCount = existingMeta.EntryCount
			etag = existingMeta.ETag
		}
		return &Result{
			NotModified: true,
			ETag:        etag,
			EntryCount:  entryCount,
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fribb: unexpected HTTP response status %d", resp.StatusCode)
	}

	etag := resp.Header.Get("ETag")
	lastModified := resp.Header.Get("Last-Modified")

	count, err := l.loadStream(ctx, resp.Body)
	if err != nil {
		return nil, err
	}

	err = l.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		query := `
			INSERT INTO fribb_meta (key, etag, last_modified, last_checked_at, entry_count)
			VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?)
			ON CONFLICT(key) DO UPDATE SET
				etag = excluded.etag,
				last_modified = excluded.last_modified,
				last_checked_at = CURRENT_TIMESTAMP,
				entry_count = excluded.entry_count;
		`
		_, err := tx.ExecContext(ctx, query, l.metaKey, etag, lastModified, count)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("fribb: failed to save metadata: %w", err)
	}

	return &Result{
		NotModified: false,
		ETag:        etag,
		EntryCount:  count,
	}, nil
}

// loadStream deserializes the incoming JSON array stream and batch inserts rows into storage.
func (l *Loader) loadStream(ctx context.Context, r io.Reader) (int, error) {
	dec := json.NewDecoder(r)

	t, err := dec.Token()
	if err != nil {
		return 0, fmt.Errorf("fribb: failed to read JSON opening token: %w", err)
	}
	delim, ok := t.(json.Delim)
	if !ok || delim != '[' {
		return 0, fmt.Errorf("fribb: expected JSON array delimiter '['")
	}

	batch := make([]Entry, 0, l.batchSize)
	totalInserted := 0

	for dec.More() {
		var raw rawEntry
		if err := dec.Decode(&raw); err != nil {
			return 0, fmt.Errorf("fribb: failed to decode JSON entry: %w", err)
		}

		if raw.AnilistID == nil || *raw.AnilistID <= 0 {
			continue
		}

		mediaType := ""
		if raw.Type != nil {
			mediaType = *raw.Type
		}

		entry := Entry{
			AnilistID:  *raw.AnilistID,
			TVDBID:     raw.TVDBID,
			TMDBID:     parseTMDBID(raw.TheMovieDBID),
			MALID:      raw.MALID,
			MediaType:  mediaType,
			TVDBSeason: parseTVDBSeason(raw.Season),
		}

		batch = append(batch, entry)
		if len(batch) >= l.batchSize {
			if err := l.insertBatch(ctx, batch); err != nil {
				return 0, err
			}
			totalInserted += len(batch)
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		if err := l.insertBatch(ctx, batch); err != nil {
			return 0, err
		}
		totalInserted += len(batch)
	}

	t, err = dec.Token()
	if err != nil {
		return 0, fmt.Errorf("fribb: failed to read JSON closing token: %w", err)
	}
	delim, ok = t.(json.Delim)
	if !ok || delim != ']' {
		return 0, fmt.Errorf("fribb: expected JSON array closing delimiter ']'")
	}

	return totalInserted, nil
}

// insertBatch commits a slice of entries inside a single database transaction.
func (l *Loader) insertBatch(ctx context.Context, batch []Entry) error {
	if len(batch) == 0 {
		return nil
	}

	return l.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		query := `
			INSERT INTO fribb_entries (anilist_id, tvdb_id, tmdb_id, mal_id, media_type, tvdb_season, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(anilist_id) DO UPDATE SET
				tvdb_id = excluded.tvdb_id,
				tmdb_id = excluded.tmdb_id,
				mal_id = excluded.mal_id,
				media_type = excluded.media_type,
				tvdb_season = excluded.tvdb_season,
				updated_at = CURRENT_TIMESTAMP;
		`
		stmt, err := tx.PrepareContext(ctx, query)
		if err != nil {
			return fmt.Errorf("fribb: failed to prepare batch insert statement: %w", err)
		}
		defer func() { _ = stmt.Close() }()

		for _, entry := range batch {
			_, err := stmt.ExecContext(ctx,
				entry.AnilistID,
				entry.TVDBID,
				entry.TMDBID,
				entry.MALID,
				entry.MediaType,
				entry.TVDBSeason,
			)
			if err != nil {
				return fmt.Errorf("fribb: failed to execute batch insert for anilist_id %d: %w", entry.AnilistID, err)
			}
		}
		return nil
	})
}

// GetMeta retrieves upstream Fribb dataset caching metadata from storage.
func (l *Loader) GetMeta(ctx context.Context) (*Meta, error) {
	var meta Meta
	var rawChecked string
	err := l.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		row := q.QueryRowContext(ctx, `
			SELECT key, COALESCE(etag, ''), COALESCE(last_modified, ''), COALESCE(last_checked_at, ''), entry_count
			FROM fribb_meta
			WHERE key = ?;
		`, l.metaKey)
		return row.Scan(&meta.Key, &meta.ETag, &meta.LastModified, &rawChecked, &meta.EntryCount)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMetaNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fribb: failed to get meta: %w", err)
	}

	if rawChecked != "" {
		if t, err := time.Parse(time.RFC3339, rawChecked); err == nil {
			meta.LastCheckedAt = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", rawChecked); err == nil {
			meta.LastCheckedAt = t
		} else if t, err := time.Parse("2006-01-02T15:04:05Z", rawChecked); err == nil {
			meta.LastCheckedAt = t
		}
	}
	return &meta, nil
}

// GetEntry retrieves a single cached mapping record by AniList ID.
func (l *Loader) GetEntry(ctx context.Context, anilistID int) (*Entry, error) {
	var entry Entry
	var tvdbID, tmdbID, malID sql.NullInt64
	var mediaType sql.NullString
	var rawUpdated string

	err := l.db.Read(ctx, func(ctx context.Context, q storage.Querier) error {
		row := q.QueryRowContext(ctx, `
			SELECT anilist_id, tvdb_id, tmdb_id, mal_id, media_type, tvdb_season, COALESCE(updated_at, '')
			FROM fribb_entries
			WHERE anilist_id = ?;
		`, anilistID)
		return row.Scan(&entry.AnilistID, &tvdbID, &tmdbID, &malID, &mediaType, &entry.TVDBSeason, &rawUpdated)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fribb: failed to get entry %d: %w", anilistID, err)
	}

	if tvdbID.Valid {
		v := int(tvdbID.Int64)
		entry.TVDBID = &v
	}
	if tmdbID.Valid {
		v := int(tmdbID.Int64)
		entry.TMDBID = &v
	}
	if malID.Valid {
		v := int(malID.Int64)
		entry.MALID = &v
	}
	if mediaType.Valid {
		entry.MediaType = mediaType.String
	}
	if rawUpdated != "" {
		if t, err := time.Parse(time.RFC3339, rawUpdated); err == nil {
			entry.UpdatedAt = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", rawUpdated); err == nil {
			entry.UpdatedAt = t
		}
	}

	return &entry, nil
}
