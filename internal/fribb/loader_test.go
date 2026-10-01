package fribb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func TestParseTMDBID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *int
	}{
		{
			name:     "tv object",
			input:    `{"tv": 26209}`,
			expected: ptr(26209),
		},
		{
			name:     "movie array",
			input:    `{"movie": [128, 456]}`,
			expected: ptr(128),
		},
		{
			name:     "movie single int",
			input:    `{"movie": 789}`,
			expected: ptr(789),
		},
		{
			name:     "direct int",
			input:    `12345`,
			expected: ptr(12345),
		},
		{
			name:     "null",
			input:    `null`,
			expected: nil,
		},
		{
			name:     "empty object",
			input:    `{}`,
			expected: nil,
		},
		{
			name:     "empty raw",
			input:    ``,
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTMDBID(json.RawMessage(tc.input))
			if tc.expected == nil {
				if got != nil {
					t.Fatalf("expected nil, got %v", *got)
				}
			} else {
				if got == nil {
					t.Fatalf("expected %d, got nil", *tc.expected)
				}
				if *got != *tc.expected {
					t.Fatalf("expected %d, got %d", *tc.expected, *got)
				}
			}
		})
	}
}

func TestParseTVDBSeason(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "season object normal",
			input:    `{"tvdb": 1, "tmdb": 1}`,
			expected: 1,
		},
		{
			name:     "season object zero specials",
			input:    `{"tvdb": 0, "tmdb": 0}`,
			expected: 0,
		},
		{
			name:     "season object season 3",
			input:    `{"tvdb": 3}`,
			expected: 3,
		},
		{
			name:     "direct int",
			input:    `2`,
			expected: 2,
		},
		{
			name:     "null defaults to 1",
			input:    `null`,
			expected: 1,
		},
		{
			name:     "empty object defaults to 1",
			input:    `{}`,
			expected: 1,
		},
		{
			name:     "empty raw defaults to 1",
			input:    ``,
			expected: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTVDBSeason(json.RawMessage(tc.input))
			if got != tc.expected {
				t.Fatalf("expected %d, got %d", tc.expected, got)
			}
		})
	}
}

