package srv

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"sync"
	"time"

	"desktriage.davea.me/freshdesk"
	"desktriage.davea.me/sprint"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// TicketCard is the merged view of a Freshdesk ticket + local state, ready
// for template rendering.
type TicketCard struct {
	// From Freshdesk
	ID          int64
	Subject     string
	Status      int
	StatusLabel string // "Open", "Pending", "Resolved", "Closed"
	CompanyName string
	UpdatedAt   time.Time
	TimeSince   string // relative time like "3h ago", "2d ago"
	LastUpdater string // "agent" or "client"
	IsAssigned  bool   // ResponderID matches our agent ID

	// From local state
	Priority       int        // 0 = unqueued
	Blocked        bool
	Note           string
	ReviewAfter    *time.Time
	Today          bool
	ReviewDeferred bool   // true if ReviewAfter is in the future
	ReviewDue      bool   // true if ReviewAfter is non-nil and ≤ today
	ViewContext    string // "dashboard", "today" — controls htmx behavior

	// Sprint info
	SprintLabel     string // e.g. "S5" — sprint the ticket was created in
	IsCurrentSprint bool   // true if ticket was created in the current sprint
	CreatedAt       time.Time // ticket creation date (for sprint calculation)
}

// DashboardData is passed to the dashboard template.
type DashboardData struct {
	Page              string       // "dashboard", "today" — for nav highlighting
	PageTitle         string       // optional, for <title> tag (used by ticket detail)
	ReviewNowTickets  []TicketCard // tickets with review date ≤ today
	AssignedTickets   []TicketCard // assigned to this agent, sorted by priority then activity
	UnassignedTickets []TicketCard // open but unassigned (current sprint), sorted by activity
	ForLaterTickets   []TicketCard // tickets with a future review date
	HistoricTickets   []TicketCard // unassigned from before the current sprint
	TotalCount        int
	TodayCount        int
	ReviewNowCount    int
}

// ---------------------------------------------------------------------------
// fetchDashboardData
// ---------------------------------------------------------------------------

