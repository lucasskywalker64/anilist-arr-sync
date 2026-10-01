package season_test

import (
	"testing"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/domain/season"
)

func TestTitleRegexFallback(t *testing.T) {
	tests := []struct {
		name       string
		titles     []string
		wantSeason int
		wantFound    bool
		wantConflict bool
	}{
		{
			name:       "season with number",
			titles:     []string{"Attack on Titan Season 2"},
			wantSeason: 2,
			wantFound:  true,
		},
		{
			name:       "ordinal nd season",
			titles:     []string{"Mob Psycho 100 2nd Season"},
			wantSeason: 2,
			wantFound:  true,
		},
		{
			name:       "ordinal rd season",
			titles:     []string{"Kaguya-sama 3rd Season"},
			wantSeason: 3,
			wantFound:  true,
		},
		{
			name:       "ordinal th season",
			titles:     []string{"Overlord 4th Season"},
			wantSeason: 4,
			wantFound:  true,
		},
		{
			name:       "ordinal st season",
			titles:     []string{"Show 1st Season"},
			wantSeason: 1,
			wantFound:  true,
		},
		{
			name:       "part with number",
			titles:     []string{"Spy x Family Part 2"},
			wantSeason: 2,
			wantFound:  true,
		},
		{
			name:       "cour with number",
			titles:     []string{"Bleach: Thousand-Year Blood War Cour 2"},
			wantSeason: 2,
			wantFound:  true,
		},
		{
			name:       "case insensitivity",
			titles:     []string{"my hero academia season 5"},
			wantSeason: 5,
			wantFound:  true,
		},
		{
			name:       "title without season marker",
			titles:     []string{"Jujutsu Kaisen"},
			wantSeason: 0,
			wantFound:  false,
		},
		{
			name:       "word boundary guard for party",
			titles:     []string{"Party People Monster"},
			wantSeason: 0,
			wantFound:  false,
		},
		{
			name:       "word boundary guard for courage",
			titles:     []string{"Courage the Cowardly Dog"},
			wantSeason: 0,
			wantFound:  false,
		},
		{
			name:       "fallback across multiple titles",
			titles:     []string{"Shingeki no Kyojin", "Attack on Titan Season 3"},
			wantSeason: 3,
			wantFound:  true,
		},
		{
			name:       "multiple titles agreeing on season",
			titles:     []string{"Attack on Titan Season 2", "Shingeki no Kyojin 2nd Season"},
			wantSeason: 2,
			wantFound:  true,
		},
		{
			name:         "multiple titles conflicting on season",
			titles:       []string{"Attack on Titan Season 2", "Attack on Titan Season 3"},
			wantSeason:   0,
			wantFound:    false,
			wantConflict: true,
		},
		{
			name:         "single title conflicting strong markers same pattern",
			titles:       []string{"Season 1 vs Season 2"},
			wantSeason:   0,
			wantFound:    false,
			wantConflict: true,
		},
		{
			name:         "single title conflicting strong markers different patterns",
			titles:       []string{"Season 2 ... 3rd Season"},
			wantSeason:   0,
			wantFound:    false,
			wantConflict: true,
		},
		{
			name:       "single title agreeing strong markers repeated",
			titles:     []string{"Attack on Titan Season 2 (Season 2)"},
			wantSeason: 2,
			wantFound:  true,
		},
		{
			name:       "single title agreeing strong markers different patterns",
			titles:     []string{"Show 2nd Season - Season 2"},
			wantSeason: 2,
			wantFound:  true,
		},
		{
			name:         "single title conflicting weak markers same pattern",
			titles:       []string{"Show Part 1 ... Part 2"},
			wantSeason:   0,
			wantFound:    false,
			wantConflict: true,
		},
		{
			name:         "single title conflicting weak markers different patterns",
			titles:       []string{"Show Part 1 ... Cour 2"},
			wantSeason:   0,
			wantFound:    false,
			wantConflict: true,
		},
		{
			name:       "single title agreeing weak markers repeated",
			titles:     []string{"Show Part 2 ... Part 2"},
			wantSeason: 2,
			wantFound:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := season.MatchSeason(time.Time{}, tt.titles, nil, 0)
			if tt.wantConflict {
				if !res.Conflict {
					t.Errorf("MatchSeason(%v) conflict = false, want true", tt.titles)
				}
				if res.Season != 0 {
					t.Errorf("MatchSeason(%v) season = %d, want 0", tt.titles, res.Season)
				}
				if res.MatchedBy != season.MatchMethodConflict {
					t.Errorf("MatchSeason(%v) matchedBy = %s, want %s", tt.titles, res.MatchedBy, season.MatchMethodConflict)
				}
				return
			}
			if res.Conflict {
				t.Errorf("MatchSeason(%v) conflict = true, want false (reason: %s)", tt.titles, res.Reason)
			}
			if tt.wantFound {
				if res.Season != tt.wantSeason {
					t.Errorf("MatchSeason(%v) season = %d, want %d", tt.titles, res.Season, tt.wantSeason)
				}
				if res.MatchedBy != season.MatchMethodRegex {
					t.Errorf("MatchSeason(%v) matchedBy = %s, want %s", tt.titles, res.MatchedBy, season.MatchMethodRegex)
				}
			} else {
				if res.Season != 0 {
					t.Errorf("MatchSeason(%v) season = %d, want 0", tt.titles, res.Season)
				}
				if res.MatchedBy != season.MatchMethodNone {
					t.Errorf("MatchSeason(%v) matchedBy = %s, want %s", tt.titles, res.MatchedBy, season.MatchMethodNone)
				}
			}
		})
	}
}