func TestLoader_Sync_InitialDownloadAnd304Conditional(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_sync.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	dataset := `[
		{"type":"TV","anilist_id":290,"mal_id":290,"tvdb_id":72025,"themoviedb_id":{"tv":26209},"season":{"tvdb":1,"tmdb":1}},
		{"type":"MOVIE","anilist_id":164,"mal_id":164,"themoviedb_id":{"movie":[128]}},
		{"type":"OVA","anilist_id":821,"mal_id":821,"tvdb_id":70900,"season":{"tvdb":0}},
		{"type":"TV","anidb_id":999}
	]`

	var requestCount atomic.Int32
	var lastIfNoneMatch atomic.Pointer[string]

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		inm := r.Header.Get("If-None-Match")
		if inm != "" {
			copyInm := inm
			lastIfNoneMatch.Store(&copyInm)
		}

		if inm == `"etag-test-123"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("ETag", `"etag-test-123"`)
		w.Header().Set("Last-Modified", "Wed, 01 Oct 2026 12:00:00 GMT")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(dataset))
	}))
	defer server.Close()

	loader := NewLoader(db,
		WithURL(server.URL),
		WithBatchSize(2),
	)

	// Step 1: Initial sync (should fetch 200 OK and populate DB)
	res, err := loader.Sync(ctx)
	if err != nil {
		t.Fatalf("loader.Sync failed: %v", err)
	}
	if res.NotModified {
		t.Fatalf("expected NotModified=false on initial sync")
	}
	if res.EntryCount != 3 {
		t.Fatalf("expected 3 valid entries, got %d", res.EntryCount)
	}
	if res.ETag != `"etag-test-123"` {
		t.Fatalf("expected etag '\"etag-test-123\"', got %q", res.ETag)
	}

	// Verify meta record in DB
	meta, err := loader.GetMeta(ctx)
	if err != nil {
		t.Fatalf("loader.GetMeta failed: %v", err)
	}
	if meta.ETag != `"etag-test-123"` {
		t.Fatalf("expected stored etag '\"etag-test-123\"', got %q", meta.ETag)
	}
	if meta.LastModified != "Wed, 01 Oct 2026 12:00:00 GMT" {
		t.Fatalf("expected stored last_modified, got %q", meta.LastModified)
	}
	if meta.EntryCount != 3 {
		t.Fatalf("expected stored entry_count 3, got %d", meta.EntryCount)
	}
	if meta.LastCheckedAt.IsZero() {
		t.Fatalf("expected last_checked_at to be non-zero")
	}

	// Verify individual entries
	e290, err := loader.GetEntry(ctx, 290)
	if err != nil {
		t.Fatalf("GetEntry(290) failed: %v", err)
	}
	if e290.MediaType != "TV" {
		t.Fatalf("expected MediaType TV, got %s", e290.MediaType)
	}
	if e290.TVDBID == nil || *e290.TVDBID != 72025 {
		t.Fatalf("expected TVDBID 72025, got %v", e290.TVDBID)
	}
	if e290.TMDBID == nil || *e290.TMDBID != 26209 {
		t.Fatalf("expected TMDBID 26209, got %v", e290.TMDBID)
	}
	if e290.MALID == nil || *e290.MALID != 290 {
		t.Fatalf("expected MALID 290, got %v", e290.MALID)
	}
	if e290.TVDBSeason != 1 {
		t.Fatalf("expected TVDBSeason 1, got %d", e290.TVDBSeason)
	}

	// Movie entry without tvdb_id
	e164, err := loader.GetEntry(ctx, 164)
	if err != nil {
		t.Fatalf("GetEntry(164) failed: %v", err)
	}
	if e164.TVDBID != nil {
		t.Fatalf("expected nil TVDBID, got %v", *e164.TVDBID)
	}
	if e164.TMDBID == nil || *e164.TMDBID != 128 {
		t.Fatalf("expected TMDBID 128, got %v", e164.TMDBID)
	}
	if e164.TVDBSeason != 1 {
		t.Fatalf("expected default TVDBSeason 1, got %d", e164.TVDBSeason)
	}

	// OVA entry with season 0
	e821, err := loader.GetEntry(ctx, 821)
	if err != nil {
		t.Fatalf("GetEntry(821) failed: %v", err)
	}
	if e821.TVDBSeason != 0 {
		t.Fatalf("expected TVDBSeason 0, got %d", e821.TVDBSeason)
	}

	// Item without anilist_id should not exist
	_, err = loader.GetEntry(ctx, 999)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for 999, got %v", err)
	}

	// Step 2: Second sync (should send If-None-Match and receive 304 Not Modified)
	res2, err := loader.Sync(ctx)
	if err != nil {
		t.Fatalf("loader.Sync 304 failed: %v", err)
	}
	if !res2.NotModified {
		t.Fatalf("expected NotModified=true on conditional sync")
	}
	if lastInm := lastIfNoneMatch.Load(); lastInm == nil || *lastInm != `"etag-test-123"` {
		t.Fatalf("expected If-None-Match '\"etag-test-123\"', got %v", lastInm)
	}
}

func TestLoader_Sync_BatchesAndETagUpdate(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_batches.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Generate 2500 entries to verify multiple 1000-row batch transactions
	totalItems := 2500
	var sb strings.Builder
	sb.WriteString("[")
	for i := 1; i <= totalItems; i++ {
		if i > 1 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"anilist_id":%d,"tvdb_id":%d,"mal_id":%d,"type":"TV"}`, i, 100000+i, 200000+i)
	}
	sb.WriteString("]")
	payload := sb.String()

	currentETag := atomic.Pointer[string]{}
	v1 := `"v1"`
	currentETag.Store(&v1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		etag := *currentETag.Load()
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	loader := NewLoader(db,
		WithURL(server.URL),
		WithBatchSize(1000),
	)

	res, err := loader.Sync(ctx)
	if err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}
	if res.EntryCount != totalItems {
		t.Fatalf("expected %d entries, got %d", totalItems, res.EntryCount)
	}

	// Verify all 2500 entries exist
	for _, id := range []int{1, 1000, 1001, 2000, 2001, 2500} {
		entry, err := loader.GetEntry(ctx, id)
		if err != nil {
			t.Fatalf("entry %d not found: %v", id, err)
		}
		if *entry.TVDBID != 100000+id {
			t.Fatalf("entry %d has wrong tvdb_id: %d", id, *entry.TVDBID)
		}
	}

	// Update upstream etag to v2
	v2 := `"v2"`
	currentETag.Store(&v2)

	resUpdate, err := loader.Sync(ctx)
	if err != nil {
		t.Fatalf("update sync failed: %v", err)
	}
	if resUpdate.NotModified {
		t.Fatalf("expected update to ingest new dataset")
	}
	if resUpdate.ETag != `"v2"` {
		t.Fatalf("expected ETag '\"v2\"', got %q", resUpdate.ETag)
	}
}

