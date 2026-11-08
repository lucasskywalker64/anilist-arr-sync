package servarr

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// NewClients creates clients for the configured Servarr URLs.
func NewClients(sonarrURL, sonarrKey, radarrURL, radarrKey string, opts ...Option) (*SonarrClient, *RadarrClient, error) {
	var sonarr *SonarrClient
	var radarr *RadarrClient
	var err error
	if sonarrURL != "" {
		sonarr, err = NewSonarrClient(sonarrURL, sonarrKey, opts...)
		if err != nil {
			return nil, nil, err
		}
	}
	if radarrURL != "" {
		radarr, err = NewRadarrClient(radarrURL, radarrKey, opts...)
		if err != nil {
			return nil, nil, err
		}
	}
	return sonarr, radarr, nil
}

// QualityProfile is a selectable Servarr quality profile.
type QualityProfile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// RootFolder is a selectable Servarr media location.
type RootFolder struct {
	ID   int    `json:"id"`
	Path string `json:"path"`
}

// ConnectionOptions contains the available defaults for newly added media.
type ConnectionOptions struct {
	QualityProfiles []QualityProfile `json:"qualityProfiles"`
	RootFolders     []RootFolder     `json:"rootFolders"`
}

// TestConnection verifies the service identity and loads its profiles and folders.
func (c *Client) TestConnection(ctx context.Context, app string) (ConnectionOptions, error) {
	get := func(endpoint, resource string, target any) error {
		req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
		if err == nil {
			resp, requestErr := c.do(req, target)
			switch {
			case requestErr == nil:
				return nil
			case resp != nil:
				return fmt.Errorf("could not load %s from %s (HTTP %d)", resource, app, resp.StatusCode)
			}
		}
		return fmt.Errorf("could not connect to %s to load %s", app, resource)
	}
	var status struct{ AppName string }
	var options ConnectionOptions
	if err := get("/api/v3/system/status", "system status", &status); err != nil {
		return options, err
	}
	if !strings.EqualFold(status.AppName, app) {
		return options, fmt.Errorf("the URL does not identify a %s instance", app)
	}
	options.QualityProfiles = []QualityProfile{}
	options.RootFolders = []RootFolder{}
	if err := get("/api/v3/qualityprofile", "quality profiles", &options.QualityProfiles); err != nil {
		return options, err
	}
	if err := get("/api/v3/rootfolder", "root folders", &options.RootFolders); err != nil {
		return options, err
	}
	return options, nil
}
