package orchestrator_test

import (
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

func TestFindSubSeasonEpisodeSlice(t *testing.T) {
	episodes := []servarr.Episode{
		{
			ID:            101,
			SeasonNumber:  1,
			EpisodeNumber: 1,
			AirDate:       "2023-01-08",
		},
		{
			ID:            102,
			SeasonNumber:  1,
			EpisodeNumber: 12,
			AirDate:       "2023-03-26",
			FinaleType:    "midseason",
		},
		{
			ID:            103,
			SeasonNumber:  1,
			EpisodeNumber: 13,
			AirDate:       "2023-07-09",
		},
		{
			ID:            104,
			SeasonNumber:  1,
			EpisodeNumber: 24,
			AirDate:       "2023-09-24",
			FinaleType:    "season",
		},
	}

	t.Run("returns false when season has no midseason finale", func(t *testing.T) {
		singlePartEps := []servarr.Episode{
			{ID: 1, SeasonNumber: 1, EpisodeNumber: 1},
			{ID: 2, SeasonNumber: 1, EpisodeNumber: 12, FinaleType: "season"},
		}

		slice, found := orchestrator.FindSubSeasonEpisodeSlice(singlePartEps, 1, 2, time.Time{}, 0)
		if found {
			t.Fatalf("expected found=false for single part season, got slice: %v", slice)
		}
	})

	t.Run("selects part 1 by sub-season number", func(t *testing.T) {
		slice, found := orchestrator.FindSubSeasonEpisodeSlice(episodes, 1, 1, time.Time{}, 0)
		if !found {
			t.Fatal("expected found=true")
		}
		if len(slice) != 2 || slice[0] != 101 || slice[1] != 102 {
			t.Fatalf("expected [101, 102], got %v", slice)
		}
	})

	t.Run("selects part 2 by sub-season number", func(t *testing.T) {
		slice, found := orchestrator.FindSubSeasonEpisodeSlice(episodes, 1, 2, time.Time{}, 0)
		if !found {
			t.Fatal("expected found=true")
		}
		if len(slice) != 2 || slice[0] != 103 || slice[1] != 104 {
			t.Fatalf("expected [103, 104], got %v", slice)
		}
	})

	t.Run("selects part 2 by air date match", func(t *testing.T) {
		startDate := time.Date(2023, 7, 10, 0, 0, 0, 0, time.UTC)
		tolerance := 14 * 24 * time.Hour

		slice, found := orchestrator.FindSubSeasonEpisodeSlice(episodes, 1, 0, startDate, tolerance)
		if !found {
			t.Fatal("expected found=true")
		}
		if len(slice) != 2 || slice[0] != 103 || slice[1] != 104 {
			t.Fatalf("expected [103, 104], got %v", slice)
		}
	})

	t.Run("selects part 1 by air date match", func(t *testing.T) {
		startDate := time.Date(2023, 1, 7, 0, 0, 0, 0, time.UTC)
		tolerance := 14 * 24 * time.Hour

		slice, found := orchestrator.FindSubSeasonEpisodeSlice(episodes, 1, 0, startDate, tolerance)
		if !found {
			t.Fatal("expected found=true")
		}
		if len(slice) != 2 || slice[0] != 101 || slice[1] != 102 {
			t.Fatalf("expected [101, 102], got %v", slice)
		}
	})
}