func TestLoader_Sync_ServerErrorsAndMalformed(t *testing.T) {
	ctx := context.Background()

	t.Run("server 500 error", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "test_err500.db")
		db, err := storage.Open(dbPath)
		if err != nil {
			t.Fatalf("storage.Open failed: %v", err)
		}
		defer func() { _ = db.Close() }()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		loader := NewLoader(db, WithURL(server.URL))
		_, err = loader.Sync(ctx)
		if err == nil {
			t.Fatalf("expected error on 500, got nil")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Fatalf("expected 500 in error, got %v", err)
		}
	})

	t.Run("malformed json stream", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "test_malformed.db")
		db, err := storage.Open(dbPath)
		if err != nil {
			t.Fatalf("storage.Open failed: %v", err)
		}
		defer func() { _ = db.Close() }()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("ETag", `"etag-malformed"`)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"anilist_id":1, "invalid_json"`))
		}))
		defer server.Close()

		loader := NewLoader(db, WithURL(server.URL))
		_, err = loader.Sync(ctx)
		if err == nil {
			t.Fatalf("expected error on malformed JSON, got nil")
		}
	})
}

func TestLoader_MemoryConsumptionUnder15MB(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_mem.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Stream 35,000 JSON items directly to simulate full community dataset
	const count = 35000
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"etag-large"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("["))
		for i := 1; i <= count; i++ {
			if i > 1 {
				_, _ = w.Write([]byte(","))
			}
			item := fmt.Sprintf(`{"anilist_id":%d,"tvdb_id":%d,"mal_id":%d,"type":"TV","themoviedb_id":{"tv":%d},"season":{"tvdb":1,"tmdb":1}}`,
				i, 10000+i, 20000+i, 30000+i)
			_, _ = w.Write([]byte(item))
			if i%5000 == 0 && flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = w.Write([]byte("]"))
	}))
	defer server.Close()

	loader := NewLoader(db,
		WithURL(server.URL),
		WithBatchSize(1000),
	)

	runtime.GC()
	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	res, err := loader.Sync(ctx)
	if err != nil {
		t.Fatalf("large stream sync failed: %v", err)
	}
	if res.EntryCount != count {
		t.Fatalf("expected %d entries, got %d", count, res.EntryCount)
	}

	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	allocDeltaBytes := int64(mAfter.Alloc) - int64(mBefore.Alloc)
	maxAllowedBytes := int64(15 * 1024 * 1024) // 15MB limit

	t.Logf("Memory stats: AllocBefore=%d KB, AllocAfter=%d KB, Delta=%d KB",
		mBefore.Alloc/1024, mAfter.Alloc/1024, allocDeltaBytes/1024)

	if allocDeltaBytes > maxAllowedBytes {
		t.Fatalf("active heap allocation delta %d bytes exceeded 15MB limit", allocDeltaBytes)
	}
}

func ptr[T any](v T) *T {
	return &v
}