// fetchAllTicketCards fetches all open tickets from Freshdesk (cached),
// merges with local state, resolves company names and last updaters
// concurrently (bounded by apiSem), and returns the full list of
// TicketCards plus the state map (to identify triageable tickets).
func (s *Server) fetchAllTicketCards(ctx context.Context) ([]TicketCard, map[int64]*ticketLocalState, error) {
	if err := s.ensureAgentID(ctx); err != nil {
		return nil, nil, fmt.Errorf("ensure agent ID: %w", err)
	}

	// ----- Fetch tickets (cached 5min) -----------------------------------
	var tickets []freshdesk.Ticket
	ok, err := s.Cache.GetJSON(ctx, "tickets:dashboard", &tickets)
	if err != nil {
		slog.Warn("cache read for tickets", "error", err)
	}
	if !ok {
		var fetchErr error

		// 1. Fetch my open+pending tickets via list filter.
		myTickets, err := s.Freshdesk.ListTickets(ctx, url.Values{
			"filter":   {"new_and_my_open"},
			"per_page": {"100"},
		})
		if err != nil {
			fetchErr = fmt.Errorf("list open tickets: %w", err)
		}

		if fetchErr == nil {
			// 2. Fetch my pending tickets (not covered by new_and_my_open).
			pendingTickets, err := s.Freshdesk.FilterTickets(ctx,
				fmt.Sprintf("agent_id:%d AND status:3", s.AgentID))
			if err != nil {
				slog.Warn("search pending tickets", "error", err)
			}

			// 3. Fetch ALL unassigned unresolved tickets.
			//    new_and_my_open only returns "new" (never-assigned) tickets;
			//    this catches unassigned+pending, re-unassigned, etc.
			unassignedTickets, err := s.Freshdesk.FilterTickets(ctx,
				"agent_id:null AND (status:2 OR status:3)")
			if err != nil {
				slog.Warn("search unassigned tickets", "error", err)
			}

			// Merge and deduplicate by ticket ID.
			seen := make(map[int64]struct{})
			var merged []freshdesk.Ticket
			for _, batch := range [][]freshdesk.Ticket{myTickets, pendingTickets, unassignedTickets} {
				for _, t := range batch {
					if _, dup := seen[t.ID]; !dup {
						seen[t.ID] = struct{}{}
						merged = append(merged, t)
					}
				}
			}

			tickets = merged
			if cErr := s.Cache.SetJSON(ctx, "tickets:dashboard", tickets, 5*time.Minute); cErr != nil {
				slog.Warn("cache write for tickets", "error", cErr)
			}
		} else {
			// API failed — try stale cache as fallback.
			staleOk, staleErr := s.Cache.GetStaleJSON(ctx, "tickets:dashboard", &tickets)
			if staleErr != nil {
				slog.Warn("stale cache read for tickets", "error", staleErr)
			}
			if staleOk {
				slog.Warn("using stale cached tickets after API failure", "error", fetchErr, "count", len(tickets))
			} else {
				return nil, nil, fmt.Errorf("%w (no stale cache available)", fetchErr)
			}
		}
	}

	// ----- Load local ticket states from DB ------------------------------
	dbStates, err := s.Queries.ListTicketStates(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list ticket states: %w", err)
	}
	stateMap := make(map[int64]*ticketLocalState, len(dbStates))
	for _, st := range dbStates {
		var ra *time.Time
		if st.ReviewAfter != nil {
			if t, pErr := time.Parse(time.RFC3339, *st.ReviewAfter); pErr == nil {
				ra = &t
			}
		}
		var da *time.Time
		if st.DeferredAt != nil {
			if t, pErr := time.Parse(time.RFC3339, *st.DeferredAt); pErr == nil {
				da = &t
			}
		}
		stateMap[st.TicketID] = &ticketLocalState{
			Priority:    int(st.Priority),
			Blocked:     st.Blocked != 0,
			Note:        st.Note,
			ReviewAfter: ra,
			DeferredAt:  da,
			Today:       st.Today != 0,
		}
	}

	// ----- Resolve company names + last updaters concurrently -----------
	companyNames := make(map[int64]string)
	lastUpdaters := make(map[int64]string, len(tickets))

	var mu sync.Mutex
	var wg sync.WaitGroup

	// Company names
	uniqueCompanies := make(map[int64]struct{})
	for _, t := range tickets {
		if t.CompanyID != 0 {
			uniqueCompanies[t.CompanyID] = struct{}{}
		}
	}
	for id := range uniqueCompanies {
		wg.Add(1)
		go func(companyID int64) {
			defer wg.Done()
			s.apiSem <- struct{}{}
			defer func() { <-s.apiSem }()
			name := s.resolveCompanyName(ctx, companyID)
			mu.Lock()
			companyNames[companyID] = name
			mu.Unlock()
		}(id)
	}

	// Last updaters
	for _, t := range tickets {
		wg.Add(1)
		go func(ticket freshdesk.Ticket) {
			defer wg.Done()
			s.apiSem <- struct{}{}
			defer func() { <-s.apiSem }()
			updater := s.determineLastUpdater(ctx, ticket)
			mu.Lock()
			lastUpdaters[ticket.ID] = updater
			mu.Unlock()
		}(t)
	}

	wg.Wait()

	// ----- Load sprint anchors -----------------------------------------
	anchors := s.loadSprintAnchors(ctx)
	var currentSprint sprint.Sprint
	var haveSprints bool
	if len(anchors) > 0 {
		currentSprint, haveSprints = sprint.Resolve(anchors, time.Now())
	}

	// ----- Build TicketCards --------------------------------------------
	cards := make([]TicketCard, 0, len(tickets))
	for _, t := range tickets {
		card := TicketCard{
			ID:          t.ID,
			Subject:     t.Subject,
			Status:      t.Status,
			StatusLabel: s.statusLabel(ctx, t.Status),
			CompanyName: companyNames[t.CompanyID],
			UpdatedAt:   t.UpdatedAt,
			TimeSince:   timeSince(t.UpdatedAt),
			LastUpdater: lastUpdaters[t.ID],
			IsAssigned:  t.ResponderID == s.AgentID,
			CreatedAt:   t.CreatedAt,
		}

		// Sprint info
		if haveSprints {
			if ts, ok := sprint.Resolve(anchors, t.CreatedAt); ok {
				card.SprintLabel = fmt.Sprintf("S%d", ts.Number)
				card.IsCurrentSprint = ts.Year == currentSprint.Year && ts.Number == currentSprint.Number
			}
		}

		if ls, ok := stateMap[t.ID]; ok {
			card.Priority = ls.Priority
			card.Blocked = ls.Blocked
			card.Note = ls.Note
			card.ReviewAfter = ls.ReviewAfter
			card.Today = ls.Today
			if ls.ReviewAfter != nil {
				today := truncate(time.Now())
				reviewDate := truncate(*ls.ReviewAfter)
				if reviewDate.After(today) {
					card.ReviewDeferred = true
				} else {
					card.ReviewDue = true
				}
			}
		}

		cards = append(cards, card)
	}

	return cards, stateMap, nil
}

