# Changelog

All notable user-visible changes are documented here. Version numbers follow
[Semantic Versioning](https://semver.org/).

## [0.3.1] - 2026-10-10

### Documentation

- Added a complete explanation of qBittorrent per-file selection and priority
  `0` (**Do not download**) handling.
- Documented automatic rechecking, dangerous-tag removal, and resume behavior
  after a dangerous file is deselected.
- Documented the persistent `payload-allowed` override, its dashboard workflow,
  and its safety limitations.
- Added the historical 99-byte `RARBG_DO_NOT_MIRROR.exe` marker as a practical
  example without weakening extension-based protection or trusting filenames.
- Documented the authenticated allow API endpoint.

There are no runtime changes from 0.3.0 in this patch release.

## [0.3.0] - 2026-10-10

### Added

- qBittorrent per-file priority support. Files explicitly marked **Do not
  download** no longer affect payload classification.
- Continuous rechecking of cached dangerous torrents so changed file selections
  take effect without deleting Guard's history.
- Automatic removal of `payload-dangerous` and automatic resume when the
  remaining selected payload becomes safe.
- A persistent per-torrent override using the `payload-allowed` qBittorrent tag.
- An authenticated `POST /api/torrents/{hash}/allow` endpoint.
- Dashboard **Allowed** filter and **Allow & resume** action with confirmation.

### Changed

- Dashboard history increased to 500 entries.
- Dangerous, suspicious, and allowed records are ordered ahead of ordinary OK
  records, keeping actionable downloads visible in large queues.
- The dashboard records whether a torrent was automatically released or
  explicitly allowed.

### Safety behavior

- Overrides are scoped to one torrent hash and persist in qBittorrent tags.
- Overrides do not reverse an Arr blocklist that has already completed.
- Filename-based global exceptions were deliberately avoided because malicious
  payloads can imitate a known harmless filename.

## [0.2.1] - 2026-10-09

### Fixed

- Enabled SQLite WAL mode and a busy timeout to prevent polling from becoming
  permanently stuck after transient database contention.

## [0.2.0] - 2026-09-13

### Added

- qBittorrent API-key authentication with password-login compatibility.
- First-run setup, authenticated dashboard, settings management, and standalone
  operation without Sonarr or Radarr.

[0.3.1]: https://github.com/fixader/torrent-payload-guard/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/fixader/torrent-payload-guard/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/fixader/torrent-payload-guard/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/fixader/torrent-payload-guard/compare/v0.1.3...v0.2.0
