# DeskTriage — Implementation Plan

## Tech Stack

- **Backend:** Go (HTTP handlers, SQLite, migrations, systemd)
- **Frontend:** Server-rendered HTML templates + vanilla JS (htmx for partial updates)
- **Database:** SQLite — stores only private ticket state (priority, blocked, note, review-after, today flag) and a Freshdesk response cache
- **Freshdesk integration:** REST API v2 via Go HTTP client, authenticated with API key from `.env`

## Architecture Overview

```
Browser  ←→  Go server (:8000)
                ├── HTML templates (dashboard, focus, triage, ticket views)
                ├── SQLite (private state + cache)
                └── Freshdesk API (proxied/cached)
```

The Go server acts as a BFF (backend-for-frontend). It fetches from Freshdesk, merges in local private state, and serves pre-rendered HTML partials. This keeps the frontend simple and avoids CORS/auth issues.

## Data Model

### SQLite Tables

```sql
-- Private per-ticket state (source of truth for these fields only)
ticket_state (
    ticket_id     INTEGER PRIMARY KEY,
    priority      INTEGER DEFAULT 0,       -- local queue priority (0 = unqueued)
    blocked       BOOLEAN DEFAULT FALSE,
    note          TEXT DEFAULT '',
    review_after  TIMESTAMP,               -- nullable
    today         BOOLEAN DEFAULT FALSE,
    updated_at    TIMESTAMP NOT NULL
)

-- Freshdesk API response cache
api_cache (
    cache_key     TEXT PRIMARY KEY,
    response_body TEXT NOT NULL,
    etag          TEXT,
    cached_at     TIMESTAMP NOT NULL,
    max_age_secs  INTEGER NOT NULL          -- TTL hint
)
```

### Freshdesk Entities Used

- **Tickets** — `GET /api/v2/tickets` with filters (agent, status, etc.)
- **Ticket detail** — `GET /api/v2/tickets/{id}`
- **Conversations** — `GET /api/v2/tickets/{id}/conversations`
- **Reply** — `POST /api/v2/tickets/{id}/reply`
- **Companies** — `GET /api/v2/companies/{id}` (for company name lookup)
- **Contacts** — `GET /api/v2/contacts/{id}` (to resolve requester → company)

## Freshdesk Client & Caching Strategy

1. **Short-lived cache with background refresh.** API responses are cached in SQLite with a short TTL (e.g. 30s for ticket lists, 60s for company lookups). On cache hit within TTL, serve from cache. On miss or stale, fetch from Freshdesk (using `If-None-Match` / ETag where supported) and update cache.
2. **User-triggered refresh.** A "Refresh" button on the dashboard forces a cache bust.
3. **Lazy company resolution.** Company names are resolved from contact → company and cached longer (5 min) since they change rarely.
4. **Rate limiting awareness.** Freshdesk rate limits are respected; the client reads `X-RateLimit-Remaining` headers and backs off.

## Pages / Views

### 1. Dashboard (main page) — `GET /`

- Two sections: **My Tickets** (assigned to agent) and **Unassigned** (open, unassigned)
- Each ticket rendered as a card showing:
  - Title
  - Company name
  - Status badge
  - Time since last activity (relative, e.g. "3h ago")
  - Who last updated (agent vs. client indicator icon)
  - Private state badges: priority position, blocked flag, today flag, review-after date
- Tickets in the priority queue are ordered by priority rank; unqueued tickets follow
- Inline controls (htmx): set/change priority, toggle blocked, toggle today, set review-after, edit note

### 2. Focus Mode — `GET /focus`

- Shows only tickets with `today = true`
- Same card format, ordered by priority
- Quick way to work through the day's list

### 3. Triage Mode — `GET /triage`

- Shows tickets that are newly opened (status = open) and have no local state yet (no row in `ticket_state` or priority = 0)
- Streamlined UI for quick triage: assign priority, flag for today, mark blocked, add note, or dismiss

### 4. Ticket View — `GET /ticket/{id}`

- Full ticket detail + conversation thread
- Private state editing sidebar
- Reply form at the bottom (posts via `POST /ticket/{id}/reply` → Freshdesk API)

## API Endpoints (server-side)

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Dashboard HTML |
| GET | `/focus` | Focus mode HTML |
| GET | `/triage` | Triage mode HTML |
| GET | `/ticket/{id}` | Ticket detail HTML |
| POST | `/ticket/{id}/reply` | Send reply via Freshdesk |
| PUT | `/ticket/{id}/state` | Update private state (htmx partial) |
| POST | `/refresh` | Bust cache, redirect back |

## Implementation Phases

### Phase 1 — Scaffold & Freshdesk Client
1. Unpack Go template, init git repo
2. Set up `.env` loading (godotenv or manual)
3. Implement Freshdesk API client (tickets list, ticket detail, conversations, reply, companies, contacts)
4. Implement SQLite cache layer
5. DB migration for `ticket_state` and `api_cache` tables