func TestMatchSeasonByAirDate(t *testing.T) {
	jst := time.FixedZone("JST", 9*3600)
	est := time.FixedZone("EST", -5*3600)

	baseDate := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		startDate   time.Time
		episodes    []season.Episode
		tolerance   time.Duration
		wantSeason  int
		wantFound   bool
		wantErrDiff bool
	}{
		{
			name:      "exact premiere date match",
			startDate: baseDate,
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: baseDate},
				{SeasonNumber: 1, EpisodeNumber: 2, AirDate: baseDate.AddDate(0, 0, 7)},
			},
			tolerance:  14 * 24 * time.Hour,
			wantSeason: 1,
			wantFound:  true,
		},
		{
			name:      "within 14-day tolerance window",
			startDate: baseDate,
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)},
				{SeasonNumber: 2, EpisodeNumber: 1, AirDate: baseDate.AddDate(0, 0, 5)},
			},
			tolerance:  14 * 24 * time.Hour,
			wantSeason: 2,
			wantFound:  true,
		},
		{
			name:      "exact boundary tolerance 14 days",
			startDate: baseDate,
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: baseDate.AddDate(0, 0, 14)},
			},
			tolerance:  14 * 24 * time.Hour,
			wantSeason: 1,
			wantFound:  true,
		},
		{
			name:      "outside tolerance 15 days",
			startDate: baseDate,
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: baseDate.AddDate(0, 0, 15)},
			},
			tolerance:  14 * 24 * time.Hour,
			wantSeason: 0,
			wantFound:  false,
		},
		{
			name:      "timezone differences JST and EST",
			startDate: time.Date(2024, 4, 1, 23, 0, 0, 0, jst),
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: time.Date(2024, 4, 1, 10, 0, 0, 0, est)},
			},
			tolerance:  24 * time.Hour,
			wantSeason: 1,
			wantFound:  true,
		},
		{
			name:      "zero start date returns no match",
			startDate: time.Time{},
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: baseDate},
			},
			tolerance:  14 * 24 * time.Hour,
			wantSeason: 0,
			wantFound:  false,
		},
		{
			name:       "empty episodes slice",
			startDate:  baseDate,
			episodes:   nil,
			tolerance:  14 * 24 * time.Hour,
			wantSeason: 0,
			wantFound:  false,
		},
		{
			name:      "ignores season 0 specials even if air date matches",
			startDate: baseDate,
			episodes: []season.Episode{
				{SeasonNumber: 0, EpisodeNumber: 1, AirDate: baseDate},
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: baseDate.AddDate(0, 0, 30)},
			},
			tolerance:  14 * 24 * time.Hour,
			wantSeason: 0,
			wantFound:  false,
		},
		{
			name:      "out of order episodes finds true premiere date",
			startDate: baseDate,
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 2, AirDate: baseDate.AddDate(0, 0, 7)},
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: baseDate},
			},
			tolerance:  14 * 24 * time.Hour,
			wantSeason: 1,
			wantFound:  true,
		},
		{
			name:      "ambiguous multiple seasons matching tolerance",
			startDate: baseDate,
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: baseDate.AddDate(0, 0, -2)},
				{SeasonNumber: 2, EpisodeNumber: 1, AirDate: baseDate.AddDate(0, 0, 3)},
			},
			tolerance:   14 * 24 * time.Hour,
			wantSeason:  0,
			wantFound:   false,
			wantErrDiff: true,
		},
		{
			name:      "arc premiere following midseason finale matches season",
			startDate: time.Date(2024, 10, 12, 0, 0, 0, 0, time.UTC),
			episodes: []season.Episode{
				{SeasonNumber: 4, EpisodeNumber: 1, AirDate: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)},
				{SeasonNumber: 4, EpisodeNumber: 24, AirDate: time.Date(2024, 6, 20, 0, 0, 0, 0, time.UTC), FinaleType: "midseason"},
				{SeasonNumber: 4, EpisodeNumber: 25, AirDate: time.Date(2024, 10, 10, 0, 0, 0, 0, time.UTC)},
				{SeasonNumber: 4, EpisodeNumber: 37, AirDate: time.Date(2024, 12, 26, 0, 0, 0, 0, time.UTC), FinaleType: "series"},
			},
			tolerance:  14 * 24 * time.Hour,
			wantSeason: 4,
			wantFound:  true,
		},
		{
			name:      "arc premiere following midseason finale outside tolerance",
			startDate: time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC),
			episodes: []season.Episode{
				{SeasonNumber: 4, EpisodeNumber: 1, AirDate: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)},
				{SeasonNumber: 4, EpisodeNumber: 24, AirDate: time.Date(2024, 6, 20, 0, 0, 0, 0, time.UTC), FinaleType: "midseason"},
				{SeasonNumber: 4, EpisodeNumber: 25, AirDate: time.Date(2024, 10, 10, 0, 0, 0, 0, time.UTC)},
			},
			tolerance:  14 * 24 * time.Hour,
			wantSeason: 0,
			wantFound:  false,
		},
		{
			name:      "arc premiere with unsorted episodes and case-insensitive finale type",
			startDate: time.Date(2024, 10, 11, 0, 0, 0, 0, time.UTC),
			episodes: []season.Episode{
				{SeasonNumber: 4, EpisodeNumber: 25, AirDate: time.Date(2024, 10, 10, 0, 0, 0, 0, time.UTC)},
				{SeasonNumber: 4, EpisodeNumber: 1, AirDate: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)},
				{SeasonNumber: 4, EpisodeNumber: 24, AirDate: time.Date(2024, 6, 20, 0, 0, 0, 0, time.UTC), FinaleType: "MidSeason"},
			},
			tolerance:  14 * 24 * time.Hour,
			wantSeason: 4,
			wantFound:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSeason, gotFound, err := season.MatchSeasonByAirDate(tt.startDate, tt.episodes, tt.tolerance)
			if tt.wantErrDiff {
				if err == nil {
					t.Fatalf("MatchSeasonByAirDate() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("MatchSeasonByAirDate() unexpected error: %v", err)
			}
			if gotFound != tt.wantFound {
				t.Fatalf("MatchSeasonByAirDate() found = %v, want %v", gotFound, tt.wantFound)
			}
			if gotSeason != tt.wantSeason {
				t.Errorf("MatchSeasonByAirDate() season = %d, want %d", gotSeason, tt.wantSeason)
			}
		})
	}
}

