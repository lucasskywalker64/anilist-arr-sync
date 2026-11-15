package server

import (
	"testing"
	"time"
)

func TestPasswordHashing_PBKDF2(t *testing.T) {
	password := "correct-horse-battery-staple"

	hash, err := HashPassword(password, "pbkdf2")
	if err != nil {
		t.Fatalf("unexpected error hashing password: %v", err)
	}

	if hash == "" || hash == password {
		t.Fatalf("hash should not be empty or plain password")
	}

	if !CheckPassword(password, hash) {
		t.Errorf("expected password to verify against its PBKDF2 hash")
	}

	if CheckPassword("wrong-password", hash) {
		t.Errorf("expected wrong password to fail verification")
	}
}

func TestPasswordHashing_Bcrypt(t *testing.T) {
	password := "super-secret-pass"

	hash, err := HashPassword(password, "bcrypt")
	if err != nil {
		t.Fatalf("unexpected error hashing password with bcrypt: %v", err)
	}

	if !CheckPassword(password, hash) {
		t.Errorf("expected password to verify against its bcrypt hash")
	}

	if CheckPassword("invalid", hash) {
		t.Errorf("expected wrong password to fail bcrypt verification")
	}
}

func TestIsLocalAddress(t *testing.T) {
	tests := []struct {
		addr     string
		expected bool
	}{
		{"127.0.0.1:7171", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"[::1]:7171", true},
		{"192.168.1.50:12345", true},
		{"10.0.0.5", true},
		{"172.16.0.10:80", true},
		{"169.254.1.1", true},
		{"8.8.8.8:53", false},
		{"1.1.1.1", false},
		{"203.0.113.195", false},
		{"2001:4860:4860::8888", false},
		{"invalid-address", false},
	}

	for _, tt := range tests {
		got := IsLocalAddress(tt.addr)
		if got != tt.expected {
			t.Errorf("IsLocalAddress(%q) = %v; want %v", tt.addr, got, tt.expected)
		}
	}
}

func TestSessionStore(t *testing.T) {
	store := NewSessionStore(1 * time.Hour)

	token := store.Create("admin")
	if token == "" {
		t.Fatalf("expected non-empty token")
	}

	username, ok := store.Validate(token)
	if !ok || username != "admin" {
		t.Fatalf("expected session to be valid with username admin, got %q, %v", username, ok)
	}

	store.Delete(token)
	_, ok = store.Validate(token)
	if ok {
		t.Fatalf("expected session to be deleted")
	}
}
