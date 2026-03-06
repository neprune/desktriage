package srv

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"desktriage.davea.me/freshdesk"
)

// HandleUpdateFreshdeskStatus sets the status of a ticket on Freshdesk.
func (s *Server) HandleUpdateFreshdeskStatus(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	ticketID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid ticket ID", http.StatusBadRequest)
		return
	}

	statusStr := r.FormValue("status")
	newStatus, err := strconv.Atoi(statusStr)
	if err != nil || newStatus < 2 {
		http.Error(w, "invalid status value", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Skip the API call if the status hasn't actually changed.
	var ticket *freshdesk.Ticket
	cacheKey := fmt.Sprintf("ticket:%d", ticketID)
	ok, _ := s.Cache.GetJSON(ctx, cacheKey, &ticket)
	if ok && ticket.Status == newStatus {
		data := s.buildFreshdeskStatusData(ctx, ticketID, newStatus)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = s.renderPartialCtx(ctx, w, "freshdesk-status", data)
		return
	}

	// Rate limit: max 1 status change per 5 seconds per ticket.
	if !s.noteLimiter.Allow(ticketID, 5*time.Second) {
		http.Error(w, "Please wait a few seconds before changing status again", http.StatusTooManyRequests)
		return
	}

	err = s.Freshdesk.UpdateTicketStatus(ctx, ticketID, newStatus)
	if err != nil {
		slog.Error("update freshdesk ticket status", "ticket_id", ticketID, "status", newStatus, "error", err)
		http.Error(w, "Failed to update ticket status on Freshdesk", http.StatusInternalServerError)
		return
	}

	slog.Info("updated freshdesk ticket status", "ticket_id", ticketID, "status", newStatus)

	// Invalidate ticket cache so subsequent page loads show the new status.
	_ = s.Cache.Delete(ctx, fmt.Sprintf("ticket:%d", ticketID))

	// If the request came from a list page, return the full ticket card.
	from := r.FormValue("from")
	if from == "list" {
		card, cErr := s.buildTicketCard(ctx, ticketID)
		if cErr != nil {
			slog.Error("build ticket card after status change", "ticket_id", ticketID, "error", cErr)
			http.Error(w, "failed to build card", http.StatusInternalServerError)
			return
		}
		// Override status on the card since the cache was just invalidated
		// and buildTicketCard may still have the old value.
		card.Status = newStatus
		card.StatusLabel = s.statusLabel(ctx, newStatus)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.renderPartialCtx(ctx, w, "ticket-card", card); err != nil {
			slog.Warn("render ticket card partial", "error", err)
		}
		return
	}

	// Return the updated panel partial + OOB badge swap (ticket detail page).
	data := s.buildFreshdeskStatusData(ctx, ticketID, newStatus)
	badge := &FreshdeskStatusBadgeData{Status: newStatus, StatusLabel: s.statusLabel(ctx, newStatus)}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderPartialCtx(ctx, w, "freshdesk-status", data); err != nil {
		slog.Warn("render freshdesk-status partial", "error", err)
	}
	if err := s.renderPartialCtx(ctx, w, "freshdesk-status-badge", badge); err != nil {
		slog.Warn("render freshdesk-status-badge partial", "error", err)
	}
}

// buildFreshdeskStatusData fetches available statuses and returns the partial data.
func (s *Server) buildFreshdeskStatusData(ctx context.Context, ticketID int64, currentStatus int) *FreshdeskStatusData {
	statusChoices := s.loadStatusChoices(ctx)
	data := &FreshdeskStatusData{ID: ticketID}
	for _, sc := range statusChoices {
		data.Statuses = append(data.Statuses, StatusOption{
			Label:    sc.Label,
			Value:    sc.Value,
			Selected: sc.Value == currentStatus,
		})
	}
	return data
}

// HandleStatusChoices returns the available Freshdesk status options as JSON.
func (s *Server) HandleStatusChoices(w http.ResponseWriter, r *http.Request) {
	statusChoices := s.loadStatusChoices(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(statusChoices); err != nil {
		slog.Warn("encode status choices", "error", err)
	}
}

// loadStatusChoices returns cached Freshdesk status choices, fetching if needed.
func (s *Server) loadStatusChoices(ctx context.Context) []freshdesk.StatusChoice {
	var statusChoices []freshdesk.StatusChoice
	scKey := "freshdesk:status_choices"
	ok, err := s.Cache.GetJSON(ctx, scKey, &statusChoices)
	if err != nil {
		slog.Warn("cache read for status choices", "error", err)
	}
	if !ok {
		if s.Freshdesk == nil {
			return nil
		}
		fields, fErr := s.Freshdesk.GetTicketFields(ctx)
		if fErr != nil {
			slog.Warn("fetch ticket fields", "error", fErr)
			return nil
		}
		statusChoices = freshdesk.ParseStatusChoices(fields)
		if cErr := s.Cache.SetJSON(ctx, scKey, statusChoices, 1*time.Hour); cErr != nil {
			slog.Warn("cache write for status choices", "error", cErr)
		}
	}
	return statusChoices
}
