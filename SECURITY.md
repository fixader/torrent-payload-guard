# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities privately through GitHub Security
Advisories for this repository. Do not include credentials, API keys, torrent
metadata, private tracker URLs, or personal media history in a public issue.

## Deployment guidance

- Configure `UI_PASSWORD`.
- Keep the dashboard on a trusted network or behind an authenticated reverse
  proxy.
- Do not expose qBittorrent, Sonarr, Radarr, or Torrent Payload Guard directly
  to the public internet.
- Start with `ACTION_MODE=observe` and `DRY_RUN=true`.
- Protect the persistent `/data` volume because it contains operational state
  and may contain saved integration credentials.

This project is provided as-is without warranty. Use it at your own risk.
