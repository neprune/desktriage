# DeskTriage

A Go web application for managing Freshdesk support tickets. Provides a dashboard with focus/triage views, per-ticket local state tracking, and a detailed ticket view with conversation threading.

## Building and Running

Build with `make build`, then run `./desktriage`. The server listens on port 8000 by default.

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

To restart after code changes:

```bash
make build
sudo systemctl restart srv
```

## Configuration

Create a `.env` file with:

- `FRESHDESK_URL` — your Freshdesk base URL
- `FRESHDESK_KEY` — API key for Basic Auth
- `FRESHDESK_COOKIE` — session cookie for browser-proxied requests

## Database

Uses SQLite (`db.sqlite3`). SQL queries are managed with sqlc.

## Code layout

- `cmd/desktriage`: main package (binary entrypoint)
- `srv`: HTTP server logic (handlers)
- `srv/templates`: Go HTML templates
- `srv/static`: CSS and JavaScript
- `db`: SQLite open + migrations
- `freshdesk`: API client (read-only)
- `cache`: SQLite-backed HTTP cache with ETag support
- `config`: Environment configuration