func (s *Server) fetchDashboardData(ctx context.Context) (*DashboardData, error) {
	cards, stateMap, err := s.fetchAllTicketCards(ctx)
	if err != nil {
		return nil, err
	}

	var reviewNow, assigned, unassigned, forLater, historic []TicketCard
	todayCount := 0
	for _, card := range cards {
		if card.Today {
			todayCount++
		}

		// Tickets with a review date due go into Review Now (plus their normal section).
		if card.ReviewDue {
			reviewNow = append(reviewNow, card)
		}

		// Deferred tickets (future review date) go to For Later, unless
		// the client replied after the ticket was deferred — in which
		// case promote to Review Now so the agent notices.
		if card.ReviewDeferred {
			if ls, ok := stateMap[card.ID]; ok && ls.DeferredAt != nil {
				if latestReply := s.latestIncomingReplyTime(ctx, card.ID); latestReply != nil && latestReply.After(*ls.DeferredAt) {
					card.ReviewDeferred = false
					card.ReviewDue = true
					reviewNow = append(reviewNow, card)
					// Don't continue — fall through to normal assignment below.
				} else {
					forLater = append(forLater, card)
					continue
				}
			} else {
				forLater = append(forLater, card)
				continue
			}
		}

		if card.IsAssigned {
			assigned = append(assigned, card)
		} else if !card.IsCurrentSprint && card.SprintLabel != "" {
			// Unassigned from a previous sprint → historic.
			historic = append(historic, card)
		} else {
			unassigned = append(unassigned, card)
		}
	}

	// ----- Sort ----------------------------------------------------------
	sortByUpdated := func(s []TicketCard) {
		sort.Slice(s, func(i, j int) bool {
			return s[i].UpdatedAt.After(s[j].UpdatedAt)
		})
	}

	sort.Slice(assigned, func(i, j int) bool {
		pi, pj := assigned[i].Priority, assigned[j].Priority
		if pi > 0 && pj > 0 {
			if pi != pj {
				return pi > pj
			}
			return assigned[i].UpdatedAt.After(assigned[j].UpdatedAt)
		}
		if pi > 0 {
			return true
		}
		if pj > 0 {
			return false
		}
		return assigned[i].UpdatedAt.After(assigned[j].UpdatedAt)
	})

	// Review Now: oldest review date first (most overdue at top).
	sort.Slice(reviewNow, func(i, j int) bool {
		if reviewNow[i].ReviewAfter == nil || reviewNow[j].ReviewAfter == nil {
			return false
		}
		return reviewNow[i].ReviewAfter.Before(*reviewNow[j].ReviewAfter)
	})

	sortByUpdated(unassigned)
	sortByUpdated(historic)

	// For Later: soonest review date first.
	sort.Slice(forLater, func(i, j int) bool {
		if forLater[i].ReviewAfter == nil || forLater[j].ReviewAfter == nil {
			return false
		}
		return forLater[i].ReviewAfter.Before(*forLater[j].ReviewAfter)
	})


	return &DashboardData{
		Page:              "dashboard",
		ReviewNowTickets:  reviewNow,
		AssignedTickets:   assigned,
		UnassignedTickets: unassigned,
		ForLaterTickets:   forLater,
		HistoricTickets:   historic,
		TotalCount:        len(cards),
		TodayCount:        todayCount,
		ReviewNowCount:    len(reviewNow),
	}, nil
}


// ticketLocalState holds the parsed local DB state for a single ticket.
type ticketLocalState struct {
	Priority    int
	Blocked     bool
	Note        string
	ReviewAfter *time.Time
	DeferredAt  *time.Time
	Today       bool
}

// resolveCompanyName returns the Freshdesk company name for the given ID.
// Results are cached for 7 days. Returns "" on error or if companyID is 0.
func (s *Server) resolveCompanyName(ctx context.Context, companyID int64) string {
	if companyID == 0 {
		return ""
	}

	cacheKey := fmt.Sprintf("company:%d", companyID)

	var name string
	ok, err := s.Cache.GetJSON(ctx, cacheKey, &name)
	if err != nil {
		slog.Warn("cache read for company", "company_id", companyID, "error", err)
	}
	if ok {
		return name
	}

	company, err := s.Freshdesk.GetCompany(ctx, companyID)
	if err != nil {
		slog.Warn("fetch company", "company_id", companyID, "error", err)
		return ""
	}

	if cErr := s.Cache.SetJSON(ctx, cacheKey, company.Name, 7*24*time.Hour); cErr != nil {
		slog.Warn("cache write for company", "company_id", companyID, "error", cErr)
	}

	return company.Name
}

