package config

import (
	"fmt"
	"strconv"
	"strings"
)

const envPrefix = "ANILIST_SYNC__"

// ApplyEnv parses environment variables matching ANILIST_SYNC__[NAMESPACE]__[KEY]
// and applies them over the receiver Config fields.
func (c *Config) ApplyEnv(environ []string) error {
	for _, entry := range environ {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}

		upperKey := strings.ToUpper(strings.TrimSpace(key))
		if !strings.HasPrefix(upperKey, envPrefix) {
			continue
		}

		subKey := strings.TrimPrefix(upperKey, envPrefix)
		val := strings.TrimSpace(value)

		if err := c.applyField(key, subKey, val); err != nil {
			return err
		}
	}

	return nil
}

func (c *Config) applyField(origKey, subKey, val string) error {
	parseInt := func() (int, error) {
		n, err := strconv.Atoi(val)
		if err != nil {
			return 0, fmt.Errorf("invalid integer value %q for %s: %w", val, origKey, err)
		}
		return n, nil
	}

	parseBool := func() (bool, error) {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return false, fmt.Errorf("invalid boolean value %q for %s: %w", val, origKey, err)
		}
		return b, nil
	}

	parseFloat := func() (float64, error) {
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid float value %q for %s: %w", val, origKey, err)
		}
		return f, nil
	}

	switch subKey {
	// Server
	case "SERVER__BINDADDRESS":
		c.BindAddress = val
	case "SERVER__PORT":
		v, err := parseInt()
		if err != nil {
			return err
		}
		c.Port = v
	case "SERVER__URLBASE":
		c.URLBase = val
	case "SERVER__ENABLESSL":
		v, err := parseBool()
		if err != nil {
			return err
		}
		c.EnableSsl = v
	case "SERVER__SSLPORT":
		v, err := parseInt()
		if err != nil {
			return err
		}
		c.SslPort = v
	case "SERVER__SSLCERTPATH":
		c.SslCertPath = val
	case "SERVER__SSLKEYPATH":
		c.SslKeyPath = val

	// Auth
	case "AUTH__METHOD", "AUTH__AUTHENTICATIONMETHOD":
		c.AuthenticationMethod = val
	case "AUTH__AUTHENTICATIONREQUIRED":
		c.AuthenticationRequired = val
	case "AUTH__APIKEY":
		c.APIKey = val

	// Log
	case "LOG__LEVEL":
		c.LogLevel = val
	case "LOG__CONSOLELEVEL":
		c.ConsoleLogLevel = val
	case "LOG__SIZELIMIT":
		v, err := parseInt()
		if err != nil {
			return err
		}
		c.LogSizeLimit = v
	case "LOG__ROTATE":
		v, err := parseInt()
		if err != nil {
			return err
		}
		c.LogRotate = v

	// App
	case "APP__INSTANCENAME":
		c.InstanceName = val
	case "APP__LAUNCHBROWSER":
		v, err := parseBool()
		if err != nil {
			return err
		}
		c.LaunchBrowser = v
	case "APP__ENABLEDESKTOPNOTIFICATIONS":
		v, err := parseBool()
		if err != nil {
			return err
		}
		c.EnableDesktopNotifications = v

	// Update
	case "UPDATE__AUTOMATICALLY":
		v, err := parseBool()
		if err != nil {
			return err
		}
		c.UpdateAutomatically = v
	case "UPDATE__MECHANISM":
		c.UpdateMechanism = val

	// Storage
	case "STORAGE__DATABASEPATH":
		c.DatabasePath = val

	// AniList
	case "ANILIST__USERNAME":
		c.AniListUsername = val
	case "ANILIST__TVSTATUS":
		c.AniListTvStatus = val
	case "ANILIST__MOVIESTATUS":
		c.AniListMovieStatus = val
	case "ANILIST__INCLUDEUNRELEASED":
		v, err := parseBool()
		if err != nil {
			return err
		}
		c.AniListIncludeUnreleased = v

	// Radarr
	case "RADARR__URL":
		c.RadarrURL = val
	case "RADARR__APIKEY":
		c.RadarrAPIKey = val
	case "RADARR__QUALITYPROFILEID":
		v, err := parseInt()
		if err != nil {
			return err
		}
		c.RadarrQualityProfileID = v
	case "RADARR__ROOTFOLDERPATH":
		c.RadarrRootFolderPath = val
	case "RADARR__SEARCHONADD":
		v, err := parseBool()
		if err != nil {
			return err
		}
		c.RadarrSearchOnAdd = v

	// Sonarr
	case "SONARR__URL":
		c.SonarrURL = val
	case "SONARR__APIKEY":
		c.SonarrAPIKey = val
	case "SONARR__QUALITYPROFILEID":
		v, err := parseInt()
		if err != nil {
			return err
		}
		c.SonarrQualityProfileID = v
	case "SONARR__ROOTFOLDERPATH":
		c.SonarrRootFolderPath = val
	case "SONARR__SERIESTYPE":
		c.SonarrSeriesType = val
	case "SONARR__SEARCHONADD":
		v, err := parseBool()
		if err != nil {
			return err
		}
		c.SonarrSearchOnAdd = v

	// Sync
	case "SYNC__TAGNAME":
		c.TagName = val
	case "SYNC__INTERVALHOURS":
		v, err := parseInt()
		if err != nil {
			return err
		}
		c.SyncIntervalHours = v
	case "SYNC__DATETOLERANCEDAYS":
		v, err := parseInt()
		if err != nil {
			return err
		}
		c.DateToleranceDays = v
	case "SYNC__CONFIDENCETHRESHOLD":
		v, err := parseFloat()
		if err != nil {
			return err
		}
		c.ConfidenceThreshold = v
	case "SYNC__UNMONITORDROPPED":
		v, err := parseBool()
		if err != nil {
			return err
		}
		c.UnmonitorDropped = v
	case "SYNC__FIRSTRUNDRYRUN":
		v, err := parseBool()
		if err != nil {
			return err
		}
		c.FirstRunDryRun = v
	}

	return nil
}
