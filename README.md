# DeskTriage

> [!CAUTION]
> **This is 100% vibe-coded slop.** It was built entirely through AI-assisted prompting with no human code review, no design process, and no quality standards whatsoever. It should not be used by anyone, for any reason, ever. If you are reading this, close the tab. If you have already deployed it, stop what you are doing and delete it immediately. You have been warned.

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

### Command Palette

Press `Cmd+Shift+P` (Mac) or `Ctrl+Shift+P` to open the command palette. Supports fuzzy filtering and includes:

- Navigation (go to dashboard, today view, ticket by number)
- Ticket actions (defer, today, handover, status changes)
- Hard refresh

### Handover

Hand a ticket off to another agent: adds a private note to Freshdesk, unassigns the ticket, and clears all local state. Available from the ticket detail page and the command palette.

### Auto-Assign

Marking a ticket as "Today" automatically assigns it to the current agent on Freshdesk (skipped if already assigned).

### Keyboard Shortcuts

- `j`/`k` or `↑`/`↓` — navigate between tickets
- `Space` — toggle inline preview
- `Enter` — open ticket detail
- `Opt+Enter` — open ticket in Freshdesk
- `d` — defer to tomorrow (skips weekends)
- `t` — flag for today
- `b` — toggle blocked
- `?` — show keyboard shortcuts help
- `Cmd+Shift+P` / `Ctrl+Shift+P` — command palette

### Other Features

- **Sprint awareness** — configurable sprint anchors, sprint badges on tickets
- **Account managers** — associate Freshdesk agents with companies
- **PWA support** — installable as a Progressive Web App
- **Dark mode** — automatic, with Lucide icons throughout
- **SIGHUP restart** — graceful re-exec for zero-downtime updates
- **Private notes** — compose and send private notes from the detail page, with draft persistence in localStorage

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
| `freshdesk` | API client (tickets, notes, status, assignment) |
| `cache` | SQLite-backed HTTP cache with ETag support |
| `config` | DB-backed configuration with `.env` seeding |
| `sprint` | Sprint date calculation from anchors |
| `docs` | Security audit and review documents |
