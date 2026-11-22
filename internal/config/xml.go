package config

import (
	"encoding/xml"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

type xmlQualityProfileOverride struct {
	Country string `xml:"country,attr"`
	Value   string `xml:",chardata"`
}

// MarshalXML serializes CountryQualityProfiles as sorted <Override country="XX">ID</Override> elements.
func (m CountryQualityProfiles) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	if err := e.EncodeToken(start); err != nil {
		return err
	}
	for _, k := range keys {
		elem := xml.StartElement{
			Name: xml.Name{Local: "Override"},
			Attr: []xml.Attr{{Name: xml.Name{Local: "country"}, Value: k}},
		}
		if err := e.EncodeElement(m[k], elem); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

// UnmarshalXML parses <Override country="XX">ID</Override> elements into CountryQualityProfiles.
func (m *CountryQualityProfiles) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	result := make(CountryQualityProfiles)
	for {
		tok, err := d.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "Override" {
				if err := d.Skip(); err != nil {
					return err
				}
				continue
			}
			var item xmlQualityProfileOverride
			if err := d.DecodeElement(&item, &t); err != nil {
				return err
			}
			country := strings.ToUpper(strings.TrimSpace(item.Country))
			if _, exists := result[country]; exists {
				return fmt.Errorf("duplicate quality profile override for country %q", country)
			}
			id, err := strconv.Atoi(strings.TrimSpace(item.Value))
			if err != nil {
				return fmt.Errorf("invalid quality profile ID %q for country %q: %w", strings.TrimSpace(item.Value), country, err)
			}
			result[country] = id
		case xml.EndElement:
			if t.Name == start.Name {
				if len(result) == 0 {
					*m = nil
				} else {
					*m = result
				}
				return nil
			}
		}
	}
	if len(result) == 0 {
		*m = nil
	} else {
		*m = result
	}
	return nil
}

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
	WizardCompleted            bool   `xml:"WizardCompleted"` // Onboarding uses lowercase XML booleans.
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

	RadarrURL                     string                 `xml:"RadarrUrl"`
	RadarrAPIKey                  string                 `xml:"RadarrApiKey"`
	RadarrQualityProfileID        int                    `xml:"RadarrQualityProfileId"`
	RadarrQualityProfileOverrides CountryQualityProfiles `xml:"RadarrQualityProfileOverrides,omitempty"`
	RadarrRootFolderPath          string                 `xml:"RadarrRootFolderPath"`
	RadarrMonitorOnAdd            string                 `xml:"RadarrMonitorOnAdd"`
	RadarrSearchOnAdd             string                 `xml:"RadarrSearchOnAdd"`

	SonarrURL                     string                 `xml:"SonarrUrl"`
	SonarrAPIKey                  string                 `xml:"SonarrApiKey"`
	SonarrQualityProfileID        int                    `xml:"SonarrQualityProfileId"`
	SonarrQualityProfileOverrides CountryQualityProfiles `xml:"SonarrQualityProfileOverrides,omitempty"`
	SonarrRootFolderPath          string                 `xml:"SonarrRootFolderPath"`
	SonarrSeriesType              string                 `xml:"SonarrSeriesType"`
	SonarrMonitorOnAdd            string                 `xml:"SonarrMonitorOnAdd"`
	SonarrSearchOnAdd             string                 `xml:"SonarrSearchOnAdd"`

	TagName             string  `xml:"TagName"`
	SyncIntervalHours   int     `xml:"SyncIntervalHours"`
	DateToleranceDays   int     `xml:"DateToleranceDays"`
	ConfidenceThreshold float64 `xml:"ConfidenceThreshold"`
	UnmonitorDropped    string  `xml:"UnmonitorDropped"`
	UnmonitorCompleted  string  `xml:"UnmonitorCompleted"`
	DeleteDropped       string  `xml:"DeleteDropped"`
	DeleteCompleted     string  `xml:"DeleteCompleted"`
	UnmonitorWatched    string  `xml:"UnmonitorWatched"`
	DeleteWatched       string  `xml:"DeleteWatched"`
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
		BindAddress:                   c.BindAddress,
		Port:                          c.Port,
		URLBase:                       c.URLBase,
		AllowedHosts:                  c.AllowedHosts,
		ApplicationURL:                c.ApplicationURL,
		EnableSsl:                     formatBool(c.EnableSsl),
		SslPort:                       c.SslPort,
		SslCertPath:                   c.SslCertPath,
		SslKeyPath:                    c.SslKeyPath,
		AuthenticationMethod:          c.AuthenticationMethod,
		AuthenticationRequired:        c.AuthenticationRequired,
		APIKey:                        c.APIKey,
		TrustedNetworks:               c.TrustedNetworks,
		UseProxy:                      formatBool(c.UseProxy),
		ProxyType:                     c.ProxyType,
		ProxyHostname:                 c.ProxyHostname,
		ProxyPort:                     c.ProxyPort,
		ProxyUsername:                 c.ProxyUsername,
		ProxyPassword:                 c.ProxyPassword,
		ProxyIgnoredAddresses:         c.ProxyIgnoredAddresses,
		ProxyBypassLocalAddresses:     formatBool(c.ProxyBypassLocalAddresses),
		LogLevel:                      c.LogLevel,
		ConsoleLogLevel:               c.ConsoleLogLevel,
		LogSizeLimit:                  c.LogSizeLimit,
		LogRotate:                     c.LogRotate,
		InstanceName:                  c.InstanceName,
		WizardCompleted:               c.WizardCompleted,
		LaunchBrowser:                 formatBool(c.LaunchBrowser),
		EnableDesktopNotifications:    formatBool(c.EnableDesktopNotifications),
		UpdateBranch:                  c.UpdateBranch,
		UpdateAutomatically:           formatBool(c.UpdateAutomatically),
		UpdateMechanism:               c.UpdateMechanism,
		BackupFolder:                  c.BackupFolder,
		BackupInterval:                c.BackupInterval,
		BackupRetention:               c.BackupRetention,
		DatabasePath:                  c.DatabasePath,
		AniListUsername:               c.AniListUsername,
		AniListTvStatus:               c.AniListTvStatus,
		AniListMovieStatus:            c.AniListMovieStatus,
		AniListIncludeUnreleased:      formatBool(c.AniListIncludeUnreleased),
		RadarrURL:                     c.RadarrURL,
		RadarrAPIKey:                  c.RadarrAPIKey,
		RadarrQualityProfileID:        c.RadarrQualityProfileID,
		RadarrQualityProfileOverrides: c.RadarrQualityProfileOverrides,
		RadarrRootFolderPath:          c.RadarrRootFolderPath,
		RadarrMonitorOnAdd:            formatBool(c.RadarrMonitorOnAdd),
		RadarrSearchOnAdd:             formatBool(c.RadarrSearchOnAdd),
		SonarrURL:                     c.SonarrURL,
		SonarrAPIKey:                  c.SonarrAPIKey,
		SonarrQualityProfileID:        c.SonarrQualityProfileID,
		SonarrQualityProfileOverrides: c.SonarrQualityProfileOverrides,
		SonarrRootFolderPath:          c.SonarrRootFolderPath,
		SonarrSeriesType:              c.SonarrSeriesType,
		SonarrMonitorOnAdd:            formatBool(c.SonarrMonitorOnAdd),
		SonarrSearchOnAdd:             formatBool(c.SonarrSearchOnAdd),
		TagName:                       c.TagName,
		SyncIntervalHours:             c.SyncIntervalHours,
		DateToleranceDays:             c.DateToleranceDays,
		ConfidenceThreshold:           c.ConfidenceThreshold,
		UnmonitorDropped:              formatBool(c.UnmonitorDropped),
		UnmonitorCompleted:            formatBool(c.UnmonitorCompleted),
		DeleteDropped:                 formatBool(c.DeleteDropped),
		DeleteCompleted:               formatBool(c.DeleteCompleted),
		UnmonitorWatched:              formatBool(c.UnmonitorWatched),
		DeleteWatched:                 formatBool(c.DeleteWatched),
		FirstRunDryRun:                formatBool(c.FirstRunDryRun),
	}

	data, err := xml.MarshalIndent(xc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize config XML: %w", err)
	}

	// Add trailing newline for POSIX cleanliness
	data = append(data, '\n')
	return data, nil
}
