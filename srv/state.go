package srv

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"desktriage.davea.me/db/dbgen"
	"desktriage.davea.me/freshdesk"
	"desktriage.davea.me/sprint"
)

// HandleUpdateState handles PUT /ticket/{id}/state.
// It updates a single field of the ticket's local state and returns
// the updated ticket card partial for htmx swap.
func (s *Server) HandleUpdateState(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	ticketID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid ticket ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form data", http.StatusBadRequest)
		return
	}

	field := r.FormValue("field")
	value := r.FormValue("value")

	// Load existing state (or defaults)
	existing, err := s.Queries.GetTicketState(r.Context(), ticketID)
	if err == sql.ErrNoRows {
		existing = dbgen.TicketState{
			TicketID: ticketID,
		}
	} else if err != nil {
		slog.Error("get ticket state", "ticket_id", ticketID, "error", err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Apply field update
	switch field {
	case "today":
		if value == "toggle" {
			if existing.Today != 0 {
				value = "0"
			} else {
				value = "1"
			}
		}
		if value == "1" {
			existing.Today = 1
			existing.ReviewAfter = nil // clear any pending review date
			existing.DeferredAt = nil
			// Assign ticket to current agent on Freshdesk.
			if err := s.assignToSelf(r.Context(), ticketID); err != nil {
				slog.Error("assign ticket on today", "ticket_id", ticketID, "error", err)
			}
		} else {
			existing.Today = 0
		}
	case "blocked":
		if value == "1" {
			existing.Blocked = 1
		} else {
			existing.Blocked = 0
		}
	case "note":
		existing.Note = value
	case "review_after":
		if value == "" {
			existing.ReviewAfter = nil
			existing.DeferredAt = nil
		} else {
			// Parse date input (YYYY-MM-DD) and store as RFC3339
			t, pErr := time.Parse("2006-01-02", value)
			if pErr != nil {
				http.Error(w, "invalid date", http.StatusBadRequest)
				return
			}
			rs := t.Format(time.RFC3339)
			existing.ReviewAfter = &rs
			nowStr := time.Now().UTC().Format(time.RFC3339)
			existing.DeferredAt = &nowStr
		}
	case "priority":
		p, pErr := strconv.ParseInt(value, 10, 64)
		if pErr != nil {
			http.Error(w, "invalid priority", http.StatusBadRequest)
			return
		}
		existing.Priority = p
	case "dismiss":
		// Create a state row with priority=-1 so it's no longer triageable
		// but not in the active queue either.
		existing.Priority = -1
	case "defer_tomorrow":
		// Unset today and set review_after to next weekday.
		existing.Today = 0
		next := nextWeekday(time.Now())
		tomorrow := next.Format(time.RFC3339)
		existing.ReviewAfter = &tomorrow
		nowStr := time.Now().UTC().Format(time.RFC3339)
		existing.DeferredAt = &nowStr
	case "defer_next_sprint":
		// Unset today and set review_after to the start of the next sprint.
		existing.Today = 0
		anchors := s.loadSprintAnchors(r.Context())
		nextStart, ok := sprint.NextStart(anchors, time.Now())
		if !ok {
			http.Error(w, "no sprint anchors configured", http.StatusBadRequest)
			return
		}
		ns := nextStart.Format(time.RFC3339)
		existing.ReviewAfter = &ns
		nowStr := time.Now().UTC().Format(time.RFC3339)
		existing.DeferredAt = &nowStr
	default:
		http.Error(w, "unknown field: "+field, http.StatusBadRequest)
		return
	}

	// Upsert
	now := time.Now().UTC().Format(time.RFC3339)
	err = s.Queries.UpsertTicketState(r.Context(), dbgen.UpsertTicketStateParams{
		TicketID:    ticketID,
		Priority:    existing.Priority,
		Blocked:     existing.Blocked,
		Note:        existing.Note,
		ReviewAfter: existing.ReviewAfter,
		Today:       existing.Today,
		UpdatedAt:   now,
		DeferredAt:  existing.DeferredAt,
	})
	if err != nil {
		slog.Error("upsert ticket state", "ticket_id", ticketID, "error", err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// If the request came from the today page (toggling today off),
	// return empty HTML to remove the card from the list.
	from := r.FormValue("from")
	if from == "today" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		return
	}

	// If the request came from the ticket detail page, render the
	// ticket-state sidebar partial.
	if from == "ticket" {
		stateData := s.buildTicketStateData(r.Context(), ticketID)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.renderPartialCtx(r.Context(), w, "ticket-state", stateData); err != nil {
			slog.Warn("render ticket-state partial", "error", err)
		}
		return
	}

	// Build a TicketCard to render the partial.
	card, err := s.buildTicketCard(r.Context(), ticketID)
	if err != nil {
		slog.Error("build ticket card", "ticket_id", ticketID, "error", err)
		http.Error(w, "failed to build card", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderPartialCtx(r.Context(), w, "ticket-card", card); err != nil {
		slog.Warn("render ticket card partial", "error", err)
	}
}

// buildTicketStateData constructs a TicketStateData for the ticket-state partial.
func (s *Server) buildTicketStateData(ctx context.Context, ticketID int64) *TicketStateData {
	data := &TicketStateData{ID: ticketID}

	st, err := s.Queries.GetTicketState(ctx, ticketID)
	if err == nil {
		data.Today = st.Today != 0
		data.Blocked = st.Blocked != 0
		data.Note = st.Note
		data.LocalPriority = int(st.Priority)
		if st.ReviewAfter != nil {
			if t, pErr := time.Parse(time.RFC3339, *st.ReviewAfter); pErr == nil {
				data.ReviewAfter = &t
				if t.After(time.Now()) {
					data.ReviewDeferred = true
				}
			}
		}
	} else if err != sql.ErrNoRows {
		slog.Warn("get ticket state for partial", "ticket_id", ticketID, "error", err)
	}

	return data
}

// findCachedTicket looks up a ticket from the cached dashboard data.
func (s *Server) findCachedTicket(ticketID int64) *freshdesk.Ticket {
	var tickets []freshdesk.Ticket
	ok, _ := s.Cache.GetJSON(context.Background(), "tickets:dashboard", &tickets)
	if !ok {
		return nil
	}
	for i := range tickets {
		if tickets[i].ID == ticketID {
			return &tickets[i]
		}
	}
	return nil
}

// buildTicketCard constructs a TicketCard for a single ticket by looking it
// up from the cached dashboard ticket list (or fetching directly) and merging
// with fresh DB state.
func (s *Server) buildTicketCard(ctx context.Context, ticketID int64) (*TicketCard, error) {
	found := s.findCachedTicket(ticketID)

	// If not in cache, fetch directly from Freshdesk
	if found == nil && s.Freshdesk != nil {
		ticket, err := s.Freshdesk.GetTicket(ctx, ticketID)
		if err != nil {
			return nil, fmt.Errorf("fetch ticket %d: %w", ticketID, err)
		}
		found = ticket
	}
	if found == nil {
		return nil, fmt.Errorf("ticket %d not found in cache or API", ticketID)
	}

	// Resolve company name
	companyName := s.resolveCompanyName(ctx, found.CompanyID)

	// Determine last updater
	lastUpdater := s.determineLastUpdater(ctx, *found)

	card := &TicketCard{
		ID:          found.ID,
		Subject:     found.Subject,
		Status:      found.Status,
		StatusLabel: s.statusLabel(ctx, found.Status),
		CompanyName: companyName,
		UpdatedAt:   found.UpdatedAt,
		TimeSince:   timeSince(found.UpdatedAt),
		LastUpdater: lastUpdater,
		IsAssigned:  found.ResponderID == s.AgentID,
		CreatedAt:   found.CreatedAt,
	}

	// Sprint info
	anchors := s.loadSprintAnchors(ctx)
	if len(anchors) > 0 {
		if currentSp, ok := sprint.Resolve(anchors, time.Now()); ok {
			if ticketSp, ok2 := sprint.Resolve(anchors, found.CreatedAt); ok2 {
				card.SprintLabel = fmt.Sprintf("S%d", ticketSp.Number)
				card.IsCurrentSprint = ticketSp.Year == currentSp.Year && ticketSp.Number == currentSp.Number
			}
		}
	}

	// Merge local state
	st, err := s.Queries.GetTicketState(ctx, ticketID)
	if err == nil {
		card.Priority = int(st.Priority)
		card.Blocked = st.Blocked != 0
		card.Note = st.Note
		card.Today = st.Today != 0
		if st.ReviewAfter != nil {
			if t, pErr := time.Parse(time.RFC3339, *st.ReviewAfter); pErr == nil {
				card.ReviewAfter = &t
				if t.After(time.Now()) {
					card.ReviewDeferred = true
				}
			}
		}
	}

	return card, nil
}

// assignToSelf assigns the ticket to the current agent on Freshdesk.
// It skips the API call if the ticket is already assigned to us.
func (s *Server) assignToSelf(ctx context.Context, ticketID int64) error {
	if s.Freshdesk == nil {
		return nil
	}
	if err := s.ensureAgentID(ctx); err != nil {
		return err
	}
	// Skip if already assigned to us.
	if t := s.findCachedTicket(ticketID); t != nil && t.ResponderID == s.AgentID {
		return nil
	}
	if err := s.Freshdesk.AssignTicket(ctx, ticketID, s.AgentID); err != nil {
		return err
	}
	_ = s.Cache.Delete(ctx, fmt.Sprintf("ticket:%d", ticketID))
	_ = s.Cache.Delete(ctx, "tickets:dashboard")
	return nil
}

// nextWeekday returns the next Mon–Fri after now, truncated to midnight.
func nextWeekday(now time.Time) time.Time {
	next := now.AddDate(0, 0, 1).Truncate(24 * time.Hour)
	for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
