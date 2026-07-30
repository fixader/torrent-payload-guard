# Torrent Payload Guard

Torrent Payload Guard is a small security service that inspects the **actual
file list** of qBittorrent downloads before Sonarr or Radarr can import them.
It acts as the missing safety bridge between qBittorrent and the Arr stack:

```text
qBittorrent metadata → payload validation → pause/tag → Sonarr/Radarr blocklist
```

Release titles are not trusted. A release may look like a normal movie or TV
episode while containing an executable payload such as `.exe` or `.scr`.

The service is a helper designed to prevent qBittorrent from wasting bandwidth
on executable payloads when Sonarr or Radarr requested a movie or TV episode.
Once metadata proves that an Arr download contains a dangerous file, Guard
pauses it before qBittorrent can continue downloading gigabytes of unwanted or
potentially malicious data.

## What it does

- Polls qBittorrent through its Web API.
- Reads the torrent's real file list after metadata becomes available.
- Classifies each payload as `ok`, `suspicious`, or `dangerous`.
- Tags and pauses dangerous downloads before they can be imported.
- Reports matching Sonarr/Radarr downloads as failed and blocklisted.
- Persists decisions in SQLite to prevent duplicate actions and reports.
- Provides a small authenticated dashboard and settings page.
- Exposes `/health` for container health checks.
- Runs safely in `observe` and dry-run mode by default.

Dangerous extensions include:

```text
.exe .scr .bat .cmd .com .msi .msp .ps1 .psm1 .vbs .vbe .js .jse
.wsf .wsh .hta .lnk .jar .reg .dll .apk
```

A dangerous file always wins over an otherwise valid video file. For example,
a torrent containing both `movie.mkv` and `codec.exe` is dangerous.

## Why this exists

qBittorrent knows the file list as soon as torrent metadata is available.
Sonarr and Radarr often do not see that list early enough to prevent a malicious
payload from downloading or reaching the import pipeline. Without a validation
bridge, qBittorrent may spend bandwidth on a fake movie or episode that is
actually an executable.

Torrent Payload Guard connects those systems:

1. qBittorrent receives a download from Sonarr or Radarr.
2. Guard waits for metadata and validates the payload.
3. A dangerous torrent is tagged `payload-dangerous` and paused.
4. Guard finds the matching Arr queue item by download hash.
5. Sonarr or Radarr removes and blocklists the release.

Non-Arr torrents can also be inspected and paused, but are not reported to an
Arr application unless their qBittorrent category or tag matches the configured
mapping.

## Quick start with Docker Compose

```bash
mkdir torrent-payload-guard
cd torrent-payload-guard
curl -O https://raw.githubusercontent.com/fixader/torrent-payload-guard/main/compose.yaml
curl -O https://raw.githubusercontent.com/fixader/torrent-payload-guard/main/.env.example
cp .env.example .env
```

Edit `.env`, then start:

```bash
docker compose up -d
docker compose logs -f torrentguard
```

Open the dashboard at `http://YOUR-NAS-IP:8080/`.

The published image supports `linux/amd64` and `linux/arm64`.

## Synology Container Manager

1. Create `/volume1/docker/torrentguard`.
2. Download `compose.yaml` and `.env.example` into that directory.
3. Copy `.env.example` to `.env` and enter your connection details.
4. In **Container Manager → Project → Create**, select the directory and use
   `compose.yaml`.
5. Start the project.
6. Open `http://YOUR-NAS-IP:8080/`.

On a new installation, Guard opens a one-time setup wizard. It asks you to
create the dashboard administrator and enter the qBittorrent connection.
Sonarr and Radarr can be configured in the wizard or added later. After the
wizard saves `/data/settings.json`, it is disabled and every dashboard and
settings route requires authentication.

Complete initial setup only from a trusted local network. Whoever completes
the wizard first becomes the administrator.

If qBittorrent, Sonarr, and Radarr use host networking, use loopback addresses
such as `http://127.0.0.1:9865`, `http://127.0.0.1:8989`, and
`http://127.0.0.1:7878`, and keep `network_mode: host`.

