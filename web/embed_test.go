package web_test

import (
	"io"
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/web"
)

func TestDist_EmbedsIndexHTML(t *testing.T) {
	dist, err := web.Dist()
	if err != nil {
		t.Fatalf("failed to open embedded dist: %v", err)
	}

	f, err := dist.Open("index.html")
	if err != nil {
		t.Fatalf("expected index.html to be embedded, got err: %v", err)
	}
	defer func() { _ = f.Close() }()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read embedded index.html: %v", err)
	}

	if len(content) == 0 {
		t.Errorf("embedded index.html is empty")
	}
}
