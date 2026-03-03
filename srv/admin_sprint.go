package srv

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"desktriage.davea.me/db/dbgen"
	"desktriage.davea.me/sprint"
)

// SprintAnchorView is a template-ready representation of a sprint anchor.
type SprintAnchorView struct {
	Year        int
	Number      int
	StartDate   string // YYYY-MM-DD
	DisplayDate string // e.g. "Jan 6"
}

// HandleAdminSprintAnchorAdd handles POST /admin/sprint-anchor.
func (s *Server) HandleAdminSprintAnchorAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form data", http.StatusBadRequest)
		return
	}

	yearStr := r.FormValue("year")
	numberStr := r.FormValue("sprint_number")
	dateStr := r.FormValue("start_date")

	year, err := strconv.Atoi(yearStr)
	if err != nil || year < 2020 || year > 2100 {
		http.Error(w, "invalid year", http.StatusBadRequest)
		return
	}

	number, err := strconv.Atoi(numberStr)
	if err != nil || number < 1 || number > 26 {
		http.Error(w, "sprint number must be 1-26", http.StatusBadRequest)
		return
	}

	if _, err := time.Parse("2006-01-02", dateStr); err != nil {
		http.Error(w, "invalid date format (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	if err := s.Queries.UpsertSprintAnchor(r.Context(), dbgen.UpsertSprintAnchorParams{
		SprintNumber: int64(number),
		Year:         int64(year),
		StartDate:    dateStr,
	}); err != nil {
		slog.Error("upsert sprint anchor", "error", err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/config?saved=1", http.StatusSeeOther)
}

// HandleAdminSprintAnchorDelete handles DELETE /admin/sprint-anchor/{year}/{sprint}.
func (s *Server) HandleAdminSprintAnchorDelete(w http.ResponseWriter, r *http.Request) {
	year, err := strconv.ParseInt(r.PathValue("year"), 10, 64)
	if err != nil {
		http.Error(w, "invalid year", http.StatusBadRequest)
		return
	}
	sp, err := strconv.ParseInt(r.PathValue("sprint"), 10, 64)
	if err != nil {
		http.Error(w, "invalid sprint", http.StatusBadRequest)
		return
	}

	if err := s.Queries.DeleteSprintAnchor(r.Context(), dbgen.DeleteSprintAnchorParams{
		Year:         year,
		SprintNumber: sp,
	}); err != nil {
		slog.Error("delete sprint anchor", "error", err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/config?saved=1", http.StatusSeeOther)
}

// loadSprintAnchors reads all sprint anchors from DB and converts to sprint.Anchor.
func (s *Server) loadSprintAnchors(ctx context.Context) []sprint.Anchor {
	rows, err := s.Queries.ListSprintAnchors(ctx)
	if err != nil {
		slog.Warn("list sprint anchors", "error", err)
		return nil
	}
	anchors := make([]sprint.Anchor, 0, len(rows))
	for _, r := range rows {
		t, err := time.Parse("2006-01-02", r.StartDate)
		if err != nil {
			slog.Warn("parse sprint anchor date", "date", r.StartDate, "error", err)
			continue
		}
		anchors = append(anchors, sprint.Anchor{
			Year:      int(r.Year),
			Number:    int(r.SprintNumber),
			StartDate: t,
		})
	}
	return anchors
}

// sprintAnchorViews builds template-ready views from DB rows.
func sprintAnchorViews(rows []dbgen.SprintAnchor) []SprintAnchorView {
	views := make([]SprintAnchorView, 0, len(rows))
	for _, r := range rows {
		display := r.StartDate
		if t, err := time.Parse("2006-01-02", r.StartDate); err == nil {
			display = t.Format("Jan 2")
		}
		views = append(views, SprintAnchorView{
			Year:        int(r.Year),
			Number:      int(r.SprintNumber),
			StartDate:   r.StartDate,
			DisplayDate: display,
		})
	}
	return views
}

// currentSprintLabel returns a human-friendly label like "S5 (2026)" for the current sprint.
func (s *Server) currentSprintLabel(ctx context.Context) string {
	anchors := s.loadSprintAnchors(ctx)
	if len(anchors) == 0 {
		return ""
	}
	sp, ok := sprint.Resolve(anchors, time.Now())
	if !ok {
		return ""
	}
	return fmt.Sprintf("S%d (%d)", sp.Number, sp.Year)
}
