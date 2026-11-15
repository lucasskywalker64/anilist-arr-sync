package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

// SetOutboundHTTPClient installs the runtime's outbound transport before serving requests.
func (s *Server) SetOutboundHTTPClient(client *http.Client) {
	s.outboundHTTPClient = client
}

// SetSyncPreparation installs the mapping prerequisite before serving requests.
func (s *Server) SetSyncPreparation(prepare func(context.Context) error) {
	s.prepareSync = prepare
}

func servarrConnectionsChanged(running, saved config.Config) bool {
	return running.SonarrURL != saved.SonarrURL || running.SonarrAPIKey != saved.SonarrAPIKey ||
		running.RadarrURL != saved.RadarrURL || running.RadarrAPIKey != saved.RadarrAPIKey
}

func validateOnboardingConfig(next config.Config) error {
	if strings.TrimSpace(next.AniListUsername) == "" || strings.TrimSpace(next.SonarrURL) == "" ||
		strings.TrimSpace(next.SonarrAPIKey) == "" || next.SonarrQualityProfileID <= 0 || strings.TrimSpace(next.SonarrRootFolderPath) == "" {
		return errors.New("completing setup requires an AniList username and Sonarr connection, profile, and root folder")
	}
	if next.RadarrURL != "" && (strings.TrimSpace(next.RadarrAPIKey) == "" || next.RadarrQualityProfileID <= 0 || strings.TrimSpace(next.RadarrRootFolderPath) == "") {
		return errors.New("completing setup with Radarr requires its API key, profile, and root folder")
	}
	effective := next
	if err := effective.ApplyEnv(os.Environ()); err != nil {
		return errors.New("could not validate setup against environment variables")
	}
	if servarrConnectionsChanged(next, effective) || effective.AniListUsername != next.AniListUsername ||
		!slices.Equal(anilist.ParseListStatuses(effective.AniListTvStatus), anilist.ParseListStatuses(next.AniListTvStatus)) ||
		!slices.Equal(anilist.ParseListStatuses(effective.AniListMovieStatus), anilist.ParseListStatuses(next.AniListMovieStatus)) ||
		effective.SonarrQualityProfileID != next.SonarrQualityProfileID || effective.SonarrRootFolderPath != next.SonarrRootFolderPath ||
		effective.RadarrQualityProfileID != next.RadarrQualityProfileID || effective.RadarrRootFolderPath != next.RadarrRootFolderPath ||
		effective.SonarrSearchOnAdd != next.SonarrSearchOnAdd || effective.RadarrSearchOnAdd != next.RadarrSearchOnAdd ||
		effective.FirstRunDryRun != next.FirstRunDryRun || effective.SyncIntervalHours != next.SyncIntervalHours {
		return errors.New("environment variables override setup settings. Adjust those variables and restart before saving setup")
	}
	return nil
}

func (s *Server) handleTestAniList(w http.ResponseWriter, r *http.Request) {
	var req struct{ Username string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		s.writeJSONError(w, http.StatusBadRequest, "AniList username is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	client := anilist.NewClient("", anilist.WithHTTPClient(s.outboundHTTPClient))
	profile, err := client.FetchProfile(ctx, username)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, anilist.ErrProfileNotFound) {
			status = http.StatusNotFound
		}
		s.writeJSONError(w, status, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

func (s *Server) handleTestServarr(w http.ResponseWriter, r *http.Request) {
	var req struct{ Type, BaseURL, APIKey string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := s.Config()
	var app, savedURL, savedKey string
	switch req.Type {
	case "sonarr":
		app, savedURL, savedKey = "Sonarr", cfg.SonarrURL, cfg.SonarrAPIKey
	case "radarr":
		app, savedURL, savedKey = "Radarr", cfg.RadarrURL, cfg.RadarrAPIKey
	default:
		s.writeJSONError(w, http.StatusBadRequest, "type must be sonarr or radarr")
		return
	}
	baseURL, err := servarr.NormalizeBaseURL(req.BaseURL)
	parsed, parseErr := url.Parse(baseURL)
	if err != nil || parseErr != nil || parsed.Hostname() == "" || parsed.User != nil || strings.ContainsAny(baseURL, "?#") {
		s.writeJSONError(w, http.StatusBadRequest, "enter a valid HTTP or HTTPS base URL")
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if key == "" {
		if saved, err := servarr.NormalizeBaseURL(savedURL); err == nil && saved == baseURL {
			key = savedKey
		}
	}
	if strings.TrimSpace(key) == "" || key == "******" {
		s.writeJSONError(w, http.StatusBadRequest, "API key is required")
		return
	}
	client := s.outboundHTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	outbound := *client
	// Keep X-Api-Key scoped to the URL the administrator provided.
	outbound.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	servarrClient, err := servarr.NewClient(baseURL, key, servarr.WithHTTPClient(&outbound))
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "enter a valid HTTP or HTTPS base URL")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	options, err := servarrClient.TestConnection(ctx, app)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(options)
}
