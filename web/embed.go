// Package web provides embedded static assets for the single-page application dashboard.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist returns an fs.FS subrooted at the dist directory.
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