func TestMatchSeason(t *testing.T) {
	baseDate := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		startDate    time.Time
		titles       []string
		episodes     []season.Episode
		tolerance    time.Duration
		wantSeason   int
		wantMethod   season.MatchMethod
		wantConflict bool
	}{
		{
			name:      "both air date and regex match same season",
			startDate: baseDate,
			titles:    []string{"Attack on Titan Season 2"},
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)},
				{SeasonNumber: 2, EpisodeNumber: 1, AirDate: baseDate.AddDate(0, 0, 2)},
			},
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   2,
			wantMethod:   season.MatchMethodBoth,
			wantConflict: false,
		},
		{
			name:      "only air date matches without regex in titles",
			startDate: baseDate,
			titles:    []string{"Jujutsu Kaisen", "Sorcery Fight"},
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: baseDate},
			},
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   1,
			wantMethod:   season.MatchMethodAirDate,
			wantConflict: false,
		},
		{
			name:         "only regex matches when air dates are missing",
			startDate:    time.Time{},
			titles:       []string{"Mob Psycho 100 2nd Season"},
			episodes:     nil,
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   2,
			wantMethod:   season.MatchMethodRegex,
			wantConflict: false,
		},
		{
			name:      "only regex matches when episode air dates are outside tolerance window",
			startDate: baseDate,
			titles:    []string{"Bleach: Thousand-Year Blood War Cour 2"},
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: baseDate.AddDate(0, -6, 0)},
			},
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   2,
			wantMethod:   season.MatchMethodRegex,
			wantConflict: false,
		},
		{
			name:      "conflict when air date and regex disagree",
			startDate: baseDate,
			titles:    []string{"My Hero Academia Season 4"},
			episodes: []season.Episode{
				{SeasonNumber: 3, EpisodeNumber: 1, AirDate: baseDate},
			},
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   0,
			wantMethod:   season.MatchMethodConflict,
			wantConflict: true,
		},
		{
			name:      "conflict when multiple seasons match air date window",
			startDate: baseDate,
			titles:    []string{"Some Anime Series"},
			episodes: []season.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, AirDate: baseDate.AddDate(0, 0, -1)},
				{SeasonNumber: 2, EpisodeNumber: 1, AirDate: baseDate.AddDate(0, 0, 2)},
			},
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   0,
			wantMethod:   season.MatchMethodConflict,
			wantConflict: true,
		},
		{
			name:         "no match when neither air date nor regex matches",
			startDate:    time.Time{},
			titles:       []string{"Standalone Anime"},
			episodes:     nil,
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   0,
			wantMethod:   season.MatchMethodNone,
			wantConflict: false,
		},
		{
			name:      "diverse titles - Attack on Titan Season 3 Part 2",
			startDate: baseDate,
			titles:    []string{"Shingeki no Kyojin Season 3 Part 2", "Attack on Titan Season 3 Part 2"},
			episodes: []season.Episode{
				{SeasonNumber: 3, EpisodeNumber: 1, AirDate: baseDate},
			},
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   3,
			wantMethod:   season.MatchMethodBoth,
			wantConflict: false,
		},
		{
			name:         "diverse titles - SPY x FAMILY Part 2 fallback",
			startDate:    time.Time{},
			titles:       []string{"SPY×FAMILY Part 2"},
			episodes:     nil,
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   2,
			wantMethod:   season.MatchMethodRegex,
			wantConflict: false,
		},
		{
			name:      "sub-season Part marker does not conflict with clean air date match",
			startDate: time.Date(2023, 10, 12, 0, 0, 0, 0, time.UTC),
			titles:    []string{"Dr. STONE: NEW WORLD Part 2"},
			episodes: []season.Episode{
				{SeasonNumber: 3, EpisodeNumber: 1, AirDate: time.Date(2023, 4, 6, 0, 0, 0, 0, time.UTC)},
				{SeasonNumber: 3, EpisodeNumber: 11, AirDate: time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC), FinaleType: "midseason"},
				{SeasonNumber: 3, EpisodeNumber: 12, AirDate: time.Date(2023, 10, 12, 0, 0, 0, 0, time.UTC)},
				{SeasonNumber: 3, EpisodeNumber: 22, AirDate: time.Date(2023, 12, 21, 0, 0, 0, 0, time.UTC), FinaleType: "season"},
			},
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   3,
			wantMethod:   season.MatchMethodAirDate,
			wantConflict: false,
		},
		{
			name:      "conflict when titles contain conflicting season markers even if air date matches",
			startDate: baseDate,
			titles:    []string{"Attack on Titan Season 2", "Attack on Titan Season 3"},
			episodes: []season.Episode{
				{SeasonNumber: 2, EpisodeNumber: 1, AirDate: baseDate},
			},
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   0,
			wantMethod:   season.MatchMethodConflict,
			wantConflict: true,
		},
		{
			name:         "conflict when titles contain conflicting season markers without air dates",
			startDate:    time.Time{},
			titles:       []string{"Attack on Titan Season 2", "Attack on Titan Season 3"},
			episodes:     nil,
			tolerance:    14 * 24 * time.Hour,
			wantSeason:   0,
			wantMethod:   season.MatchMethodConflict,
			wantConflict: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := season.MatchSeason(tt.startDate, tt.titles, tt.episodes, tt.tolerance)
			if result.Conflict != tt.wantConflict {
				t.Errorf("MatchSeason() conflict = %v, want %v (reason: %s)", result.Conflict, tt.wantConflict, result.Reason)
			}
			if result.MatchedBy != tt.wantMethod {
				t.Errorf("MatchSeason() matchedBy = %s, want %s (reason: %s)", result.MatchedBy, tt.wantMethod, result.Reason)
			}
			if result.Season != tt.wantSeason {
				t.Errorf("MatchSeason() season = %d, want %d (reason: %s)", result.Season, tt.wantSeason, result.Reason)
			}
		})
	}
}
