package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/servarr"
)

func (s *Server) reviewPreparationID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		s.writeJSONError(w, 400, "invalid review ID")
		return 0, false
	}
	if s.orch == nil {
		s.writeJSONError(w, 503, "sync services are not configured")
		return 0, false
	}
	return id, true
}

func (s *Server) writePreparation(w http.ResponseWriter, result *orchestrator.ReviewPreparation, err error) {
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, orchestrator.ErrSyncInProgress) {
			status = http.StatusConflict
		}
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		s.writeJSONError(w, status, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) handlePrepareReview(w http.ResponseWriter, r *http.Request) {
	id, ok := s.reviewPreparationID(w, r)
	if !ok {
		return
	}
	var req struct {
		TargetID int `json:"targetId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetID <= 0 {
		s.writeJSONError(w, 400, "positive targetId is required")
		return
	}
	// Persist ownership even if the browser disconnects while Sonarr is accepting the add.
	ctx, cancel, ok := s.reviewWork(45 * time.Second)
	if !ok {
		s.writeJSONError(w, 503, "server is stopping")
		return
	}
	defer cancel()
	result, err := s.orch.PrepareReview(ctx, id, req.TargetID)
	s.writePreparation(w, result, err)
}

func (s *Server) handlePollReview(w http.ResponseWriter, r *http.Request) {
	id, ok := s.reviewPreparationID(w, r)
	if !ok {
		return
	}
	result, err := s.orch.PollReview(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		result = &orchestrator.ReviewPreparation{Status: "CANCELLED"}
		err = nil
	}
	s.writePreparation(w, result, err)
}

func (s *Server) handleCancelPreparation(w http.ResponseWriter, r *http.Request) {
	id, ok := s.reviewPreparationID(w, r)
	if !ok {
		return
	}
	ctx, cancel, ok := s.reviewWork(30 * time.Second)
	if !ok {
		s.writeJSONError(w, 503, "server is stopping")
		return
	}
	defer cancel()
	result, err := s.orch.CancelReview(ctx, id)
	s.writePreparation(w, result, err)
}

func (s *Server) reviewWork(timeout time.Duration) (context.Context, context.CancelFunc, bool) {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	if s.stopping {
		return nil, nil, false
	}
	s.work.Add(1)
	ctx, cancel := context.WithTimeout(s.workCtx, timeout)
	return ctx, func() { cancel(); s.work.Done() }, true
}

func (s *Server) handleReviewMovieLookup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("tmdbId"))
	if err != nil || id <= 0 {
		s.writeJSONError(w, 400, "invalid TMDB movie ID")
		return
	}
	cfg := s.Config()
	client, err := servarr.NewRadarrClient(cfg.RadarrURL, cfg.RadarrAPIKey, servarr.WithHTTPClient(s.outboundHTTPClient))
	if err != nil {
		s.writeJSONError(w, 400, "Radarr is not configured")
		return
	}
	movie, err := client.LookupMovieMetadata(r.Context(), id)
	if err != nil {
		s.writeJSONError(w, 400, "movie lookup failed: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(movie)
}

func (s *Server) maintainReviewPreparations() {
	if s.orch == nil {
		return
	}
	s.workMu.Lock()
	if s.stopping {
		s.workMu.Unlock()
		return
	}
	s.work.Add(1)
	s.workMu.Unlock()
	go func() {
		defer s.work.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(s.workCtx, 30*time.Second)
			err := s.orch.MaintainReviewPreparations(ctx)
			cancel()
			if err != nil && !errors.Is(err, orchestrator.ErrSyncInProgress) && !errors.Is(err, context.Canceled) {
				log.Printf("review preparation cleanup: %v", err)
			}
			select {
			case <-s.workCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
