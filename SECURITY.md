# Security policy

AniList-Arr-Sync is a self-hosted application that stores Sonarr and Radarr API
keys, exposes a web dashboard, creates configuration and database backups, and
installs updates. This policy explains how to report security issues privately
and describes the application's security boundaries and expected protections.

## Reporting a vulnerability

Report suspected vulnerabilities privately through [GitHub private vulnerability
reporting](https://github.com/lucasskywalker64/anilist-arr-sync/security/advisories/new).

Do not open public GitHub issues, pull requests, or discussions for security
vulnerabilities, and never include API keys, passwords, session cookies, webhook
URLs, or exploit details in public threads.

To help reproduce and assess a report, include:

- The affected release version or exact source commit.
- Relevant deployment context, such as authentication mode, reverse-proxy setup,
  and operating system or container environment.
- Clear reproduction steps or a minimal proof of concept.
- A description of the attacker's required access and the resulting impact.

## Supported versions

| Release channel | Security fixes |
| --- | --- |
| Latest stable release (`main`) | Supported |
| Nightly builds (`develop`) | Reports welcome |
| Older stable releases | No promised backports |

Security fixes target the latest stable release. Reports affecting current
nightly builds on `develop` are welcome so issues can be fixed before the next
stable release.

## Deployment assumptions and trust boundaries

AniList-Arr-Sync is designed as a single-operator, self-hosted service where the
administrator is trusted and controls the host, configuration file, persistent
data directory, backups, and connected media services.

By default, the server listens on `0.0.0.0` with `Forms` authentication and
`DisabledForLocalAddresses`, which bypasses sign-in for loopback, private, and
link-local client addresses. When deploying behind a reverse proxy or exposing
the dashboard remotely, operators are expected to configure authentication, TLS
(directly or at the proxy), `AllowedHosts`, `UrlBase`, and `TrustedNetworks`.
Forwarded client IP headers are only intended to be trusted from configured
proxy networks. See [README.md](README.md#install-and-run) and
[README.md](README.md#runtime-and-data) for listener, proxy, and data-storage
configuration.

Running on a local network does not automatically place a vulnerability out of
scope. Issues reachable from a browser on the same machine or LAN, such as XSS,
CSRF, host-header or DNS-rebinding bypasses, or unexpected cross-device access,
remain relevant.

## Expected security properties

The following properties describe intended behavior rather than guarantees;
deviations are security defects:

- Unauthorized callers cannot read stored credentials or secrets, change
  settings, download or restore backups, trigger updates or lifecycle actions,
  or mutate connected Sonarr and Radarr libraries.
- State-mutating browser requests enforce CSRF protection (session CSRF tokens
  in Forms mode, `X-Requested-With` in Basic auth mode, or a valid API key).
- Backup, restore, and update archive handling respects filesystem boundaries
  and rejects path traversal or writes outside intended paths.
- Built-in updates require cryptographic verification of release checksums,
  signatures, and build identity before replacing the running executable.

## Reportable findings

Reports are evaluated against realistic attacker access and impact. Relevant
classes of findings include:

- Authentication or authorization bypass, including client-IP or proxy-trust
  bypasses.
- Secret or credential disclosure in API responses, logs, UI output, or error
  messages.
- Exploitable cross-site scripting (XSS) or cross-site request forgery (CSRF).
- Unintended server-side requests (SSRF) in service connections, webhooks, or
  metadata handling.
- Path traversal or unintended file read/write in backup, restore, archive, or
  filesystem endpoints.
- Bypass of built-in update verification or release authenticity checks.

## Release verification

See [CODE_SIGNING.md](CODE_SIGNING.md) for download verification steps,
Minisign checksum signatures, Windows Authenticode signing policy, and built-in
updater verification behavior.
