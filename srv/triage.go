package srv

import (
	"log/slog"
	"net/http"
	"sort"
)

// TriageData is passed to the triage template.
type TriageData struct {
	Page        string
	PageTitle   string
	Tickets     []TicketCard
	TotalCount  int
	TodayCount  int
	TriageCount int
}

// HandleTriage renders the triage view: tickets with no local state set.
func (s *Server) HandleTriage(w http.ResponseWriter, r *http.Request) {
	cards, stateMap, err := s.fetchAllTicketCards(r.Context())
	if err != nil {
		slog.Error("fetch ticket cards", "error", err)
		http.Error(w, "Failed to load tickets", http.StatusInternalServerError)
		return
	}

	var triageCards []TicketCard
	todayCount := 0
	for _, c := range cards {
		if c.Today {
			todayCount++
		}
		if isTriageable(c, stateMap) {
			triageCards = append(triageCards, c)
		}
	}

	// Sort by last activity (most recent first — needs attention soonest)
	sort.Slice(triageCards, func(i, j int) bool {
		return triageCards[i].UpdatedAt.After(triageCards[j].UpdatedAt)
	})

	data := &TriageData{
		Page:        "triage",
		Tickets:     triageCards,
		TotalCount:  len(cards),
		TodayCount:  todayCount,
		TriageCount: len(triageCards),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderTemplate(w, "triage.html", data); err != nil {
		slog.Warn("render triage", "error", err)
	}
}
