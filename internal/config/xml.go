package config

import (
	"encoding/xml"
	"fmt"
)

// xmlConfig is a helper struct for XML marshaling to ensure Servarr PascalCase booleans and exact element order.
type xmlConfig struct {
	XMLName xml.Name `xml:"Config"`

	BindAddress string `xml:"BindAddress"`
	Port        int    `xml:"Port"`
	UrlBase     string `xml:"UrlBase"`
	EnableSsl   string `xml:"EnableSsl"`
	SslPort     int    `xml:"SslPort"`
	SslCertPath string `xml:"SslCertPath"`
	SslKeyPath  string `xml:"SslKeyPath"`

	AuthenticationMethod   string `xml:"AuthenticationMethod"`
	AuthenticationRequired string `xml:"AuthenticationRequired"`
	ApiKey                 string `xml:"ApiKey"`

	LogLevel        string `xml:"LogLevel"`
	ConsoleLogLevel string `xml:"ConsoleLogLevel"`
	LogSizeLimit    int    `xml:"LogSizeLimit"`
	LogRotate       int    `xml:"LogRotate"`

	InstanceName               string `xml:"InstanceName"`
	LaunchBrowser              string `xml:"LaunchBrowser"`
	EnableDesktopNotifications string `xml:"EnableDesktopNotifications"`

	UpdateAutomatically string `xml:"UpdateAutomatically"`
	UpdateMechanism     string `xml:"UpdateMechanism"`

	DatabasePath string `xml:"DatabasePath"`

	AniListUsername          string `xml:"AniListUsername"`
	AniListTvStatus          string `xml:"AniListTvStatus"`
	AniListMovieStatus       string `xml:"AniListMovieStatus"`
	AniListIncludeUnreleased string `xml:"AniListIncludeUnreleased"`

	RadarrUrl              string `xml:"RadarrUrl"`
	RadarrApiKey           string `xml:"RadarrApiKey"`
	RadarrQualityProfileId int    `xml:"RadarrQualityProfileId"`
	RadarrRootFolderPath   string `xml:"RadarrRootFolderPath"`
	RadarrSearchOnAdd      string `xml:"RadarrSearchOnAdd"`

	SonarrUrl              string `xml:"SonarrUrl"`
	SonarrApiKey           string `xml:"SonarrApiKey"`
	SonarrQualityProfileId int    `xml:"SonarrQualityProfileId"`
	SonarrRootFolderPath   string `xml:"SonarrRootFolderPath"`
	SonarrSeriesType       string `xml:"SonarrSeriesType"`
	SonarrSearchOnAdd      string `xml:"SonarrSearchOnAdd"`

	TagName             string  `xml:"TagName"`
	SyncIntervalHours   int     `xml:"SyncIntervalHours"`
	DateToleranceDays   int     `xml:"DateToleranceDays"`
	ConfidenceThreshold float64 `xml:"ConfidenceThreshold"`
	UnmonitorDropped    string  `xml:"UnmonitorDropped"`
	FirstRunDryRun      string  `xml:"FirstRunDryRun"`
}

func formatBool(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

// ParseXML unmarshals XML data onto a default Config instance.
// Any element missing from the XML retains its documented default value.
func ParseXML(data []byte) (*Config, error) {
	cfg := NewDefault()

	// Unmarshal directly onto cfg so default values remain for omitted elements.
	// encoding/xml handles standard int, string, float64, and bool (case-insensitively).
	if err := xml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config XML: %w", err)
	}

	return cfg, nil
}

// ToXML serializes the Config into formatted Servarr-compliant XML.
func (c *Config) ToXML() ([]byte, error) {
	xc := xmlConfig{
		BindAddress:                c.BindAddress,
		Port:                       c.Port,
		UrlBase:                    c.UrlBase,
		EnableSsl:                  formatBool(c.EnableSsl),
		SslPort:                    c.SslPort,
		SslCertPath:                c.SslCertPath,
		SslKeyPath:                 c.SslKeyPath,
		AuthenticationMethod:       c.AuthenticationMethod,
		AuthenticationRequired:     c.AuthenticationRequired,
		ApiKey:                     c.ApiKey,
		LogLevel:                   c.LogLevel,
		ConsoleLogLevel:            c.ConsoleLogLevel,
		LogSizeLimit:               c.LogSizeLimit,
		LogRotate:                  c.LogRotate,
		InstanceName:               c.InstanceName,
		LaunchBrowser:              formatBool(c.LaunchBrowser),
		EnableDesktopNotifications: formatBool(c.EnableDesktopNotifications),
		UpdateAutomatically:        formatBool(c.UpdateAutomatically),
		UpdateMechanism:            c.UpdateMechanism,
		DatabasePath:               c.DatabasePath,
		AniListUsername:            c.AniListUsername,
		AniListTvStatus:            c.AniListTvStatus,
		AniListMovieStatus:         c.AniListMovieStatus,
		AniListIncludeUnreleased:   formatBool(c.AniListIncludeUnreleased),
		RadarrUrl:                  c.RadarrUrl,
		RadarrApiKey:               c.RadarrApiKey,
		RadarrQualityProfileId:     c.RadarrQualityProfileId,
		RadarrRootFolderPath:       c.RadarrRootFolderPath,
		RadarrSearchOnAdd:          formatBool(c.RadarrSearchOnAdd),
		SonarrUrl:                  c.SonarrUrl,
		SonarrApiKey:               c.SonarrApiKey,
		SonarrQualityProfileId:     c.SonarrQualityProfileId,
		SonarrRootFolderPath:       c.SonarrRootFolderPath,
		SonarrSeriesType:           c.SonarrSeriesType,
		SonarrSearchOnAdd:          formatBool(c.SonarrSearchOnAdd),
		TagName:                    c.TagName,
		SyncIntervalHours:          c.SyncIntervalHours,
		DateToleranceDays:          c.DateToleranceDays,
		ConfidenceThreshold:        c.ConfidenceThreshold,
		UnmonitorDropped:           formatBool(c.UnmonitorDropped),
		FirstRunDryRun:             formatBool(c.FirstRunDryRun),
	}

	data, err := xml.MarshalIndent(xc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize config XML: %w", err)
	}

	// Add trailing newline for POSIX cleanliness
	data = append(data, '\n')
	return data, nil
}
