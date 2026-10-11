package notification

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCoverPlaceholderURL = "https://dummyimage.com/400x600/2b2d42/2b2d42.png"

	colorSuccess = 0x2ecc71 // Emerald green
	colorWarning = 0xf39c12 // Amber orange
	colorError   = 0xe74c3c // Crimson red
	colorInfo    = 0x3498db // Sky blue
	colorUpdate  = 0x9b59b6 // Amethyst purple
)

var hexColorRegex = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

// ResolvePosterURL resolves poster artwork URL, falling back to a solid color
// placeholder image if artwork is missing.
func ResolvePosterURL(posterURL, color string) string {
	if strings.TrimSpace(posterURL) != "" {
		return strings.TrimSpace(posterURL)
	}

	sanitizedColor := strings.TrimPrefix(strings.TrimSpace(color), "#")
	if hexColorRegex.MatchString(sanitizedColor) {
		return fmt.Sprintf("https://dummyimage.com/400x600/%s/%s.png", sanitizedColor, sanitizedColor)
	}

	return defaultCoverPlaceholderURL
}

// DiscordPayload represents an outbound Discord webhook JSON payload.
type DiscordPayload struct {
	Username  string         `json:"username,omitempty"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Embeds    []DiscordEmbed `json:"embeds,omitempty"`
}

// DiscordEmbed represents a single Discord embed card.
type DiscordEmbed struct {
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Color       int               `json:"color,omitempty"`
	Thumbnail   *DiscordThumbnail `json:"thumbnail,omitempty"`
	Fields      []DiscordField    `json:"fields,omitempty"`
	Timestamp   string            `json:"timestamp,omitempty"`
	Footer      *DiscordFooter    `json:"footer,omitempty"`
}

// DiscordThumbnail defines the thumbnail image for an embed.
type DiscordThumbnail struct {
	URL string `json:"url"`
}

// DiscordField represents a name-value metadata pair inside an embed.
type DiscordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// DiscordFooter defines footer text for an embed.
type DiscordFooter struct {
	Text string `json:"text"`
}

// BuildDiscordPayload transforms a SyncEvent into a rich Discord webhook payload.
func BuildDiscordPayload(event SyncEvent) (DiscordPayload, error) {
	var embed DiscordEmbed

	if !event.Timestamp.IsZero() {
		embed.Timestamp = event.Timestamp.UTC().Format(time.RFC3339)
	} else {
		embed.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	embed.Footer = &DiscordFooter{Text: "AniList Arr Sync"}

	switch event.Type {
	case EventMediaAdded:
		embed.Title = fmt.Sprintf("Media Added: %s", event.Title)
		embed.Color = colorSuccess
		embed.Thumbnail = &DiscordThumbnail{
			URL: ResolvePosterURL(event.PosterURL, ""),
		}

		if event.TargetService != "" {
			embed.Fields = append(embed.Fields, DiscordField{
				Name:   "Service",
				Value:  event.TargetService,
				Inline: true,
			})
		}
		if event.MediaType != "" {
			embed.Fields = append(embed.Fields, DiscordField{
				Name:   "Type",
				Value:  event.MediaType,
				Inline: true,
			})
		}
		if event.AniListID > 0 {
			embed.Fields = append(embed.Fields, DiscordField{
				Name:   "AniList ID",
				Value:  strconv.Itoa(event.AniListID),
				Inline: true,
			})
		}
		if event.Season != nil {
			embed.Fields = append(embed.Fields, DiscordField{
				Name:   "Season",
				Value:  strconv.Itoa(*event.Season),
				Inline: true,
			})
		}
		if len(event.Episodes) > 0 {
			epList := make([]string, len(event.Episodes))
			for i, ep := range event.Episodes {
				epList[i] = strconv.Itoa(ep)
			}
			embed.Fields = append(embed.Fields, DiscordField{
				Name:   "Episodes",
				Value:  strings.Join(epList, ", "),
				Inline: true,
			})
		}

	case EventReviewRequired:
		embed.Title = fmt.Sprintf("Review Required: %s", event.Title)
		embed.Color = colorWarning
		embed.Thumbnail = &DiscordThumbnail{
			URL: ResolvePosterURL(event.PosterURL, ""),
		}
		if event.Reason != "" {
			embed.Fields = append(embed.Fields, DiscordField{
				Name:   "Reason",
				Value:  event.Reason,
				Inline: true,
			})
		}
		if event.MediaType != "" {
			embed.Fields = append(embed.Fields, DiscordField{
				Name:   "Type",
				Value:  event.MediaType,
				Inline: true,
			})
		}
		if event.AniListID > 0 {
			embed.Fields = append(embed.Fields, DiscordField{
				Name:   "AniList ID",
				Value:  strconv.Itoa(event.AniListID),
				Inline: true,
			})
		}

	case EventSyncError:
		embed.Title = "Sync Error"
		embed.Color = colorError
		if event.Error != "" {
			embed.Description = event.Error
		} else {
			embed.Description = "An error occurred during synchronization."
		}

	case EventSyncComplete:
		embed.Title = "Sync Complete"
		embed.Color = colorInfo
		embed.Fields = append(embed.Fields,
			DiscordField{
				Name:   "Duration",
				Value:  fmt.Sprintf("%dms", event.DurationMs),
				Inline: true,
			},
			DiscordField{
				Name:   "Items Scanned",
				Value:  strconv.Itoa(event.ItemsScanned),
				Inline: true,
			},
			DiscordField{
				Name:   "Added Radarr",
				Value:  strconv.Itoa(event.AddedRadarr),
				Inline: true,
			},
			DiscordField{
				Name:   "Monitored Sonarr",
				Value:  strconv.Itoa(event.MonitoredSonarr),
				Inline: true,
			},
			DiscordField{
				Name:   "Queued Review",
				Value:  strconv.Itoa(event.QueuedReview),
				Inline: true,
			},
		)

	case EventUpdateAvailable:
		embed.Title = fmt.Sprintf("Update Available: %s", event.NewVersion)
		embed.Color = colorUpdate
		if event.CurrentVersion != "" {
			embed.Fields = append(embed.Fields, DiscordField{
				Name:   "Current Version",
				Value:  event.CurrentVersion,
				Inline: true,
			})
		}
		if event.NewVersion != "" {
			embed.Fields = append(embed.Fields, DiscordField{
				Name:   "New Version",
				Value:  event.NewVersion,
				Inline: true,
			})
		}
		if event.ChangelogURL != "" {
			embed.Fields = append(embed.Fields, DiscordField{
				Name:   "Changelog",
				Value:  event.ChangelogURL,
				Inline: false,
			})
		}

	default:
		embed.Title = string(event.Type)
		embed.Color = colorInfo
		if event.Title != "" {
			embed.Description = event.Title
		}
	}

	return DiscordPayload{
		Username: "AniList Arr Sync",
		Embeds:   []DiscordEmbed{embed},
	}, nil
}
