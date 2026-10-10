package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/storage"
)

func newTestStorage(t *testing.T) *storage.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test storage: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestAuthAPIEndpoints(t *testing.T) {
	db := newTestStorage(t)
	cfg := config.NewDefault()
	cfg.AuthenticationMethod = "Forms"
	cfg.AuthenticationRequired = "Enabled"
	cfg.APIKey = "test-api-key"

	srv := NewServer(cfg, "", db, nil, nil)
	handler := srv.Handler()

	// 1. Initial setup creates first admin user
	setupBody, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "Password123!",
	})
	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(setupBody))
	setupReq.Header.Set("Content-Type", "application/json")
	setupRec := httptest.NewRecorder()
	handler.ServeHTTP(setupRec, setupReq)

	if setupRec.Code != http.StatusCreated {
		t.Fatalf("expected setup to return 201 Created, got %d: %s", setupRec.Code, setupRec.Body.String())
	}

	// 2. Subsequent setup should fail
	setupRec2 := httptest.NewRecorder()
	setupReq2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(setupBody))
	setupReq2.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(setupRec2, setupReq2)
	if setupRec2.Code != http.StatusBadRequest {
		t.Errorf("expected duplicate setup to return 400 Bad Request, got %d", setupRec2.Code)
	}

	// 3. Login with wrong password fails
	badLogin, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "wrong",
	})
	badLoginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(badLogin))
	badLoginReq.Header.Set("Content-Type", "application/json")
	badLoginRec := httptest.NewRecorder()
	handler.ServeHTTP(badLoginRec, badLoginReq)
	if badLoginRec.Code != http.StatusUnauthorized {
		t.Errorf("expected failed login to return 401 Unauthorized, got %d", badLoginRec.Code)
	}

	// 4. Login with correct password succeeds and sets session cookie
	loginBody, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "Password123!",
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected login to return 200 OK, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	cookies := loginRec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected session cookie to be set")
	}
	if !sessionCookie.HttpOnly {
		t.Errorf("expected session cookie to be HttpOnly")
	}

	// 5. Query /api/v1/auth/user with the session cookie
	userReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/user", nil)
	userReq.AddCookie(sessionCookie)
	userRec := httptest.NewRecorder()
	handler.ServeHTTP(userRec, userReq)

	if userRec.Code != http.StatusOK {
		t.Fatalf("expected /api/v1/auth/user to return 200 OK, got %d", userRec.Code)
	}
	var userResp map[string]any
	_ = json.Unmarshal(userRec.Body.Bytes(), &userResp)
	if userResp["username"] != "admin" {
		t.Errorf("expected user admin, got %v", userResp["username"])
	}

	// 6. Logout clears session
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.Header.Set("X-Api-Key", "test-api-key")
	logoutReq.AddCookie(sessionCookie)
	logoutRec := httptest.NewRecorder()
	handler.ServeHTTP(logoutRec, logoutReq)

	if logoutRec.Code != http.StatusOK {
		t.Fatalf("expected logout to return 200 OK, got %d", logoutRec.Code)
	}

	// 7. Subsequent request with old cookie should fail
	userReq2 := httptest.NewRequest(http.MethodGet, "/api/v1/auth/user", nil)
	userReq2.AddCookie(sessionCookie)
	userRec2 := httptest.NewRecorder()
	handler.ServeHTTP(userRec2, userReq2)
	if userRec2.Code != http.StatusUnauthorized {
		t.Errorf("expected logged-out session to return 401, got %d", userRec2.Code)
	}
}

func TestInsertBcryptUserDirectly(t *testing.T) {
	db := newTestStorage(t)
	cfg := config.NewDefault()
	cfg.AuthenticationMethod = "Forms"
	cfg.AuthenticationRequired = "Enabled"
	cfg.APIKey = "test-api-key"

	srv := NewServer(cfg, "", db, nil, nil)

	hash, _ := HashPassword("bcrypt-pass", "bcrypt")
	err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		// insert user directly
		_, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?)", "bcryptuser", hash)
		return err
	})
	if err != nil {
		t.Fatalf("failed to insert bcrypt user: %v", err)
	}

	loginBody, _ := json.Marshal(map[string]string{
		"username": "bcryptuser",
		"password": "bcrypt-pass",
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected bcrypt login to return 200 OK, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
}
