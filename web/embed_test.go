package web

import (
	"io"
	"strings"
	"testing"
)

func TestDistFileSystem(t *testing.T) {
	dist, err := Dist()
	if err != nil {
		t.Fatalf("expected Dist() to succeed, got %v", err)
	}

	f, err := dist.Open("index.html")
	if err != nil {
		t.Fatalf("expected index.html to exist in embedded fs, got %v", err)
	}
	defer func() { _ = f.Close() }()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}

	if !strings.Contains(string(content), "<!DOCTYPE html>") {
		t.Errorf("expected valid HTML in index.html, got: %s", string(content))
	}
}
