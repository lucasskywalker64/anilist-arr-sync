// Package router categorizes AniList media format attributes and determines
// the appropriate downstream synchronization target (Radarr, Sonarr, Review Queue, or Discard).
package router

import "strings"

// MediaFormat represents an AniList media format attribute.
type MediaFormat string

const (
	// FormatMovie represents feature films routed to Radarr.
	FormatMovie MediaFormat = "MOVIE"
	// FormatTV represents standard television series routed to Sonarr.
	FormatTV MediaFormat = "TV"
	// FormatTVShort represents short-form television series routed to Sonarr.
	FormatTVShort MediaFormat = "TV_SHORT"
	// FormatONA represents original net animations routed to Sonarr.
	FormatONA MediaFormat = "ONA"
	// FormatOVA represents original video animations routed to Sonarr.
	FormatOVA MediaFormat = "OVA"
	// FormatSpecial represents special episodes requiring review.
	FormatSpecial MediaFormat = "SPECIAL"

	// FormatMusic represents music video releases discarded from processing.
	FormatMusic MediaFormat = "MUSIC"
	// FormatManga represents print manga releases discarded from processing.
	FormatManga MediaFormat = "MANGA"
	// FormatNovel represents light novel releases discarded from processing.
	FormatNovel MediaFormat = "NOVEL"
	// FormatOneShot represents one-shot print releases discarded from processing.
	FormatOneShot MediaFormat = "ONE_SHOT"
)

// RouteDecision represents the target synchronization path or action.
type RouteDecision int

const (
	// RouteRadarr directs media to Radarr for movie management.
	RouteRadarr RouteDecision = iota
	// RouteSonarr directs media to Sonarr for series management.
	RouteSonarr
	// RouteDiscard drops media with zero downstream processing.
	RouteDiscard
	// RouteReviewQueue directs ambiguous media to the review queue.
	RouteReviewQueue
)

// String returns the canonical uppercase representation of the route decision.
func (d RouteDecision) String() string {
	switch d {
	case RouteRadarr:
		return "RADARR"
	case RouteSonarr:
		return "SONARR"
	case RouteDiscard:
		return "DISCARD"
	case RouteReviewQueue:
		return "REVIEW_QUEUE"
	default:
		return "UNKNOWN"
	}
}

// TargetService returns the target Servarr service name, or empty string if not applicable.
func (d RouteDecision) TargetService() string {
	switch d {
	case RouteRadarr:
		return "RADARR"
	case RouteSonarr:
		return "SONARR"
	default:
		return ""
	}
}

// Route determines the synchronization path for an AniList media entry.
//
// Rules:
// 1. MOVIE routes exclusively to Radarr.
// 2. TV, TV_SHORT, ONA, and OVA route to Sonarr.
// 3. SPECIAL directs to the review queue for manual review.
// 4. MUSIC and non-video/unknown formats are discarded with zero downstream processing.
func Route(format MediaFormat) RouteDecision {
	normalized := MediaFormat(strings.ToUpper(strings.TrimSpace(string(format))))

	switch normalized {
	case FormatMovie:
		return RouteRadarr
	case FormatTV, FormatTVShort, FormatONA, FormatOVA:
		return RouteSonarr
	case FormatSpecial:
		return RouteReviewQueue
	case FormatMusic, FormatManga, FormatNovel, FormatOneShot:
		return RouteDiscard
	default:
		return RouteDiscard
	}
}
