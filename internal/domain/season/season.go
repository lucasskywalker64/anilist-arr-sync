// Package season provides air-date matching and season regex parsing to determine
// target Sonarr season numbers for AniList media entries.
package season

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultAirDateTolerance represents the default air-date tolerance window (14 days).
const DefaultAirDateTolerance = 14 * 24 * time.Hour

// ErrAmbiguousAirDate indicates multiple seasons matched within the air-date tolerance window.
var ErrAmbiguousAirDate = errors.New("multiple seasons match air date within tolerance")

// MatchMethod represents the strategy that produced a season match.
type MatchMethod string

const (
	// MatchMethodNone indicates no match was found by air date or regex.
	MatchMethodNone MatchMethod = "NONE"
	// MatchMethodAirDate indicates the season was matched solely by episode air date.
	MatchMethodAirDate MatchMethod = "AIR_DATE"
	// MatchMethodRegex indicates the season was matched solely by title regular expressions.
	MatchMethodRegex MatchMethod = "REGEX"
	// MatchMethodBoth indicates both air date and title regex agreed on the season.
	MatchMethodBoth MatchMethod = "BOTH"
	// MatchMethodConflict indicates a conflict occurred between strategies or multiple candidates.
	MatchMethodConflict MatchMethod = "CONFLICT"
)

// MatchResult encapsulates the outcome of season matching.
type MatchResult struct {
	Season    int
	MatchedBy MatchMethod
	Conflict  bool
	Reason    string
}

// Episode represents a Sonarr episode air date record.
type Episode struct {
	SeasonNumber  int
	EpisodeNumber int
	AirDate       time.Time
	FinaleType    string
}

var seasonPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bseason\s+(\d+)\b`),
	regexp.MustCompile(`(?i)\b(\d+)(?:st|nd|rd|th)\s+season\b`),
	regexp.MustCompile(`(?i)\bpart\s+(\d+)\b`),
	regexp.MustCompile(`(?i)\bcour\s+(\d+)\b`),
}

// ParseSeasonNumber attempts to extract a season number from one or more candidate titles.
// Returns the extracted season number and true if found, or 0 and false if no pattern matches
// or if candidate titles contain conflicting season numbers.
func ParseSeasonNumber(titles ...string) (int, bool) {
	var foundSeason int
	for _, title := range titles {
		clean := strings.TrimSpace(title)
		if clean == "" {
			continue
		}
		for _, re := range seasonPatterns {
			matches := re.FindStringSubmatch(clean)
			if len(matches) >= 2 {
				val, err := strconv.Atoi(matches[1])
				if err == nil && val > 0 {
					if foundSeason != 0 && foundSeason != val {
						return 0, false
					}
					foundSeason = val
					break
				}
			}
		}
	}
	if foundSeason > 0 {
		return foundSeason, true
	}
	return 0, false
}

// MatchSeasonByAirDate matches an AniList start date against Sonarr episode air dates
// within a specified tolerance window. It inspects each season's premiere dates,
// which include the season premiere (earliest aired episode) and any arc or sub-season premiere
// (the first aired episode following an episode with a midseason finale marker).
// Returns the matched season number, a boolean indicating whether a match was found,
// and an error if multiple seasons match within the tolerance window.
func MatchSeasonByAirDate(startDate time.Time, episodes []Episode, tolerance time.Duration) (int, bool, error) {
	if startDate.IsZero() || len(episodes) == 0 {
		return 0, false, nil
	}
	if tolerance <= 0 {
		tolerance = DefaultAirDateTolerance
	}

	bySeason := make(map[int][]Episode)
	for _, ep := range episodes {
		if ep.SeasonNumber <= 0 || ep.AirDate.IsZero() {
			continue
		}
		bySeason[ep.SeasonNumber] = append(bySeason[ep.SeasonNumber], ep)
	}

	seasonCandidates := make(map[int][]time.Time)
	for seasonNum, eps := range bySeason {
		sort.Slice(eps, func(i, j int) bool {
			return eps[i].EpisodeNumber < eps[j].EpisodeNumber
		})

		var earliest time.Time
		for _, ep := range eps {
			if earliest.IsZero() || ep.AirDate.Before(earliest) {
				earliest = ep.AirDate
			}
		}
		if !earliest.IsZero() {
			seasonCandidates[seasonNum] = append(seasonCandidates[seasonNum], earliest)
		}

		for i, ep := range eps {
			if strings.EqualFold(strings.TrimSpace(ep.FinaleType), "midseason") {
				for j := i + 1; j < len(eps); j++ {
					if !eps[j].AirDate.IsZero() {
						seasonCandidates[seasonNum] = append(seasonCandidates[seasonNum], eps[j].AirDate)
						break
					}
				}
			}
		}
	}

	var matchedSeasons []int
	for seasonNum, candidateDates := range seasonCandidates {
		matched := false
		for _, candidate := range candidateDates {
			diff := startDate.Sub(candidate)
			if diff < 0 {
				diff = -diff
			}
			if diff <= tolerance {
				matched = true
				break
			}
		}
		if matched {
			matchedSeasons = append(matchedSeasons, seasonNum)
		}
	}

	if len(matchedSeasons) == 0 {
		return 0, false, nil
	}
	if len(matchedSeasons) > 1 {
		return 0, false, ErrAmbiguousAirDate
	}
	return matchedSeasons[0], true, nil
}

// MatchSeason evaluates AniList start date and candidate titles against Sonarr episode air dates
// to determine the target season number.
// If air date matching and title regex disagree or if multiple seasons match the air date window,
// a conflict is flagged.
func MatchSeason(startDate time.Time, titles []string, episodes []Episode, tolerance time.Duration) MatchResult {
	dateSeason, dateFound, dateErr := MatchSeasonByAirDate(startDate, episodes, tolerance)
	regexSeason, regexFound := ParseSeasonNumber(titles...)

	if dateErr != nil {
		return MatchResult{
			Season:    0,
			MatchedBy: MatchMethodConflict,
			Conflict:  true,
			Reason:    dateErr.Error(),
		}
	}

	if dateFound && regexFound {
		if dateSeason == regexSeason {
			return MatchResult{
				Season:    dateSeason,
				MatchedBy: MatchMethodBoth,
				Conflict:  false,
				Reason:    fmt.Sprintf("both air date and title regex matched season %d", dateSeason),
			}
		}
		return MatchResult{
			Season:    0,
			MatchedBy: MatchMethodConflict,
			Conflict:  true,
			Reason:    fmt.Sprintf("air date matched season %d but title regex matched season %d", dateSeason, regexSeason),
		}
	}

	if dateFound {
		return MatchResult{
			Season:    dateSeason,
			MatchedBy: MatchMethodAirDate,
			Conflict:  false,
			Reason:    fmt.Sprintf("air date matched season %d within tolerance", dateSeason),
		}
	}

	if regexFound {
		return MatchResult{
			Season:    regexSeason,
			MatchedBy: MatchMethodRegex,
			Conflict:  false,
			Reason:    fmt.Sprintf("title regex matched season %d", regexSeason),
		}
	}

	return MatchResult{
		Season:    0,
		MatchedBy: MatchMethodNone,
		Conflict:  false,
		Reason:    "no season matched by air date or title regex",
	}
}
