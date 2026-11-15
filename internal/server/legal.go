package server

import (
	"html/template"
	"io"
	"net/http"

	licensing "github.com/lucasskywalker64/anilist-arr-sync"
)

var legalPage = template.Must(template.New("legal").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>AniList-Arr-Sync license and source</title></head>
<body><main><h1>AniList-Arr-Sync: license and source</h1>
<p>{{.Copyright}}. Licensed under {{.License}}.</p>
<p>This is free software. You may redistribute and modify it under the GNU Affero General Public License, version 3 only. This program comes with absolutely no warranty; see the full license for details.</p>
<p><a href="{{.Source}}">Download source code for this build</a></p>
<p><a href="{{.Instructions}}">Dependency source and build instructions</a>. Corresponding source includes the application and its dependencies; the instructions and third-party notices provide their source locations.</p>
<p><a href="{{.ToolchainSource}}">Go standard library source for this build</a></p>
<p><a href="{{.Base}}/legal/license">Full license</a> | <a href="{{.Base}}/legal/notices">Third-party notices and dependency source</a></p>
<p>Version: {{.Version}}. Commit: {{.Commit}}.</p>
</main></body></html>`))

func (s *Server) handleLegal(w http.ResponseWriter, _ *http.Request) {
	source, instructions := licensing.Source(s.commit)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = legalPage.Execute(w, struct {
		Copyright, License, Source, Instructions, Base, Version, Commit, ToolchainSource string
	}{licensing.Copyright, licensing.Identifier, source, instructions, s.getCleanURLBase(), s.version, s.commit, licensing.ToolchainSource()})
}

func (s *Server) handleLicense(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, licensing.License)
}

func (s *Server) handleNotices(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, licensing.Notices)
}
