package anilist_test

import (
	"testing"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/anilist"
)

func TestMedia_CoverImageURL_PreferenceOrder(t *testing.T) {
	tests := []struct {
		name       string
		coverImage anilist.MediaCoverImage
		expected   string
	}{
		{
			name: "prefers extraLarge when all URLs are present",
			coverImage: anilist.MediaCoverImage{
				ExtraLarge: "https://example.com/extralarge.jpg",
				Large:      "https://example.com/large.jpg",
				Medium:     "https://example.com/medium.jpg",
				Color:      "#e4a15d",
			},
			expected: "https://example.com/extralarge.jpg",
		},
		{
			name: "prefers large when extraLarge is absent",
			coverImage: anilist.MediaCoverImage{
				ExtraLarge: "",
				Large:      "https://example.com/large.jpg",
				Medium:     "https://example.com/medium.jpg",
				Color:      "#e4a15d",
			},
			expected: "https://example.com/large.jpg",
		},
		{
			name: "falls back to medium when extraLarge and large are absent",
			coverImage: anilist.MediaCoverImage{
				ExtraLarge: "",
				Large:      "",
				Medium:     "https://example.com/medium.jpg",
				Color:      "#e4a15d",
			},
			expected: "https://example.com/medium.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := anilist.Media{
				CoverImage: tt.coverImage,
			}
			got := m.CoverImageURL()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestMedia_CoverImageURL_ValidColorFallback(t *testing.T) {
	tests := []struct {
		name       string
		coverImage anilist.MediaCoverImage
		expected   string
	}{
		{
			name: "missing image URLs with leading # in color",
			coverImage: anilist.MediaCoverImage{
				Color: "#e4a15d",
			},
			expected: "https://dummyimage.com/400x600/e4a15d/e4a15d.png",
		},
		{
			name: "missing image URLs without leading # in color",
			coverImage: anilist.MediaCoverImage{
				Color: "35acff",
			},
			expected: "https://dummyimage.com/400x600/35acff/35acff.png",
		},
		{
			name: "missing image URLs with uppercase hex color",
			coverImage: anilist.MediaCoverImage{
				Color: "#A1B2C3",
			},
			expected: "https://dummyimage.com/400x600/A1B2C3/A1B2C3.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := anilist.Media{
				CoverImage: tt.coverImage,
			}
			got := m.CoverImageURL()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestMedia_CoverImageURL_DefaultFallback(t *testing.T) {
	tests := []struct {
		name       string
		coverImage anilist.MediaCoverImage
		expected   string
	}{
		{
			name: "missing image URLs and empty color",
			coverImage: anilist.MediaCoverImage{
				Color: "",
			},
			expected: "https://dummyimage.com/400x600/2b2d42/2b2d42.png",
		},
		{
			name: "missing image URLs and 3-digit hex color",
			coverImage: anilist.MediaCoverImage{
				Color: "#fff",
			},
			expected: "https://dummyimage.com/400x600/2b2d42/2b2d42.png",
		},
		{
			name: "missing image URLs and 7-digit hex color",
			coverImage: anilist.MediaCoverImage{
				Color: "#1234567",
			},
			expected: "https://dummyimage.com/400x600/2b2d42/2b2d42.png",
		},
		{
			name: "missing image URLs and non-hex characters",
			coverImage: anilist.MediaCoverImage{
				Color: "#xyz123",
			},
			expected: "https://dummyimage.com/400x600/2b2d42/2b2d42.png",
		},
		{
			name: "missing image URLs and random text color",
			coverImage: anilist.MediaCoverImage{
				Color: "blue",
			},
			expected: "https://dummyimage.com/400x600/2b2d42/2b2d42.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := anilist.Media{
				CoverImage: tt.coverImage,
			}
			got := m.CoverImageURL()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
