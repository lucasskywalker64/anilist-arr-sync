package router_test

import (
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/domain/router"
)

func TestRoute_MovieRoutesToRadarr(t *testing.T) {
	got := router.Route(router.FormatMovie)
	want := router.RouteRadarr

	if got != want {
		t.Fatalf("Route(FormatMovie) = %v, want %v", got, want)
	}
}

func TestRoute_SeriesFormatsRouteToSonarr(t *testing.T) {
	tests := []struct {
		name   string
		format router.MediaFormat
	}{
		{name: "TV", format: router.FormatTV},
		{name: "TV_SHORT", format: router.FormatTVShort},
		{name: "ONA", format: router.FormatONA},
		{name: "OVA", format: router.FormatOVA},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := router.Route(tt.format)
			want := router.RouteSonarr

			if got != want {
				t.Fatalf("Route(%v) = %v, want %v", tt.format, got, want)
			}
		})
	}
}

func TestRoute_MusicFormatDiscards(t *testing.T) {
	got := router.Route(router.FormatMusic)
	want := router.RouteDiscard

	if got != want {
		t.Fatalf("Route(FormatMusic) = %v, want %v", got, want)
	}
}

func TestRoute_SpecialFormat(t *testing.T) {
	got := router.Route(router.FormatSpecial)
	want := router.RouteReviewQueue

	if got != want {
		t.Fatalf("Route(FormatSpecial) = %v, want %v", got, want)
	}
}

func TestRoute_AllVariantsTableDriven(t *testing.T) {
	tests := []struct {
		name   string
		format router.MediaFormat
		want   router.RouteDecision
	}{
		// Radarr route
		{name: "MOVIE", format: router.FormatMovie, want: router.RouteRadarr},
		{name: "lowercase movie", format: "movie", want: router.RouteRadarr},
		{name: "whitespace movie", format: " MOVIE ", want: router.RouteRadarr},

		// Sonarr route
		{name: "TV", format: router.FormatTV, want: router.RouteSonarr},
		{name: "TV_SHORT", format: router.FormatTVShort, want: router.RouteSonarr},
		{name: "ONA", format: router.FormatONA, want: router.RouteSonarr},
		{name: "OVA", format: router.FormatOVA, want: router.RouteSonarr},
		{name: "lowercase tv", format: "tv", want: router.RouteSonarr},
		{name: "lowercase ova", format: "ova", want: router.RouteSonarr},

		// Special route
		{name: "SPECIAL", format: router.FormatSpecial, want: router.RouteReviewQueue},
		{name: "lowercase special", format: "special", want: router.RouteReviewQueue},

		// Discard route: Music
		{name: "MUSIC", format: router.FormatMusic, want: router.RouteDiscard},
		{name: "lowercase music", format: "music", want: router.RouteDiscard},

		// Discard route: Non-video AniList formats
		{name: "MANGA", format: router.FormatManga, want: router.RouteDiscard},
		{name: "NOVEL", format: router.FormatNovel, want: router.RouteDiscard},
		{name: "ONE_SHOT", format: router.FormatOneShot, want: router.RouteDiscard},

		// Discard route: Unknown or invalid formats
		{name: "unknown format string", format: "PODCAST", want: router.RouteDiscard},
		{name: "empty string", format: "", want: router.RouteDiscard},
		{name: "whitespace only", format: "   ", want: router.RouteDiscard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := router.Route(tt.format)
			if got != tt.want {
				t.Fatalf("Route(%q) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestRouteDecision_String(t *testing.T) {
	tests := []struct {
		decision router.RouteDecision
		want     string
	}{
		{decision: router.RouteRadarr, want: "RADARR"},
		{decision: router.RouteSonarr, want: "SONARR"},
		{decision: router.RouteDiscard, want: "DISCARD"},
		{decision: router.RouteReviewQueue, want: "REVIEW_QUEUE"},
		{decision: router.RouteDecision(999), want: "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.decision.String(); got != tt.want {
				t.Fatalf("RouteDecision(%d).String() = %q, want %q", tt.decision, got, tt.want)
			}
		})
	}
}

func TestRouteDecision_TargetService(t *testing.T) {
	tests := []struct {
		decision router.RouteDecision
		want     string
	}{
		{decision: router.RouteRadarr, want: "RADARR"},
		{decision: router.RouteSonarr, want: "SONARR"},
		{decision: router.RouteDiscard, want: ""},
		{decision: router.RouteReviewQueue, want: ""},
		{decision: router.RouteDecision(999), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.decision.String(), func(t *testing.T) {
			if got := tt.decision.TargetService(); got != tt.want {
				t.Fatalf("RouteDecision(%d).TargetService() = %q, want %q", tt.decision, got, tt.want)
			}
		})
	}
}
