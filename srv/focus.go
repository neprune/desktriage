package srv

import (
	"log/slog"
	"net/http"
	"sort"
)

// TodayData is passed to the today (focus) template.
type TodayData struct {
	Page       string
	PageTitle  string
	Tickets    []TicketCard
	TotalCount int
	TodayCount int
}

// HandleToday renders the today view: only tickets flagged for today.
func (s *Server) HandleToday(w http.ResponseWriter, r *http.Request) {
	cards, _, err := s.fetchAllTicketCards(r.Context())
	if err != nil {
		slog.Error("fetch ticket cards", "error", err)
		http.Error(w, "Failed to load tickets", http.StatusInternalServerError)
		return
	}

	var todayCards []TicketCard
	todayCount := 0
	for _, c := range cards {
		if c.Today {
			c.ViewContext = "today"
			todayCards = append(todayCards, c)
			todayCount++
		}
	}

	// Sort by priority (higher first), then by last activity
	sort.Slice(todayCards, func(i, j int) bool {
		pi, pj := todayCards[i].Priority, todayCards[j].Priority
		if pi != pj {
			return pi > pj
		}
		return todayCards[i].UpdatedAt.After(todayCards[j].UpdatedAt)
	})

	data := &TodayData{
		Page:       "today",
		Tickets:    todayCards,
		TotalCount: len(cards),
		TodayCount: todayCount,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderTemplate(w, "focus.html", data); err != nil {
		slog.Warn("render today", "error", err)
	}
}