If they share a regular Docker network, use their service names instead, such
as `http://qbittorrent:8080`.

No privileged mode or Docker socket mount is required.

## Safe first run

Always begin in observation mode:

```env
ACTION_MODE=observe
DRY_RUN=true
DELETE_DATA=false
```

Review the dashboard and logs. When the classifications look correct, enable
reversible protection:

```env
ACTION_MODE=pause
DRY_RUN=false
```

Deletion must be enabled explicitly:

```env
ACTION_MODE=delete
DRY_RUN=false
DELETE_DATA=false
```

Suspicious torrents are tagged but never deleted automatically.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `QBIT_URL` | empty | qBittorrent Web UI base URL; blank starts the setup wizard |
| `QBIT_USERNAME` | empty | qBittorrent username |
| `QBIT_PASSWORD` | empty | qBittorrent password |
| `SONARR_URL` | empty | Sonarr base URL |
| `SONARR_API_KEY` | empty | Sonarr API key |
| `RADARR_URL` | empty | Radarr base URL |
| `RADARR_API_KEY` | empty | Radarr API key |
| `POLL_INTERVAL_SECONDS` | `30` | Polling interval |
| `ACTION_MODE` | `observe` | `observe`, `pause`, or `delete` |
| `DRY_RUN` | `true` | Prevents external changes |
| `DELETE_DATA` | `false` | Deletes payload data in delete mode |
| `PAUSE_UNMAPPED` | `true` | Pause/delete dangerous non-Arr torrents; when false they are tag-only |
| `SONARR_CATEGORIES` | `sonarr` | Comma-separated qBit categories/tags |
| `RADARR_CATEGORIES` | `radarr` | Comma-separated qBit categories/tags |
| `DANGEROUS_EXTENSIONS` | built in | Comma-separated extension list |
| `UI_USERNAME` | `admin` | Dashboard username |
| `UI_PASSWORD` | empty | Dashboard password; blank on a new install starts the setup wizard |
| `DATABASE_PATH` | `/data/torrentguard.db` | Persistent SQLite path |
| `SETTINGS_PATH` | `/data/settings.json` | Dashboard settings path |
| `LISTEN_ADDRESS` | `:8080` | Dashboard listen address |

Secrets are never returned by the settings API and are not written to logs.
Leave a secret field blank on the settings page to retain its current value.
Existing environment-based installations continue to start normally when
qBittorrent details and `UI_PASSWORD` are already configured.

## Endpoints

- `/` — authenticated dashboard
- `/settings` — authenticated integration settings
- `/status` — authenticated counters and operating mode
- `/api/torrents` — authenticated recent classification records
- `/health` — unauthenticated container health check

## Development

Requires Go 1.24:

```bash
go test ./...
go vet ./...
go run ./cmd/torrentguard
```

Build locally:

```bash
docker build -t torrent-payload-guard .
```

## Security model and limitations

- Guard cannot inspect a torrent until qBittorrent has received its metadata.
- Arr feedback is best-effort because queue entries may appear asynchronously.
  Temporary failures and missing queue items are retried.
- Protect port `8080` with `UI_PASSWORD`, a firewall, or a trusted reverse
  proxy. Do not expose the dashboard directly to the public internet.
- Keep dry-run enabled until you have validated behavior in your environment.
- Back up `/data` before upgrades.

Security reports should be submitted privately through GitHub Security
Advisories rather than public issues.

## Disclaimer

THIS SOFTWARE IS PROVIDED **AS IS**, WITHOUT WARRANTY OF ANY KIND. It may pause,
remove, or blocklist downloads when configured to do so. The authors and
contributors accept no responsibility for lost data, interrupted downloads,
security incidents, service outages, false positives, or any other damages.

**You use this software entirely at your own risk.**

See [LICENSE](LICENSE) and [NOTICE](NOTICE) for the complete legal terms.

## License

Licensed under the Apache License 2.0. You may use, modify, and redistribute the
software, provided that the required copyright and attribution notices are
preserved. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