// ---------------------------------------------------------------------------
// determineLastUpdater
// ---------------------------------------------------------------------------

// fetchConversations returns the cached (or freshly-fetched) conversations
// for a ticket. The result is cached for 5 minutes.
func (s *Server) fetchConversations(ctx context.Context, ticketID int64) []freshdesk.Conversation {
	cacheKey := fmt.Sprintf("conversations:%d", ticketID)

	var convos []freshdesk.Conversation
	ok, err := s.Cache.GetJSON(ctx, cacheKey, &convos)
	if err != nil {
		slog.Warn("cache read for conversations", "ticket_id", ticketID, "error", err)
	}
	if !ok {
		if s.Freshdesk == nil {
			return nil
		}
		convos, err = s.Freshdesk.GetConversations(ctx, ticketID)
		if err != nil {
			slog.Warn("fetch conversations", "ticket_id", ticketID, "error", err)
			return nil
		}
		if cErr := s.Cache.SetJSON(ctx, cacheKey, convos, 5*time.Minute); cErr != nil {
			slog.Warn("cache write for conversations", "ticket_id", ticketID, "error", cErr)
		}
	}

	return convos
}

// determineLastUpdater returns "agent" or "client" based on the last non-private
// conversation on the ticket. If the ticket has no conversations, it returns
// "client" (assuming the initial ticket creation by the requester).
func (s *Server) determineLastUpdater(ctx context.Context, ticket freshdesk.Ticket) string {
	convos := s.fetchConversations(ctx, ticket.ID)

	if len(convos) == 0 {
		return "client"
	}

	// Find the last non-private conversation.
	for i := len(convos) - 1; i >= 0; i-- {
		if !convos[i].Private {
			if convos[i].Incoming {
				return "client"
			}
			return "agent"
		}
	}

	// All conversations are private (internal notes only) — treat as agent.
	return "agent"
}

// latestIncomingReplyTime returns the CreatedAt time of the most recent
// incoming (client) non-private conversation, or nil if there are none.
func (s *Server) latestIncomingReplyTime(ctx context.Context, ticketID int64) *time.Time {
	convos := s.fetchConversations(ctx, ticketID)
	for i := len(convos) - 1; i >= 0; i-- {
		if !convos[i].Private && convos[i].Incoming {
			t := convos[i].CreatedAt
			return &t
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// ensureAgentID
// ---------------------------------------------------------------------------

// ensureAgentID resolves and caches the current Freshdesk agent. It sets
// s.AgentID on success. The agent info is cached for 1 hour.
func (s *Server) ensureAgentID(ctx context.Context) error {
	if s.AgentID != 0 {
		return nil
	}

	var agent freshdesk.Agent
	ok, err := s.Cache.GetJSON(ctx, "agent:me", &agent)
	if err != nil {
		slog.Warn("cache read for agent", "error", err)
	}
	if ok {
		s.AgentID = agent.ID
		return nil
	}

	ap, err := s.Freshdesk.GetCurrentAgent(ctx)
	if err != nil {
		return fmt.Errorf("get current agent: %w", err)
	}

	s.AgentID = ap.ID

	if cErr := s.Cache.SetJSON(ctx, "agent:me", ap, 7*24*time.Hour); cErr != nil {
		slog.Warn("cache write for agent", "error", cErr)
	}

	return nil
}


// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// truncate strips a time down to midnight UTC (date-only comparison).
func truncate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// statusLabel maps a Freshdesk status code to a human-readable label
// using cached status choices from the Freshdesk API.
func (s *Server) statusLabel(ctx context.Context, status int) string {
	for _, sc := range s.loadStatusChoices(ctx) {
		if sc.Value == status {
			return sc.Label
		}
	}
	// Fallback if cache is empty or status not found.
	switch status {
	case 2:
		return "Open"
	case 3:
		return "Pending"
	case 4:
		return "Resolved"
	case 5:
		return "Closed"
	default:
		return "Unknown"
	}
}

// timeSince returns a human-readable relative time string.
func timeSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	}
}
