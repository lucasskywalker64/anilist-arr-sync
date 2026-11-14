// Package licensing supplies the license, notices, and source location carried
// by every executable, including portable updates that contain no other files.
package licensing

import (
	_ "embed"
	"regexp"
	"runtime"
)

// Identifier is the SPDX identifier for the application's license.
const Identifier = "AGPL-3.0-only"

// Copyright identifies the copyright holders of the original application code.
const Copyright = "Copyright (C) 2026 AniList-Arr-Sync contributors"

// Repository hosts the application's source; forks must update their source offer.
const Repository = "https://github.com/lucasskywalker64/anilist-arr-sync"

// License is the complete, unmodified AGPLv3 license text.
//
//go:embed LICENSE
var License string

// Notices preserves the licenses, copyright notices, and sources of dependencies.
//
//go:embed THIRD_PARTY_NOTICES.txt
var Notices string

var sourceCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Source returns the exact application source and its dependency-source and
// build instructions. Development builds without a commit point to the repo.
func Source(commit string) (archive, instructions string) {
	if !sourceCommit.MatchString(commit) {
		return Repository, Repository + "/blob/develop/SOURCE.md"
	}
	return Repository + "/archive/" + commit + ".tar.gz", Repository + "/blob/" + commit + "/SOURCE.md"
}

// Information is available without starting a server or creating configuration.
func Information(commit string) string {
	archive, instructions := Source(commit)
	return "AniList-Arr-Sync\n" + Copyright + "\nLicensed under " + Identifier +
		". This program comes with absolutely no warranty.\n\nSource: " + archive +
		"\nDependency source and build instructions: " + instructions +
		"\nGo standard library source: " + ToolchainSource() + "\n\n" + License + "\n\n" + Notices
}

// ToolchainSource identifies the standard library actually compiled into a build.
func ToolchainSource() string { return "https://go.dev/dl/" + runtime.Version() + ".src.tar.gz" }
