package anilist_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
)

func TestFetchWatchlist_QueriesGraphQLWithPOSTAndUserAgent(t *testing.T) {
	var mu sync.Mutex
	var capturedMethod string
	var capturedUserAgent string
	var capturedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedMethod = r.Method
		capturedUserAgent = r.Header.Get("User-Agent")
		capturedContentType = r.Header.Get("Content-Type")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Unlock()

		resp := map[string]any{
			"data": map[string]any{
				"MediaListCollection": map[string]any{
					"hasNextChunk": false,
					"lists": []map[string]any{
						{
							"name":   "Watching",
							"status": "CURRENT",
							"entries": []map[string]any{
								{
									"id":     101,
									"status": "CURRENT",
									"media": map[string]any{
										"id":    5001,
										"idMal": 6001,
										"title": map[string]any{
											"romaji":        "Frieren: Beyond Journey's End",
											"english":       "Frieren: Beyond Journey's End",
											"native":        "葬送のフリーレン",
											"userPreferred": "Frieren: Beyond Journey's End",
										},
										"format": "TV",
										"status": "FINISHED",
										"startDate": map[string]any{
											"year":  2023,
											"month": 9,
											"day":   29,
										},
										"episodes": 28,
										"synonyms": []string{"Sousou no Frieren"},
									},
								},
							},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := anilist.NewClient(server.URL)
	entries, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username: "testuser",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	method := capturedMethod
	userAgent := capturedUserAgent
	contentType := capturedContentType
	mu.Unlock()

	if method != http.MethodPost {
		t.Errorf("expected POST method, got %q", method)
	}
	if userAgent != "AniList-Arr-Sync/1.0" {
		t.Errorf("expected User-Agent %q, got %q", "AniList-Arr-Sync/1.0", userAgent)
	}
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.ID != 101 {
		t.Errorf("expected entry ID 101, got %d", entry.ID)
	}
	if entry.Media.ID != 5001 {
		t.Errorf("expected media ID 5001, got %d", entry.Media.ID)
	}
	if entry.Media.Title.Romaji != "Frieren: Beyond Journey's End" {
		t.Errorf("expected title Romaji 'Frieren: Beyond Journey's End', got %q", entry.Media.Title.Romaji)
	}
	if entry.Media.Format != "TV" {
		t.Errorf("expected format 'TV', got %q", entry.Media.Format)
	}
}

func TestFetchWatchlist_FiltersStatusAndUnreleased(t *testing.T) {
	var mu sync.Mutex
	var capturedVariables map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		capturedVariables, _ = req["variables"].(map[string]any)
		mu.Unlock()

		resp := map[string]any{
			"data": map[string]any{
				"MediaListCollection": map[string]any{
					"hasNextChunk": false,
					"lists": []map[string]any{
						{
							"name":   "Custom List",
							"status": "CURRENT",
							"entries": []map[string]any{
								{
									"id":     1,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     1001,
										"format": "TV",
										"status": "RELEASING",
										"title":  map[string]any{"romaji": "Releasing Anime"},
									},
								},
								{
									"id":     2,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     1002,
										"format": "TV",
										"status": "NOT_YET_RELEASED",
										"title":  map[string]any{"romaji": "Future Anime"},
									},
								},
							},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := anilist.NewClient(server.URL)

	t.Run("exclude unreleased media by default", func(t *testing.T) {
		entries, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
			Username:          "testuser",
			Statuses:          []anilist.MediaListStatus{anilist.StatusCurrent, anilist.StatusPlanning},
			IncludeUnreleased: false,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		vars := capturedVariables
		mu.Unlock()

		statusesRaw, ok := vars["statusIn"].([]any)
		if !ok || len(statusesRaw) != 2 {
			t.Fatalf("expected statusIn with 2 elements in variables, got %#v", vars["statusIn"])
		}

		if len(entries) != 1 {
			t.Fatalf("expected 1 released entry, got %d", len(entries))
		}
		if entries[0].Media.ID != 1001 {
			t.Errorf("expected media ID 1001, got %d", entries[0].Media.ID)
		}
	})

	t.Run("include unreleased media when toggle enabled", func(t *testing.T) {
		entries, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
			Username:          "testuser",
			IncludeUnreleased: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(entries) != 2 {
			t.Fatalf("expected 2 entries when unreleased included, got %d", len(entries))
		}
	})
}

func TestFetchWatchlist_PaginationMultiChunk(t *testing.T) {
	var chunkRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunkRequests.Add(1)
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		vars := req["variables"].(map[string]any)

		chunk := 1
		if c, ok := vars["chunk"].(float64); ok {
			chunk = int(c)
		}

		var resp map[string]any
		if chunk == 1 {
			resp = map[string]any{
				"data": map[string]any{
					"MediaListCollection": map[string]any{
						"hasNextChunk": true,
						"lists": []map[string]any{
							{
								"name":   "Chunk 1 List",
								"status": "CURRENT",
								"entries": []map[string]any{
									{
										"id":     1,
										"status": "CURRENT",
										"media": map[string]any{
											"id":     101,
											"format": "TV",
											"status": "FINISHED",
											"title":  map[string]any{"romaji": "Anime 1"},
										},
									},
								},
							},
						},
					},
				},
			}
		} else {
			resp = map[string]any{
				"data": map[string]any{
					"MediaListCollection": map[string]any{
						"hasNextChunk": false,
						"lists": []map[string]any{
							{
								"name":   "Chunk 2 List",
								"status": "CURRENT",
								"entries": []map[string]any{
									{
										"id":     2,
										"status": "CURRENT",
										"media": map[string]any{
											"id":     102,
											"format": "TV",
											"status": "FINISHED",
											"title":  map[string]any{"romaji": "Anime 2"},
										},
									},
								},
							},
						},
					},
				},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := anilist.NewClient(server.URL)
	entries, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username:          "testuser",
		IncludeUnreleased: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if chunkRequests.Load() != 2 {
		t.Fatalf("expected 2 chunk requests, got %d", chunkRequests.Load())
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries across chunks, got %d", len(entries))
	}
	if entries[0].Media.ID != 101 || entries[1].Media.ID != 102 {
		t.Errorf("unexpected entry IDs: %d, %d", entries[0].Media.ID, entries[1].Media.ID)
	}
}

func TestFetchWatchlist_EnforcesRateLimiting(t *testing.T) {
	var mu sync.Mutex
	requestTimes := make([]time.Time, 0, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestTimes = append(requestTimes, time.Now())
		mu.Unlock()

		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		vars := req["variables"].(map[string]any)

		chunk := 1
		if c, ok := vars["chunk"].(float64); ok {
			chunk = int(c)
		}

		resp := map[string]any{
			"data": map[string]any{
				"MediaListCollection": map[string]any{
					"hasNextChunk": chunk < 2,
					"lists": []map[string]any{
						{
							"name":   "Watching",
							"status": "CURRENT",
							"entries": []map[string]any{
								{
									"id":     chunk,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     chunk,
										"format": "TV",
										"status": "FINISHED",
										"title":  map[string]any{"romaji": fmt.Sprintf("Anime %d", chunk)},
									},
								},
							},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 600 requests per minute = 10 requests per second = 100ms interval
	client := anilist.NewClient(server.URL, anilist.WithRateLimit(600))
	start := time.Now()
	_, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username:          "testuser",
		IncludeUnreleased: true,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	times := make([]time.Time, len(requestTimes))
	copy(times, requestTimes)
	mu.Unlock()

	if len(times) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(times))
	}

	gap := times[1].Sub(times[0])
	if gap < 80*time.Millisecond {
		t.Errorf("expected at least 80ms gap between requests, got %v (total elapsed: %v)", gap, elapsed)
	}
}

func TestFetchWatchlist_RateLimitContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"MediaListCollection":{"hasNextChunk":true,"lists":[]}}}`))
	}))
	defer server.Close()

	// Low rate limit: 1 request per minute (60s interval)
	client := anilist.NewClient(server.URL, anilist.WithRateLimit(1))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.FetchWatchlist(ctx, anilist.WatchlistFilter{
		Username:          "testuser",
		IncludeUnreleased: true,
	})
	if err == nil {
		t.Fatal("expected error due to context cancellation, got nil")
	}
}

func TestFetchWatchlist_HandlesHTTP429WithRetryAfterSeconds(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		att := attempts.Add(1)
		if att == 1 {
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Too Many Requests"}]}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"MediaListCollection": map[string]any{
					"hasNextChunk": false,
					"lists": []map[string]any{
						{
							"name":   "Watching",
							"status": "CURRENT",
							"entries": []map[string]any{
								{
									"id":     999,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     999,
										"format": "MOVIE",
										"status": "FINISHED",
										"title":  map[string]any{"romaji": "Movie After Retry"},
									},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	// Use fast rate limit and fast backoff unit for tests
	client := anilist.NewClient(
		server.URL,
		anilist.WithRateLimit(60000), // no rate limiter wait
		anilist.WithBackoffBase(10*time.Millisecond),
	)

	start := time.Now()
	entries, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username:          "testuser",
		IncludeUnreleased: true,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts.Load())
	}
	if len(entries) != 1 || entries[0].Media.ID != 999 {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	// Retry-After was 1 second
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected at least 900ms wait for Retry-After: 1, got %v", elapsed)
	}
}

func TestFetchWatchlist_HandlesHTTP429WithRetryAfterDate(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		att := attempts.Add(1)
		if att == 1 {
			// Set Retry-After to 2 seconds in the future formatted as RFC1123 (integer second resolution)
			targetDate := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
			w.Header().Set("Retry-After", targetDate)
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Too Many Requests"}]}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"MediaListCollection": map[string]any{
					"hasNextChunk": false,
					"lists": []map[string]any{
						{
							"name":   "Watching",
							"status": "CURRENT",
							"entries": []map[string]any{
								{
									"id":     888,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     888,
										"format": "TV",
										"status": "FINISHED",
										"title":  map[string]any{"romaji": "Anime After Date Retry"},
									},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := anilist.NewClient(
		server.URL,
		anilist.WithRateLimit(60000),
	)

	start := time.Now()
	entries, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username:          "testuser",
		IncludeUnreleased: true,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts.Load())
	}
	if len(entries) != 1 || entries[0].Media.ID != 888 {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if elapsed < 800*time.Millisecond {
		t.Errorf("expected at least 800ms wait for date Retry-After, got %v", elapsed)
	}
}

func TestFetchWatchlist_HandlesHTTP429ExponentialBackoffWhenHeaderMissing(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		att := attempts.Add(1)
		if att < 3 {
			// No Retry-After header
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"MediaListCollection": map[string]any{
					"hasNextChunk": false,
					"lists":        []map[string]any{},
				},
			},
		})
	}))
	defer server.Close()

	client := anilist.NewClient(
		server.URL,
		anilist.WithRateLimit(60000),
		anilist.WithBackoffBase(30*time.Millisecond),
	)

	start := time.Now()
	_, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username:          "testuser",
		IncludeUnreleased: true,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}
	// attempt 0: 30ms, attempt 1: 60ms => total >= 90ms
	if elapsed < 80*time.Millisecond {
		t.Errorf("expected exponential backoff delay >= 80ms, got %v", elapsed)
	}
}

func TestFetchWatchlist_HTTP429ExhaustsMaxRetries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := anilist.NewClient(
		server.URL,
		anilist.WithRateLimit(60000),
		anilist.WithBackoffBase(5*time.Millisecond),
		anilist.WithMaxRetries(3),
	)

	_, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username:          "testuser",
		IncludeUnreleased: true,
	})
	if err == nil {
		t.Fatal("expected error when 429 retries exhausted, got nil")
	}
	// initial + 3 retries = 4 attempts
	if attempts.Load() != 4 {
		t.Errorf("expected 4 attempts (1 initial + 3 retries), got %d", attempts.Load())
	}
}

func TestFetchWatchlist_DeduplicatesAcrossCustomLists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"MediaListCollection": map[string]any{
					"hasNextChunk": false,
					"lists": []map[string]any{
						{
							"name":   "Watching",
							"status": "CURRENT",
							"entries": []map[string]any{
								{
									"id":     1,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     42,
										"format": "TV",
										"status": "RELEASING",
										"title":  map[string]any{"romaji": "Duplicate Anime"},
									},
								},
							},
						},
						{
							"name":   "Favorites",
							"status": "CURRENT",
							"entries": []map[string]any{
								{
									"id":     2,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     42, // same media ID in custom list
										"format": "TV",
										"status": "RELEASING",
										"title":  map[string]any{"romaji": "Duplicate Anime"},
									},
								},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := anilist.NewClient(server.URL, anilist.WithRateLimit(60000))
	entries, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username: "testuser",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 deduplicated entry, got %d", len(entries))
	}
	if entries[0].Media.ID != 42 {
		t.Errorf("expected media ID 42, got %d", entries[0].Media.ID)
	}
}

func TestFetchWatchlist_NullIDMalPreserved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"MediaListCollection": map[string]any{
					"hasNextChunk": false,
					"lists": []map[string]any{
						{
							"name":   "Watching",
							"status": "CURRENT",
							"entries": []map[string]any{
								{
									"id":     1,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     101,
										"idMal":  nil, // explicitly null
										"format": "TV",
										"status": "FINISHED",
										"title":  map[string]any{"romaji": "Anime Without MAL"},
									},
								},
								{
									"id":     2,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     102,
										"idMal":  54321,
										"format": "TV",
										"status": "FINISHED",
										"title":  map[string]any{"romaji": "Anime With MAL"},
									},
								},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := anilist.NewClient(server.URL, anilist.WithRateLimit(60000))
	entries, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username: "testuser",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Media.IDMal != nil {
		t.Errorf("expected nil IDMal for entry 0, got %d", *entries[0].Media.IDMal)
	}
	if entries[1].Media.IDMal == nil || *entries[1].Media.IDMal != 54321 {
		t.Errorf("expected IDMal 54321 for entry 1, got %v", entries[1].Media.IDMal)
	}
}

func TestFetchWatchlist_MultipleGraphQLErrorsJoined(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"errors": []map[string]any{
				{"message": "User not found"},
				{"message": "Rate limit warning"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := anilist.NewClient(server.URL, anilist.WithRateLimit(60000))
	_, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username: "missinguser",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "User not found") || !strings.Contains(errMsg, "Rate limit warning") {
		t.Errorf("expected combined error message to contain both errors, got %q", errMsg)
	}
}

func TestFetchWatchlist_NullMediaListCollectionReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"MediaListCollection": nil,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := anilist.NewClient(server.URL, anilist.WithRateLimit(60000))
	_, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username: "testuser",
	})
	if err == nil {
		t.Fatal("expected error when MediaListCollection is null, got nil")
	}
	if !strings.Contains(err.Error(), "media list collection not found or inaccessible") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFetchWatchlist_QueriesCoverImageAndMapsCoverImagePayloads(t *testing.T) {
	var mu sync.Mutex
	var capturedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		mu.Lock()
		if q, ok := body["query"].(string); ok {
			capturedQuery = q
		}
		mu.Unlock()

		resp := map[string]any{
			"data": map[string]any{
				"MediaListCollection": map[string]any{
					"hasNextChunk": false,
					"lists": []map[string]any{
						{
							"name":   "Watching",
							"status": "CURRENT",
							"entries": []map[string]any{
								{
									"id":     1,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     101,
										"format": "TV",
										"status": "FINISHED",
										"title":  map[string]any{"romaji": "Anime With Full Cover"},
										"coverImage": map[string]any{
											"extraLarge": "https://img.anilist.co/media/101/extralarge.jpg",
											"large":      "https://img.anilist.co/media/101/large.jpg",
											"medium":     "https://img.anilist.co/media/101/medium.jpg",
											"color":      "#e4a15d",
										},
									},
								},
								{
									"id":     2,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     102,
										"format": "TV",
										"status": "FINISHED",
										"title":  map[string]any{"romaji": "Anime With Missing Images But Valid Color"},
										"coverImage": map[string]any{
											"extraLarge": nil,
											"large":      nil,
											"medium":     nil,
											"color":      "#43a047",
										},
									},
								},
								{
									"id":     3,
									"status": "CURRENT",
									"media": map[string]any{
										"id":     103,
										"format": "TV",
										"status": "FINISHED",
										"title":  map[string]any{"romaji": "Anime With Null Color Payload"},
										"coverImage": map[string]any{
											"extraLarge": nil,
											"large":      nil,
											"medium":     nil,
											"color":      nil,
										},
									},
								},
							},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := anilist.NewClient(server.URL, anilist.WithRateLimit(60000))
	entries, err := client.FetchWatchlist(context.Background(), anilist.WatchlistFilter{
		Username: "testuser",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	query := capturedQuery
	mu.Unlock()

	// Verify coverImage and fields are requested inside query
	requiredSubstrings := []string{
		"coverImage",
		"extraLarge",
		"large",
		"medium",
		"color",
	}
	for _, sub := range requiredSubstrings {
		if !strings.Contains(query, sub) {
			t.Errorf("expected query to contain %q, but query was:\n%s", sub, query)
		}
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Entry 1: valid image URLs present
	e1 := entries[0]
	if e1.Media.CoverImage.ExtraLarge != "https://img.anilist.co/media/101/extralarge.jpg" {
		t.Errorf("expected ExtraLarge URL, got %q", e1.Media.CoverImage.ExtraLarge)
	}
	if e1.Media.CoverImage.Color != "#e4a15d" {
		t.Errorf("expected Color #e4a15d, got %q", e1.Media.CoverImage.Color)
	}
	if gotURL := e1.Media.CoverImageURL(); gotURL != "https://img.anilist.co/media/101/extralarge.jpg" {
		t.Errorf("expected e1 CoverImageURL %q, got %q", "https://img.anilist.co/media/101/extralarge.jpg", gotURL)
	}

	// Entry 2: missing images with valid color
	e2 := entries[1]
	if e2.Media.CoverImage.Color != "#43a047" {
		t.Errorf("expected Color #43a047, got %q", e2.Media.CoverImage.Color)
	}
	if gotURL := e2.Media.CoverImageURL(); gotURL != "https://dummyimage.com/400x600/43a047/43a047.png" {
		t.Errorf("expected e2 CoverImageURL %q, got %q", "https://dummyimage.com/400x600/43a047/43a047.png", gotURL)
	}

	// Entry 3: null color payload
	e3 := entries[2]
	if e3.Media.CoverImage.Color != "" {
		t.Errorf("expected empty Color for null payload, got %q", e3.Media.CoverImage.Color)
	}
	if gotURL := e3.Media.CoverImageURL(); gotURL != "https://dummyimage.com/400x600/2b2d42/2b2d42.png" {
		t.Errorf("expected e3 CoverImageURL default fallback %q, got %q", "https://dummyimage.com/400x600/2b2d42/2b2d42.png", gotURL)
	}
}
