package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/notification"
)

type createNotificationRequest struct {
	Name                string         `json:"name"`
	Provider            string         `json:"provider"`
	Type                string         `json:"type"`
	ConfigJSON          string         `json:"configJson"`
	Settings            map[string]any `json:"settings"`
	URL                 string         `json:"url"`
	OnAdded             *bool          `json:"onAdded"`
	OnReview            *bool          `json:"onReview"`
	OnError             *bool          `json:"onError"`
	OnComplete          *bool          `json:"onComplete"`
	OnUpdate            *bool          `json:"onUpdate"`
	OnSyncStart         *bool          `json:"onSyncStart"`
	OnSyncSuccess       *bool          `json:"onSyncSuccess"`
	OnSyncFailure       *bool          `json:"onSyncFailure"`
	OnReviewQueueNeeded *bool          `json:"onReviewQueueNeeded"`
}

type testNotificationRequest struct {
	Name       string         `json:"name"`
	Provider   string         `json:"provider"`
	Type       string         `json:"type"`
	ConfigJSON string         `json:"configJson"`
	Settings   map[string]any `json:"settings"`
	URL        string         `json:"url"`
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	if s.notifStore == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "notification store not initialized")
		return
	}

	list, err := s.notifStore.List(r.Context())
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "failed to query notification connections")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(list)
}

func (s *Server) handleCreateNotification(w http.ResponseWriter, r *http.Request) {
	if s.notifStore == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "notification store not initialized")
		return
	}

	var req createNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeJSONError(w, http.StatusBadRequest, "name is required")
		return
	}

	if req.Provider == "" && req.Type != "" {
		req.Provider = req.Type
	}
	req.Provider = strings.ToUpper(strings.TrimSpace(req.Provider))
	if req.Provider != "DISCORD" && req.Provider != "WEBHOOK" {
		s.writeJSONError(w, http.StatusBadRequest, "provider must be DISCORD or WEBHOOK")
		return
	}

	req.ConfigJSON = strings.TrimSpace(req.ConfigJSON)
	if req.ConfigJSON == "" {
		if req.Settings != nil {
			if u, ok := req.Settings["webhookUrl"].(string); ok && u != "" {
				b, _ := json.Marshal(map[string]string{"url": u})
				req.ConfigJSON = string(b)
			} else if u, ok := req.Settings["url"].(string); ok && u != "" {
				b, _ := json.Marshal(map[string]string{"url": u})
				req.ConfigJSON = string(b)
			}
		}
		if req.ConfigJSON == "" && req.URL != "" {
			b, _ := json.Marshal(map[string]string{"url": req.URL})
			req.ConfigJSON = string(b)
		}
	}
	if req.ConfigJSON == "" {
		s.writeJSONError(w, http.StatusBadRequest, "configJson is required")
		return
	}

	if req.OnAdded == nil && req.OnSyncStart != nil {
		req.OnAdded = req.OnSyncStart
	}
	if req.OnComplete == nil && req.OnSyncSuccess != nil {
		req.OnComplete = req.OnSyncSuccess
	}
	if req.OnError == nil && req.OnSyncFailure != nil {
		req.OnError = req.OnSyncFailure
	}
	if req.OnReview == nil && req.OnReviewQueueNeeded != nil {
		req.OnReview = req.OnReviewQueueNeeded
	}

	conn := notification.Connection{
		Name:       req.Name,
		Provider:   req.Provider,
		ConfigJSON: req.ConfigJSON,
		OnAdded:    true,
		OnReview:   true,
		OnError:    true,
		OnComplete: false,
		OnUpdate:   true,
	}

	if req.OnAdded != nil {
		conn.OnAdded = *req.OnAdded
	}
	if req.OnReview != nil {
		conn.OnReview = *req.OnReview
	}
	if req.OnError != nil {
		conn.OnError = *req.OnError
	}
	if req.OnComplete != nil {
		conn.OnComplete = *req.OnComplete
	}
	if req.OnUpdate != nil {
		conn.OnUpdate = *req.OnUpdate
	}

	id, err := s.notifStore.Insert(r.Context(), conn)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "failed to save notification connection")
		return
	}

	saved, err := s.notifStore.Get(r.Context(), id)
	if err != nil || saved == nil {
		conn.ID = id
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(conn)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(saved)
}

