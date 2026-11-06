# anilist-arr-sync

Build the embedded UI before compiling the service: run `npm ci` and
`npm run build` in `web`, then `go build ./cmd/anilist-arr-sync` at the repo root.
Production builds support `CGO_ENABLED=0`. Build metadata can be supplied with
`-ldflags "-X main.version=v1.0.0 -X main.commit=<sha> -X main.date=<date>"`.

The executable supports `--config`, `--data-dir`, `--port`, `--bind-address`,
`--sync`, `--no-browser`, and `--version`. `--sync` prints a summary and exits
with code 0 on success or 1 on error. CLI port and bind overrides are transient.

The default data directory is `/config` in containers,
`%ProgramData%\AniList-Arr-Sync` on Windows (falling back to
`%APPDATA%\AniList-Arr-Sync` when unwritable), and
`$XDG_CONFIG_HOME/anilist-arr-sync` or `~/.config/anilist-arr-sync` on Linux/macOS.
Startup creates configuration and a random API key when missing. Relative
database and backup paths resolve under the data directory. Backup retention is
measured in days. Scheduled sync stays idle until AniList and at least one
Servarr URL/API key are configured. Restart after changing service connections,
proxy, storage, or backup destination/retention settings.

The header actions menu provides Restart, Shutdown, and Keyboard Shortcuts.
Lifecycle POST endpoints accept an API key or a session with CSRF protection,
using the existing authentication middleware. Shutdown is disabled in containers.
Restart rebuilds dependencies and reloads configuration in the same process;
valid browser sessions survive these restarts. A process restart or a change of
database requires signing in again. SIGINT and SIGTERM request shutdown, with a
15-second cleanup deadline.

When disabling HTTPS, the runtime temporarily serves restart status at the old
HTTPS address and automatically navigates to HTTP after the new instance is
healthy. This status-only listener expires within one minute. If it cannot use
the old port, or reconnection fails or times out, the overlay offers a manual
reconnect link. Browser HTTPS enforcement and reverse proxy routing may still
require manual intervention. Secure cookies may require a new login when
switching to HTTP.
