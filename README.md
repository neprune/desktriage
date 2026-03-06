# DeskTriage

A Go web application for managing Freshdesk support tickets. Provides a dashboard with prioritised sections (Today, Review Now, For Later, Historic), a Today focus view, per-ticket local state tracking, and a detailed ticket view with conversation threading.

## Building and Running

Build with `make build`, then run `./desktriage`. The server listens on port 8086 by default.

Send SIGHUP to gracefully restart the process (re-exec) without downtime.

## Running as a systemd service

To run the server as a systemd service:

```bash
# Install the service file
sudo cp srv.service /etc/systemd/system/srv.service

# Reload systemd and enable the service
sudo systemctl daemon-reload
sudo systemctl enable srv.service

# Start the service
sudo systemctl start srv

# Check status
systemctl status srv

# View logs
journalctl -u srv -f
```

To update from git and rebuild (skips build if already up to date):

```bash
make update
```

Or manually:

```bash
make build
sudo systemctl restart srv
```

## Configuration

Configuration is stored in the SQLite database and managed through the admin UI at `/admin/config`. On first run, values are seeded from a `.env` file if present.

| Key | Description |
|-----|-------------|
| `freshdesk_url` | Freshdesk API base URL (required) |
| `freshdesk_key` | API key for Basic Auth (required) |
| `session_password` | Password for DeskTriage login (blank disables auth) |

The `.env` file format uses `FRESHDESK_URL`, `FRESHDESK_KEY`, and `SESSION_SECRET` as keys.

## Authentication & Security

- **Password auth** — optional session-based login, controlled by the `session_password` config key.
- **CSRF protection** — all mutating requests require a valid CSRF token.
- **Rate limiting** — login attempts are rate-limited.
- **Secure cookies** — `Secure` flag set by default (with localhost exemption for development).

## Features

### Dashboard

The main dashboard groups tickets into collapsible sections:

- **Today** — tickets flagged for immediate attention
- **Review Now** — deferred tickets whose review date has arrived, plus tickets where the client has replied since deferral
- **For Later** — tickets deferred to a future date
- **Historic** — older tickets for reference

### Today View

A focused view showing only Today tickets, with keyboard navigation.

### Ticket Detail

- Full conversation thread with quoted-reply folding and auto-collapse of old conversations
- Inline display of image and PDF attachments
- Private note composition with draft persistence in localStorage (htmx submission)
- Freshdesk status changes directly from the detail page (htmx)
- Local state editing (flag, defer, notes)

### Keyboard Shortcuts

- `j`/`k` or `↑`/`↓` — navigate between tickets
- `Space` — toggle preview
- `Enter` — open ticket detail
- `Opt+Enter` — open ticket in Freshdesk
- `d` — defer to tomorrow (skips weekends)
- `t` — flag for today

### Other Features

- **Sprint awareness** — configurable sprint anchors, sprint badges on tickets
- **Account managers** — associate Freshdesk agents with companies
- **PWA support** — installable as a Progressive Web App
- **Dark mode** — automatic, with Lucide icons throughout
- **SIGHUP restart** — graceful re-exec for zero-downtime updates

## Database

Uses SQLite (`db.sqlite3`). Migrations are in `db/migrations/`. SQL queries are managed with [sqlc](https://sqlc.dev).

## Code Layout

| Directory | Purpose |
|-----------|--------|
| `cmd/desktriage` | Main package (binary entrypoint) |
| `srv` | HTTP server, handlers, auth, CSRF, rate limiting |
| `srv/templates` | Go HTML templates |
| `srv/static` | CSS, JavaScript, PWA manifest and icons |
| `db` | SQLite open, migrations |
| `db/dbgen` | sqlc-generated query code |
| `freshdesk` | API client (read-only + note/status mutations) |
| `cache` | SQLite-backed HTTP cache with ETag support |
| `config` | DB-backed configuration with `.env` seeding |
| `sprint` | Sprint date calculation from anchors |
| `docs` | Security audit and review documents |
