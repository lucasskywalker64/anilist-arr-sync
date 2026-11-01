package config

import (
	"encoding/xml"
	"fmt"
)

// xmlConfig is a helper struct for XML marshaling to ensure Servarr PascalCase booleans and exact element order.
type xmlConfig struct {
	XMLName xml.Name `xml:"Config"`

	BindAddress    string `xml:"BindAddress"`
	Port           int    `xml:"Port"`
	URLBase        string `xml:"UrlBase"`
	AllowedHosts   string `xml:"AllowedHosts"`
	ApplicationURL string `xml:"ApplicationUrl"`
	EnableSsl      string `xml:"EnableSsl"`
	SslPort        int    `xml:"SslPort"`
	SslCertPath    string `xml:"SslCertPath"`
	SslKeyPath     string `xml:"SslKeyPath"`

	AuthenticationMethod   string `xml:"AuthenticationMethod"`
	AuthenticationRequired string `xml:"AuthenticationRequired"`
	APIKey                 string `xml:"ApiKey"`
	TrustedNetworks        string `xml:"TrustedNetworks"`

	UseProxy                  string `xml:"UseProxy"`
	ProxyType                 string `xml:"ProxyType"`
	ProxyHostname             string `xml:"ProxyHostname"`
	ProxyPort                 int    `xml:"ProxyPort"`
	ProxyUsername             string `xml:"ProxyUsername"`
	ProxyPassword             string `xml:"ProxyPassword"`
	ProxyIgnoredAddresses     string `xml:"ProxyIgnoredAddresses"`
	ProxyBypassLocalAddresses string `xml:"ProxyBypassLocalAddresses"`

	LogLevel        string `xml:"LogLevel"`
	ConsoleLogLevel string `xml:"ConsoleLogLevel"`
	LogSizeLimit    int    `xml:"LogSizeLimit"`
	LogRotate       int    `xml:"LogRotate"`

	InstanceName               string `xml:"InstanceName"`
	LaunchBrowser              string `xml:"LaunchBrowser"`
	EnableDesktopNotifications string `xml:"EnableDesktopNotifications"`

	UpdateBranch        string `xml:"UpdateBranch"`
	UpdateAutomatically string `xml:"UpdateAutomatically"`
	UpdateMechanism     string `xml:"UpdateMechanism"`

	BackupFolder    string `xml:"BackupFolder"`
	BackupInterval  int    `xml:"BackupInterval"`
	BackupRetention int    `xml:"BackupRetention"`

	DatabasePath string `xml:"DatabasePath"`

	AniListUsername          string `xml:"AniListUsername"`
	AniListTvStatus          string `xml:"AniListTvStatus"`
	AniListMovieStatus       string `xml:"AniListMovieStatus"`
	AniListIncludeUnreleased string `xml:"AniListIncludeUnreleased"`

	RadarrURL              string `xml:"RadarrUrl"`
	RadarrAPIKey           string `xml:"RadarrApiKey"`
	RadarrQualityProfileID int    `xml:"RadarrQualityProfileId"`
	RadarrRootFolderPath   string `xml:"RadarrRootFolderPath"`
	RadarrSearchOnAdd      string `xml:"RadarrSearchOnAdd"`

	SonarrURL              string `xml:"SonarrUrl"`
	SonarrAPIKey           string `xml:"SonarrApiKey"`
	SonarrQualityProfileID int    `xml:"SonarrQualityProfileId"`
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

// formatBool converts a boolean value to Servarr PascalCase representation ("True" or "False").
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
		URLBase:                    c.URLBase,
		AllowedHosts:               c.AllowedHosts,
		ApplicationURL:             c.ApplicationURL,
		EnableSsl:                  formatBool(c.EnableSsl),
		SslPort:                    c.SslPort,
		SslCertPath:                c.SslCertPath,
		SslKeyPath:                 c.SslKeyPath,
		AuthenticationMethod:       c.AuthenticationMethod,
		AuthenticationRequired:     c.AuthenticationRequired,
		APIKey:                     c.APIKey,
		TrustedNetworks:            c.TrustedNetworks,
		UseProxy:                   formatBool(c.UseProxy),
		ProxyType:                  c.ProxyType,
		ProxyHostname:              c.ProxyHostname,
		ProxyPort:                  c.ProxyPort,
		ProxyUsername:              c.ProxyUsername,
		ProxyPassword:              c.ProxyPassword,
		ProxyIgnoredAddresses:      c.ProxyIgnoredAddresses,
		ProxyBypassLocalAddresses:  formatBool(c.ProxyBypassLocalAddresses),
		LogLevel:                   c.LogLevel,
		ConsoleLogLevel:            c.ConsoleLogLevel,
		LogSizeLimit:               c.LogSizeLimit,
		LogRotate:                  c.LogRotate,
		InstanceName:               c.InstanceName,
		LaunchBrowser:              formatBool(c.LaunchBrowser),
		EnableDesktopNotifications: formatBool(c.EnableDesktopNotifications),
		UpdateBranch:               c.UpdateBranch,
		UpdateAutomatically:        formatBool(c.UpdateAutomatically),
		UpdateMechanism:            c.UpdateMechanism,
		BackupFolder:               c.BackupFolder,
		BackupInterval:             c.BackupInterval,
		BackupRetention:            c.BackupRetention,
		DatabasePath:               c.DatabasePath,
		AniListUsername:            c.AniListUsername,
		AniListTvStatus:            c.AniListTvStatus,
		AniListMovieStatus:         c.AniListMovieStatus,
		AniListIncludeUnreleased:   formatBool(c.AniListIncludeUnreleased),
		RadarrURL:                  c.RadarrURL,
		RadarrAPIKey:               c.RadarrAPIKey,
		RadarrQualityProfileID:     c.RadarrQualityProfileID,
		RadarrRootFolderPath:       c.RadarrRootFolderPath,
		RadarrSearchOnAdd:          formatBool(c.RadarrSearchOnAdd),
		SonarrURL:                  c.SonarrURL,
		SonarrAPIKey:               c.SonarrAPIKey,
		SonarrQualityProfileID:     c.SonarrQualityProfileID,
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