### Phase 2 — Dashboard
1. Fetch assigned + unassigned tickets, merge with local state
2. Resolve company names
3. Compute "last activity" and "who updated last" from ticket/conversation data
4. Render dashboard template with ticket cards
5. Style with CSS (clean, functional desktop layout)

### Phase 3 — Private State & Interactivity
1. htmx-powered inline editing of priority, blocked, today, review-after, note
2. PUT `/ticket/{id}/state` handler returning updated card partial
3. Priority queue ordering logic

### Phase 4 — Focus & Triage Modes
1. Focus mode (filter `today = true`)
2. Triage mode (new/unprocessed tickets)
3. Navigation bar linking all views

### Phase 5 — Ticket View & Reply
1. Ticket detail page with conversation thread
2. Reply form + Freshdesk API integration
3. HTML rendering of conversation bodies (Freshdesk stores HTML)

### Phase 6 — Polish
1. Auto-refresh with htmx polling (every 60s on dashboard)
2. Loading states and error handling
3. Keyboard shortcuts (j/k navigation, t for today, etc.)
4. Responsive tweaks for large screens
5. systemd service setup

## Key Design Decisions

- **Server-rendered + htmx** over SPA: simpler, faster to build, no build toolchain, good UX with partial swaps
- **SQLite** for local state: zero-ops, perfect for single-user app
- **BFF pattern**: all Freshdesk calls go through the Go server, never from the browser directly
- **No auth in the app**: per spec, credentials are in `.env`; this is a single-user tool
- **Read-only Freshdesk access**: only GET requests to the Freshdesk API; no POST/PUT/PATCH/DELETE
- **Cache in DB, not memory**: survives restarts, easy to inspect/debug

## Detailed TODO List

### Phase 1 — Scaffold & Freshdesk Client

- [x] Unpack Go template into project directory
- [x] `git init` and initial commit
- [x] Remove template boilerplate (visitors table, welcome page, etc.)
- [x] Add `.env` to `.gitignore`
- [x] Implement `.env` file loader (parse key=value, populate config struct)
- [x] Define config struct: `FreshdeskURL`, `FreshdeskKey`, `FreshdeskCookie`, `AgentName`
- [x] Write DB migration `001-base.sql`: `ticket_state` table, `api_cache` table, `migrations` table
- [x] Generate sqlc queries for `ticket_state` CRUD and `api_cache` get/set/delete
- [x] Create `freshdesk/client.go`: HTTP client struct holding base URL + API key
- [x] Implement `client.ListTickets(filter string)` — `GET /api/v2/tickets?...`
- [x] Implement `client.GetTicket(id int64)` — `GET /api/v2/tickets/{id}`
- [x] Implement `client.GetConversations(ticketID int64)` — `GET /api/v2/tickets/{id}/conversations`
- [x] Implement `client.ReplyToTicket(ticketID int64, body string)` — `POST /api/v2/tickets/{id}/reply`
- [x] Implement `client.GetCompany(id int64)` — `GET /api/v2/companies/{id}`
- [x] Implement `client.GetContact(id int64)` — `GET /api/v2/contacts/{id}`
- [x] Define Go structs for Freshdesk JSON responses: `Ticket`, `Conversation`, `Company`, `Contact`
- [x] Implement cache layer: `CachedGet(key, fetcher, maxAge)` using `api_cache` table
- [ ] Handle ETag: store on cache write, send `If-None-Match` on re-fetch, handle 304
- [x] Read and log `X-RateLimit-Remaining` header; back off if low
- [x] Handle Freshdesk pagination (`page`, `per_page` params; `link` header)
- [x] Write tests for Freshdesk client (mock HTTP server)
- [x] Write tests for cache layer
- [x] Commit: "Phase 1: scaffold, Freshdesk client, cache layer"

### Phase 2 — Dashboard

- [x] Identify agent's Freshdesk user ID (fetch `/api/v2/agents/me` on startup, cached 1hr)
- [x] Implement `fetchDashboardData()`: fetch assigned tickets + unassigned open tickets
- [x] For each ticket, resolve company name: ticket → company (cached 5min, concurrent bounded to 5)
- [x] Compute "time since last activity" from `ticket.updated_at`
- [x] Determine "who last updated": compare last conversation's `incoming` field
- [x] Merge Freshdesk ticket data with local `ticket_state` rows
- [x] Define template data struct: `DashboardData` with `AssignedTickets`, `UnassignedTickets`
- [x] Define `TicketCard` struct: merged Freshdesk + local fields
- [x] Sort assigned tickets: queued (by priority rank) first, then unqueued (by last activity)
- [x] Create base HTML layout template: nav bar, main content area, common CSS/JS includes
- [x] Create `dashboard.html` template: two-section layout (My Tickets + Unassigned)
- [x] Create ticket card markup (inline in dashboard.html; partial extraction deferred to Phase 3)
- [x] Register `GET /` handler rendering dashboard
- [x] Write CSS: card styles, status badge colors, layout grid, typography
- [x] Add htmx via CDN `<script>` tag in base layout
- [x] Add "Refresh" button → `POST /refresh` → bust cache → redirect to `/`
- [x] Also fetch pending tickets assigned to agent (search API, status:3)
- [x] Test: start server, verify dashboard renders with real Freshdesk data
- [x] Commit: "Phase 2: dashboard with ticket cards"