func (s *Server) handleUpdateNotification(w http.ResponseWriter, r *http.Request) {
	if s.notifStore == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "notification store not initialized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid connection id")
		return
	}

	existing, err := s.notifStore.Get(r.Context(), id)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "failed to query connection")
		return
	}
	if existing == nil {
		s.writeJSONError(w, http.StatusNotFound, "notification connection not found")
		return
	}

	var req createNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Name) != "" {
		existing.Name = strings.TrimSpace(req.Name)
	}
	if req.Provider == "" && req.Type != "" {
		req.Provider = req.Type
	}
	if strings.TrimSpace(req.Provider) != "" {
		prov := strings.ToUpper(strings.TrimSpace(req.Provider))
		if prov != "DISCORD" && prov != "WEBHOOK" {
			s.writeJSONError(w, http.StatusBadRequest, "provider must be DISCORD or WEBHOOK")
			return
		}
		existing.Provider = prov
	}
	if strings.TrimSpace(req.ConfigJSON) != "" {
		existing.ConfigJSON = strings.TrimSpace(req.ConfigJSON)
	} else {
		newConfigJSON := ""
		if req.Settings != nil {
			if u, ok := req.Settings["webhookUrl"].(string); ok && u != "" {
				b, _ := json.Marshal(map[string]string{"url": u})
				newConfigJSON = string(b)
			} else if u, ok := req.Settings["url"].(string); ok && u != "" {
				b, _ := json.Marshal(map[string]string{"url": u})
				newConfigJSON = string(b)
			}
		}
		if newConfigJSON == "" && req.URL != "" {
			b, _ := json.Marshal(map[string]string{"url": req.URL})
			newConfigJSON = string(b)
		}
		if newConfigJSON != "" {
			existing.ConfigJSON = newConfigJSON
		}
	}

	if req.OnAdded == nil && req.OnSyncStart != nil {
		req.OnAdded = req.OnSyncStart
	}
	if req.OnComplete == nil && req.OnSyncSuccess != nil {
		req.OnComplete = req.OnSyncSuccess
	}
	if req.OnError == nil && req.OnSyncFailure != nil {
		req.OnError = req.OnSyncFailure
	}
	if req.OnReview == nil && req.OnReviewQueueNeeded != nil {
		req.OnReview = req.OnReviewQueueNeeded
	}

	if req.OnAdded != nil {
		existing.OnAdded = *req.OnAdded
	}
	if req.OnReview != nil {
		existing.OnReview = *req.OnReview
	}
	if req.OnError != nil {
		existing.OnError = *req.OnError
	}
	if req.OnComplete != nil {
		existing.OnComplete = *req.OnComplete
	}
	if req.OnUpdate != nil {
		existing.OnUpdate = *req.OnUpdate
	}

	if err := s.notifStore.Update(r.Context(), *existing); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "failed to update notification connection")
		return
	}

	updated, err := s.notifStore.Get(r.Context(), id)
	if err != nil || updated == nil {
		updated = existing
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(updated)
}

func (s *Server) handleDeleteNotification(w http.ResponseWriter, r *http.Request) {
	if s.notifStore == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "notification store not initialized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid connection id")
		return
	}

	if err := s.notifStore.Delete(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeJSONError(w, http.StatusNotFound, "notification connection not found")
			return
		}
		s.writeJSONError(w, http.StatusInternalServerError, "failed to delete notification connection")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
	})
}

func (s *Server) handleTestNotificationPayload(w http.ResponseWriter, r *http.Request) {
	if s.notifier == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "notification engine not initialized")
		return
	}

	var req testNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	prov := strings.ToUpper(strings.TrimSpace(req.Provider))
	if prov == "" && req.Type != "" {
		prov = strings.ToUpper(strings.TrimSpace(req.Type))
	}
	if prov != "DISCORD" && prov != "WEBHOOK" {
		s.writeJSONError(w, http.StatusBadRequest, "provider must be DISCORD or WEBHOOK")
		return
	}

	configJSON := strings.TrimSpace(req.ConfigJSON)
	if configJSON == "" {
		if req.Settings != nil {
			if u, ok := req.Settings["webhookUrl"].(string); ok && u != "" {
				b, _ := json.Marshal(map[string]string{"url": u})
				configJSON = string(b)
			} else if u, ok := req.Settings["url"].(string); ok && u != "" {
				b, _ := json.Marshal(map[string]string{"url": u})
				configJSON = string(b)
			}
		}
		if configJSON == "" && req.URL != "" {
			b, _ := json.Marshal(map[string]string{"url": req.URL})
			configJSON = string(b)
		}
	}

	conn := notification.Connection{
		Name:       req.Name,
		Provider:   prov,
		ConfigJSON: configJSON,
	}

	if err := s.notifier.TestConnection(r.Context(), conn); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"message": "Test notification delivered successfully",
	})
}

func (s *Server) handleTestNotificationConnection(w http.ResponseWriter, r *http.Request) {
	if s.notifier == nil || s.notifStore == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "notification engine not initialized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid connection id")
		return
	}

	conn, err := s.notifStore.Get(r.Context(), id)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "failed to query connection")
		return
	}
	if conn == nil {
		s.writeJSONError(w, http.StatusNotFound, "notification connection not found")
		return
	}

	if err := s.notifier.TestConnection(r.Context(), *conn); err != nil {
		s.writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"message": "Test notification delivered successfully",
	})
}
