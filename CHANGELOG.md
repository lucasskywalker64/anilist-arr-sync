# Changelog

All notable changes to AniList-Arr-Sync are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v1.0.0] - 2026-11-27

Initial stable release of AniList-Arr-Sync.

### Added

- AniList synchronization to Sonarr (series) and Radarr (movies) with separate
  status selection, scheduled and manual runs, and one-shot `--sync` CLI mode.
- Title matching using Fribb community anime mappings, with manual overrides,
  ignored titles, and country-of-origin quality profile overrides.
- Dry-run preview and staged action queue to inspect, apply, or reject proposed
  library additions and monitoring changes before modifying Sonarr or Radarr.
- Embedded web dashboard with a first-run setup wizard, ambiguous match and
  episode/season review flow, synchronization history, unsaved-changes guard,
  and in-app restart and shutdown controls.
- Forms and Basic authentication, API key support, configurable local-network
  authentication bypass, CSRF protection, `AllowedHosts` checks, reverse-proxy
  (`UrlBase`, `TrustedNetworks`) support, and optional HTTPS listener.
- Online SQLite and configuration zip backups with rolling retention and
  restore support.
- Discord and custom webhook notifications.
- Built-in updater with Minisign checksum signature verification, release
  identity checks, and automatic rollback on startup or health-check failure.
- Portable executables for Linux and Windows (`amd64`, `arm64`), Windows
  installer with service and console-free tray modes (plus optional app-data
  removal during uninstall), and Linux container images.

[Unreleased]: https://github.com/lucasskywalker64/anilist-arr-sync/compare/v1.0.0...HEAD
[v1.0.0]: https://github.com/lucasskywalker64/anilist-arr-sync/releases/tag/v1.0.0
