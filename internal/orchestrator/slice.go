package orchestrator

import (
	"sort"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

// FindSubSeasonEpisodeSlice partitions the episodes of targetSeason into sub-season slices
// based on Sonarr's midseason finale indicators. If the season is split into multiple parts,
// it selects the slice corresponding to subSeasonNumber (1-indexed) or matching startDate
// within tolerance.
// Returns the episode IDs in the matching slice and true if a split-cour slice was identified.
func FindSubSeasonEpisodeSlice(
	episodes []servarr.Episode,
	targetSeason int,
	subSeasonNumber int,
	startDate time.Time,
	tolerance time.Duration,
) ([]int, bool) {
	var seasonEps []servarr.Episode
	for _, ep := range episodes {
		if ep.SeasonNumber == targetSeason {
			seasonEps = append(seasonEps, ep)
		}
	}
	if len(seasonEps) == 0 {
		return nil, false
	}

	sort.Slice(seasonEps, func(i, j int) bool {
		return seasonEps[i].EpisodeNumber < seasonEps[j].EpisodeNumber
	})

	var slices [][]servarr.Episode
	var currentSlice []servarr.Episode

	for _, ep := range seasonEps {
		currentSlice = append(currentSlice, ep)
		if strings.EqualFold(strings.TrimSpace(ep.FinaleType), "midseason") {
			slices = append(slices, currentSlice)
			currentSlice = nil
		}
	}
	if len(currentSlice) > 0 {
		slices = append(slices, currentSlice)
	}

	// If there is only one slice, this season has no midseason split
	if len(slices) <= 1 {
		return nil, false
	}

	// Match by explicit sub-season number (e.g., Part 2 -> slice 2)
	if subSeasonNumber > 0 {
		idx := subSeasonNumber - 1
		if idx >= 0 && idx < len(slices) {
			ids := make([]int, len(slices[idx]))
			for i, ep := range slices[idx] {
				ids[i] = ep.ID
			}
			return ids, true
		}
		return nil, false
	}

	// Match by air date against slice premieres
	if !startDate.IsZero() {
		if tolerance <= 0 {
			tolerance = 14 * 24 * time.Hour
		}

		for _, sl := range slices {
			var premiere time.Time
			for _, ep := range sl {
				if d, ok := ep.PremiereDate(); ok && !d.IsZero() {
					premiere = d
					break
				}
			}
			if premiere.IsZero() {
				continue
			}

			diff := startDate.Sub(premiere)
			if diff < 0 {
				diff = -diff
			}
			if diff <= tolerance {
				ids := make([]int, len(sl))
				for i, ep := range sl {
					ids[i] = ep.ID
				}
				return ids, true
			}
		}
	}

	return nil, false
}
