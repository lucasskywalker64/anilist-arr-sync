# Source code and licensing

AniList-Arr-Sync is free software under **AGPL-3.0-only**. Copyright (C) 2026
AniList-Arr-Sync contributors. You may redistribute and modify it under version
3 of the GNU Affero General Public License. There is no warranty; see [LICENSE](LICENSE).
Third-party components retain the licenses and notices in
[THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt).

See the [code signing policy](CODE_SIGNING.md) for release verification and the
[privacy policy](PRIVACY.md) for application data and network requests.

## Get the source for a particular build

The application's **License and source** link and `anilist-arr-sync --license`
provide a source download pinned to its full build commit, plus these instructions
at that same commit. Release notes provide the same links beside the downloads.
Use that commit rather than a moving branch or the rolling `nightly` tag.

For a full commit `COMMIT`, the application source is available without charge at:

`https://github.com/lucasskywalker64/anilist-arr-sync/archive/COMMIT.tar.gz`

This includes the backend, Svelte frontend, lockfiles, installer code, and build
and release scripts. The corresponding source also includes the dependency source:
each exact Go module source ZIP and frontend source package has an immutable
download URL in **THIRD_PARTY_NOTICES.txt**, both here and in the running program's
**Third-party notices and dependency source** page. Those archives contain the
dependency sources and upstream notices. They can be downloaded separately;
`go mod download` and `npm ci --prefix web` fetch the pinned dependencies and
verify the checksums recorded by the lockfiles. Preserve these source locations
while distributing the corresponding binaries. If an upstream location becomes
unavailable, restore equivalent source access before continuing distribution.

## Build and run a modified version

Install the Go version from `go.mod` and Node.js 24 (used by CI and release
workflows). The frontend also supports Node.js 22.13+ within version 22, or
version 26 and newer. Python 3 is used to check license notices.

```sh
go mod download
npm ci --prefix web
npm run build --prefix web
python packaging/licenses.py --check
go build ./cmd/anilist-arr-sync
```

With GNU Make installed, `make build` runs the frontend build, checks license
notices, and writes the same application executable in the repository root.
Frontend targets install locked npm dependencies when the npm installation marker
is missing or older than either package file. Use `make frontend-install` to force
a clean reinstall. On Windows, stop frontend dev servers and test watchers before
reinstalling dependencies; editor extensions can also keep native modules locked.

| Command | Work performed |
| --- | --- |
| `make` or `make all` | Build, frontend tests, Go race tests, frontend typechecking, license checks, and Go lint checks. |
| `make build` | Build the embedded frontend, check notices, and write the application executable. |
| `make test` | Build the frontend, check notices, and run frontend and Go tests. |
| `make test-race` | Run the same checks as `make test`, with the Go race detector enabled. |
| `make lint` | Build the frontend, check its types and notices, then run `go vet` and `golangci-lint`. |
| `make frontend-install` | Reinstall frontend dependencies from the lockfile. |
| `make clean` | Remove the local application executable and generated frontend files, preserving `web/dist/.gitkeep` and installed dependencies. |

`make all` and `make lint` run `golangci-lint` v2.14.0 through Go, without a
separate installation or PATH changes. The first run downloads and compiles the
linter and its dependencies into Go's caches, so it needs network access and can
take longer than subsequent runs.
Go race tests require CGO and a working C compiler. The Python command defaults to
`python`; use `make PYTHON=python3 build` if your Python 3 executable is named
`python3`. These targets perform local builds and checks; they do not publish or
request production signatures.

Run `anilist-arr-sync` (`anilist-arr-sync.exe` on Windows) and open
`http://localhost:7171`. Use `--data-dir` to choose persistent storage. No private
signing credentials are needed to build, modify, install, or run this software.
The optional Windows installer is built from `packaging/windows.iss` using Inno
Setup 7.1.1; it supports service and desktop modes. Packaging automation is in
`.goreleaser.yaml`, `packaging/`, and `.github/workflows/release*.yml`.

To embed source identity in a build, supply the actual version and full source
commit, replacing the placeholders below:

```sh
go build -ldflags "-X main.version=VERSION -X main.commit=COMMIT" ./cmd/anilist-arr-sync
```

Set `CGO_ENABLED=0` for standalone release executables. `GOOS=linux` or `windows`
and `GOARCH=amd64` or `arm64` select the four supported release targets. Rebuild
the frontend before the executable after modifying frontend sources.

## Redistributing and hosting modified versions

Keep the license, copyright, and dependency notices. Provide corresponding source
for the version you distribute, including its changes and necessary build scripts.
A modified version used over a network must prominently offer its corresponding
source, without charge, to users interacting with it. Preserve a working source
offer when changing the UI, and update `Repository` in `license.go` to your own
public source repository and embed the actual modified commit. The source links
must lead to your running version, not the original project's unchanged code.

Source and notices are embedded in every executable, including one-file portable
archives. `--license` works before configuration or startup, and `/legal`,
`/legal/license`, and `/legal/notices` are accessible without authentication under
the configured URL base. This preserves the updater's single-executable archive
format. The repository owner must retain public access to release commits and
their source instructions, including old nightly commits.

When adding dependencies, audit their licenses, add required source locations and
notices, update `packaging/licenses.py`, and regenerate the notices. CI rejects
missing or stale runtime notices. This document explains the project's source
delivery; the full license controls the permissions and obligations.