### Phase 3 — Private State & Interactivity

- [x] Implement `PUT /ticket/{id}/state` handler: parse form fields, upsert `ticket_state` row
- [x] Return updated `_ticket_card.html` partial for htmx swap
- [x] Add priority controls to card: "Add to queue" (📌) / "Remove from queue" (✖) buttons
- [x] Implement priority queue logic: set priority=1 on add, priority=0 on remove
- [x] Add "Today" toggle button on card (htmx `hx-put`) — ⭐ icon
- [x] Add "Blocked" toggle button on card (htmx `hx-put`) — 🚫 icon
- [x] Add "Review after" date picker on card (htmx `hx-put`) — 📅 icon with clear button
- [x] Add inline "Note" field: click 📝 to reveal, Enter to save (htmx `hx-put`)
- [x] Visual indicators: today → blue badge, blocked → red badge + dimmed, review-after → orange badge
- [x] Grey out / de-emphasize tickets where `review_after` is in the future (is-deferred class)
- [x] Test all state mutations: 10 tests for create, update, toggle, preserve fields, invalid input
- [x] Commit: "Phase 3: private state editing with htmx"

### Phase 4 — Focus & Triage Modes

- [x] Implement `GET /focus` handler: query tickets where `today = true`, merge with Freshdesk data
- [x] Create `focus.html` template: streamlined single-column list, ordered by priority
- [x] Show empty state message when no tickets flagged for today
- [x] Allow toggling "today" off from focus view (card disappears via htmx outerHTML → empty)
- [x] Implement `GET /triage` handler: fetch open tickets with no `ticket_state` row (or all-default state)
- [x] Create `triage.html` template: dedicated triage-card partial with quick-action buttons
- [x] Triage actions per ticket: ⭐ Today, 📌 Queue, 🚫 Blocked, ✔ Dismiss (priority=-1)
- [x] After triage action, card removed from view (from=triage returns empty HTML)
- [x] Add nav bar links: Dashboard, Focus, Triage (highlight active page via .active class)
- [x] Add ticket count badges in nav bar (total, today count, triage count)
- [x] Extracted `fetchAllTicketCards()` shared method, `isTriageable()` filter logic
- [x] Test dismiss, from-triage, from-focus empty responses (13 handler tests total)
- [x] Commit: "Phase 4: focus and triage modes"

### Phase 5 — Ticket View & Reply

- [x] Implement `GET /ticket/{id}` handler: fetch ticket detail + conversations + local state
- [x] Create `ticket.html` template: ticket header, metadata sidebar, conversation thread
- [x] Render conversation entries: sender name, timestamp, HTML body
- [x] Visually distinguish agent replies vs. customer messages (color-coded left borders)
- [x] Visually distinguish private notes (yellow background)
- [x] Resolve sender names (contacts + agents fallback, cached 5min)
- [x] Add `Attachment` struct to Freshdesk types (Ticket + Conversation)
- [x] Private state editing sidebar with htmx (`_ticket_state.html` partial)
- [x] State handler returns sidebar partial when `from=ticket`
- [N/A] Reply form — Freshdesk API is READ-ONLY, no POST/PUT/PATCH/DELETE
- [x] "Back to dashboard" link
- [x] Make ticket title on dashboard cards a link to `/ticket/{id}`
- [x] Lightweight nav badge counts via `fetchNavCounts()` (reuses cached data)
- [x] Add `GetAgent()` to Freshdesk client for agent name resolution
- [x] Tests: from=ticket state partial (2 new tests, 15 total)
- [x] Responsive layout (grid stacks on narrow screens)
- [x] Commit: "Phase 5: ticket detail view and reply"

### Phase 6 — Polish

- [x] Add htmx polling on dashboard: `hx-trigger="every 60s"` on ticket list container
- [x] Add loading indicator (htmx `hx-indicator`) for all async operations
- [x] Error handling: display user-friendly messages when Freshdesk API fails
- [x] Error handling: graceful degradation if cache is stale and API is unreachable
- [x] Keyboard shortcuts: `j`/`k` to navigate tickets, `t` toggle today, `b` toggle blocked, `Enter` to open ticket
- [x] Keyboard shortcut help overlay (`?` to show)
- [x] Add `<title>` tags per page (e.g. "DeskTriage — Focus (3)")
- [x] Favicon
- [x] Responsive layout tweaks: ensure usable on wide monitors (max-width, readable line lengths)
- [x] Add systemd service file, wire up `make build`
- [x] Deploy: `sudo cp srv.service /etc/systemd/system/ && sudo systemctl daemon-reload && sudo systemctl enable --now srv`
- [x] Smoke test deployed service via `https://aggtivity.exe.xyz:8000/`
- [x] Final commit: "Phase 6: polish, keyboard shortcuts, systemd deploy"
