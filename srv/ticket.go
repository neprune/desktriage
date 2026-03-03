package srv

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"desktriage.davea.me/freshdesk"
	"desktriage.davea.me/sprint"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ConversationView is a template-ready representation of a single conversation.
type ConversationView struct {
	ID             int64
	Body           template.HTML
	BodyText       string // plain-text version for detecting auto-responses
	SenderName     string
	SenderEmail    string
	Incoming       bool
	Private        bool
	IsAutoResponse bool
	CreatedAt      time.Time
	TimeSince      string
}

// TicketDetailData is passed to the ticket detail template.
type TicketDetailData struct {
	// Layout fields (required by layout.html nav)
	Page        string
	PageTitle   string // for <title> tag, e.g. "#6678: Grass area..."
	TotalCount  int
	TodayCount  int
	TriageCount int

	// Ticket fields
	ID             int64
	FreshdeskURL   string // link to ticket on Freshdesk
	Subject        string
	Description    template.HTML
	Status         int
	StatusLabel    string
	Priority       int
	PriorityLabel  string
	Type           string
	Tags           []string
	CompanyName    string
	RequesterName  string
	RequesterEmail string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DueBy            time.Time
	CreatedTimeSince string

	// Conversations
	Conversations []ConversationView

	// Local state (for sidebar controls)
	Today          bool
	Blocked        bool
	Note           string
	ReviewAfter    *time.Time
	LocalPriority  int
	ReviewDeferred bool
	ViewContext    string

	// Sprint info
	SprintLabel     string
	IsCurrentSprint bool
}

// TicketStateData is the minimal struct passed to the ticket-state partial.
type TicketStateData struct {
	ID             int64
	Today          bool
	Blocked        bool
	Note           string
	ReviewAfter    *time.Time
	LocalPriority  int
	ReviewDeferred bool
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// HandleTicketDetail renders the ticket detail page.
func (s *Server) HandleTicketDetail(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	ticketID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid ticket ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	if err := s.ensureAgentID(ctx); err != nil {
		slog.Error("ensure agent ID", "error", err)
		http.Error(w, "Failed to resolve agent", http.StatusInternalServerError)
		return
	}

	// --- Fetch full ticket (cached 2min) ---------------------------------
	// The dashboard list cache omits the description field, so we need
	// the single-ticket endpoint for the detail view.
	var ticket *freshdesk.Ticket
	ticketCacheKey := fmt.Sprintf("ticket:%d", ticketID)
	ok, err := s.Cache.GetJSON(ctx, ticketCacheKey, &ticket)
	if err != nil {
		slog.Warn("cache read for ticket", "ticket_id", ticketID, "error", err)
	}
	if !ok {
		ticket, err = s.Freshdesk.GetTicket(ctx, ticketID)
		if err != nil {
			slog.Error("fetch ticket", "ticket_id", ticketID, "error", err)
			http.Error(w, "Failed to load ticket", http.StatusInternalServerError)
			return
		}
		if cErr := s.Cache.SetJSON(ctx, ticketCacheKey, ticket, 2*time.Minute); cErr != nil {
			slog.Warn("cache write for ticket", "ticket_id", ticketID, "error", cErr)
		}
	}

	// --- Fetch conversations (cached 2min) -------------------------------
	var convos []freshdesk.Conversation
	cacheKey := fmt.Sprintf("conversations:%d", ticketID)
	ok, err = s.Cache.GetJSON(ctx, cacheKey, &convos)
	if err != nil {
		slog.Warn("cache read for conversations", "ticket_id", ticketID, "error", err)
	}
	if !ok {
		convos, err = s.Freshdesk.GetConversations(ctx, ticketID)
		if err != nil {
			slog.Warn("fetch conversations", "ticket_id", ticketID, "error", err)
			// Non-fatal: render page without conversations
			convos = nil
		} else {
			if cErr := s.Cache.SetJSON(ctx, cacheKey, convos, 2*time.Minute); cErr != nil {
				slog.Warn("cache write for conversations", "ticket_id", ticketID, "error", cErr)
			}
		}
	}

	// --- Resolve requester -----------------------------------------------
	reqName, reqEmail := s.resolveContact(ctx, ticket.RequesterID)

	// --- Resolve company name --------------------------------------------
	companyName := s.resolveCompanyName(ctx, ticket.CompanyID)

	// --- Resolve conversation sender names concurrently ------------------
	uniqueUserIDs := make(map[int64]struct{})
	for _, c := range convos {
		if c.UserID != 0 {
			uniqueUserIDs[c.UserID] = struct{}{}
		}
	}

	contactNames := make(map[int64]string, len(uniqueUserIDs))
	contactEmails := make(map[int64]string, len(uniqueUserIDs))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for uid := range uniqueUserIDs {
		wg.Add(1)
		go func(userID int64) {
			defer wg.Done()
			s.apiSem <- struct{}{}
			defer func() { <-s.apiSem }()

			name, email := s.resolveContact(ctx, userID)
			mu.Lock()
			contactNames[userID] = name
			contactEmails[userID] = email
			mu.Unlock()
		}(uid)
	}
	wg.Wait()

	// --- Build conversation views ----------------------------------------
	convoViews := make([]ConversationView, 0, len(convos))
	for _, c := range convos {
		cv := ConversationView{
			ID:             c.ID,
			Body:           template.HTML(c.Body),
			BodyText:       c.BodyText,
			SenderName:     contactNames[c.UserID],
			SenderEmail:    contactEmails[c.UserID],
			Incoming:       c.Incoming,
			Private:        c.Private,
			IsAutoResponse: c.Private && strings.HasPrefix(c.BodyText, "Auto response sent:"),
			CreatedAt:      c.CreatedAt,
			TimeSince:      timeSince(c.CreatedAt),
		}
		if cv.SenderName == "" {
			if c.FromEmail != "" {
				cv.SenderEmail = c.FromEmail
				cv.SenderName = c.FromEmail
			} else {
				cv.SenderName = "Unknown"
			}
		}
		convoViews = append(convoViews, cv)
	}

	// --- Load local state from DB ----------------------------------------
	var today bool
	var blocked bool
	var note string
	var reviewAfter *time.Time
	var localPriority int
	var reviewDeferred bool

	st, err := s.Queries.GetTicketState(ctx, ticketID)
	if err == nil {
		today = st.Today != 0
		blocked = st.Blocked != 0
		note = st.Note
		localPriority = int(st.Priority)
		if st.ReviewAfter != nil {
			if t, pErr := time.Parse(time.RFC3339, *st.ReviewAfter); pErr == nil {
				reviewAfter = &t
				if t.After(time.Now()) {
					reviewDeferred = true
				}
			}
		}
	} else if err != sql.ErrNoRows {
		slog.Warn("get ticket state", "ticket_id", ticketID, "error", err)
	}

	// --- Nav badge counts (lightweight) ----------------------------------
	totalCount, todayCount, triageCount := s.fetchNavCounts(ctx)

	// --- Sprint info ----------------------------------------------------
	var sprintLabel string
	var isCurrentSprint bool
	anchors := s.loadSprintAnchors(ctx)
	if len(anchors) > 0 {
		if currentSp, spOk := sprint.Resolve(anchors, time.Now()); spOk {
			if ticketSp, spOk2 := sprint.Resolve(anchors, ticket.CreatedAt); spOk2 {
				sprintLabel = fmt.Sprintf("S%d", ticketSp.Number)
				isCurrentSprint = ticketSp.Year == currentSp.Year && ticketSp.Number == currentSp.Number
			}
		}
	}

	data := &TicketDetailData{
		Page:           "ticket",
		PageTitle:      fmt.Sprintf("#%d: %s", ticket.ID, ticket.Subject),
		TotalCount:     totalCount,
		TodayCount:     todayCount,
		TriageCount:    triageCount,
		ID:             ticket.ID,
		FreshdeskURL:   fmt.Sprintf("%s/a/tickets/%d", s.Config.FreshdeskURL, ticket.ID),
		Subject:        ticket.Subject,
		Description:    template.HTML(ticket.Description),
		Status:         ticket.Status,
		StatusLabel:    statusLabel(ticket.Status),
		Priority:       ticket.Priority,
		PriorityLabel:  priorityLabel(ticket.Priority),
		Type:           ticket.Type,
		Tags:           ticket.Tags,
		CompanyName:    companyName,
		RequesterName:  reqName,
		RequesterEmail: reqEmail,
		CreatedAt:      ticket.CreatedAt,
		UpdatedAt:      ticket.UpdatedAt,
		DueBy:            ticket.DueBy,
		CreatedTimeSince: timeSince(ticket.CreatedAt),
		Conversations:  convoViews,
		Today:          today,
		Blocked:        blocked,
		Note:           note,
		ReviewAfter:    reviewAfter,
		LocalPriority:  localPriority,
		ReviewDeferred: reviewDeferred,
		ViewContext:    "ticket",
		SprintLabel:     sprintLabel,
		IsCurrentSprint: isCurrentSprint,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderTemplate(w, "ticket.html", data); err != nil {
		slog.Warn("render ticket detail", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Ticket Preview (inline expand)
// ---------------------------------------------------------------------------

// TicketPreviewData is passed to the _ticket_preview.html partial.
type TicketPreviewData struct {
	ID              int64
	Description     template.HTML // initial ticket message
	LatestReply     *PreviewReply // most recent human reply, if any
}

// PreviewReply represents a single conversation entry for the preview.
type PreviewReply struct {
	Body       template.HTML
	SenderName string
	Incoming   bool
	CreatedAt  time.Time
	TimeSince  string
}

// HandleTicketPreview returns an HTML partial with the ticket description
// and the most recent human reply for inline expansion on dashboard/triage.
func (s *Server) HandleTicketPreview(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	ticketID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid ticket ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Fetch full ticket (for description)
	var ticket *freshdesk.Ticket
	ticketCacheKey := fmt.Sprintf("ticket:%d", ticketID)
	ok, err := s.Cache.GetJSON(ctx, ticketCacheKey, &ticket)
	if err != nil {
		slog.Warn("cache read for ticket preview", "ticket_id", ticketID, "error", err)
	}
	if !ok {
		ticket, err = s.Freshdesk.GetTicket(ctx, ticketID)
		if err != nil {
			slog.Error("fetch ticket for preview", "ticket_id", ticketID, "error", err)
			http.Error(w, "Failed to load ticket", http.StatusInternalServerError)
			return
		}
		if cErr := s.Cache.SetJSON(ctx, ticketCacheKey, ticket, 2*time.Minute); cErr != nil {
			slog.Warn("cache write for ticket preview", "ticket_id", ticketID, "error", cErr)
		}
	}

	// Fetch conversations
	var convos []freshdesk.Conversation
	convoCacheKey := fmt.Sprintf("conversations:%d", ticketID)
	ok, err = s.Cache.GetJSON(ctx, convoCacheKey, &convos)
	if err != nil {
		slog.Warn("cache read for conversations preview", "ticket_id", ticketID, "error", err)
	}
	if !ok {
		convos, err = s.Freshdesk.GetConversations(ctx, ticketID)
		if err != nil {
			slog.Warn("fetch conversations for preview", "ticket_id", ticketID, "error", err)
			convos = nil
		} else {
			if cErr := s.Cache.SetJSON(ctx, convoCacheKey, convos, 2*time.Minute); cErr != nil {
				slog.Warn("cache write for conversations preview", "ticket_id", ticketID, "error", cErr)
			}
		}
	}

	data := &TicketPreviewData{
		ID:          ticket.ID,
		Description: template.HTML(ticket.Description),
	}

	// Find latest human reply: walk backwards through conversations,
	// skip private notes and auto-responses.
	for i := len(convos) - 1; i >= 0; i-- {
		c := convos[i]
		if c.Private {
			continue
		}
		// Skip auto-responses: outgoing replies created within 2 minutes of ticket creation.
		if !c.Incoming && c.CreatedAt.Sub(ticket.CreatedAt) < 2*time.Minute {
			continue
		}
		senderName := s.resolveContactName(ctx, c.UserID)
		if senderName == "" || senderName == "Unknown" {
			if c.FromEmail != "" {
				senderName = c.FromEmail
			}
		}
		data.LatestReply = &PreviewReply{
			Body:       template.HTML(c.Body),
			SenderName: senderName,
			Incoming:   c.Incoming,
			CreatedAt:  c.CreatedAt,
			TimeSince:  timeSince(c.CreatedAt),
		}
		break
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderPartial(w, "ticket-preview", data); err != nil {
		slog.Warn("render ticket preview", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveContact returns the name and email for a Freshdesk contact.
// Results are cached at contact:{id} for 5 minutes. If the contact API
// fails (e.g. the ID belongs to an agent, not a contact), it falls back
// to the cached agent info.
func (s *Server) resolveContact(ctx context.Context, contactID int64) (name, email string) {
	if contactID == 0 {
		return "Unknown", ""
	}

	cacheKey := fmt.Sprintf("contact:%d", contactID)

	var ct freshdesk.Contact
	ok, err := s.Cache.GetJSON(ctx, cacheKey, &ct)
	if err != nil {
		slog.Warn("cache read for contact", "contact_id", contactID, "error", err)
	}
	if ok {
		n := ct.Name
		if n == "" {
			n = "Unknown"
		}
		return n, ct.Email
	}

	contact, err := s.Freshdesk.GetContact(ctx, contactID)
	if err != nil {
		// The ID may belong to an agent rather than a contact.
		// Try the agents API as a fallback.
		if s.Freshdesk != nil {
			agent, aErr := s.Freshdesk.GetAgent(ctx, contactID)
			if aErr == nil && agent.Contact.Name != "" {
				// Cache as a contact so we don't hit the agent API again.
				fakeContact := &freshdesk.Contact{
					ID:    contactID,
					Name:  agent.Contact.Name,
					Email: agent.Contact.Email,
				}
				_ = s.Cache.SetJSON(ctx, cacheKey, fakeContact, 7*24*time.Hour)
				return agent.Contact.Name, agent.Contact.Email
			}
		}
		slog.Warn("fetch contact", "contact_id", contactID, "error", err)
		return "Unknown", ""
	}

	if cErr := s.Cache.SetJSON(ctx, cacheKey, contact, 7*24*time.Hour); cErr != nil {
		slog.Warn("cache write for contact", "contact_id", contactID, "error", cErr)
	}

	n := contact.Name
	if n == "" {
		n = "Unknown"
	}
	return n, contact.Email
}

// resolveContactName returns just the name for a Freshdesk contact.
func (s *Server) resolveContactName(ctx context.Context, contactID int64) string {
	name, _ := s.resolveContact(ctx, contactID)
	return name
}

// fetchNavCounts returns lightweight badge counts for the nav bar.
// It reuses cached dashboard data if available; otherwise returns zeros
// to avoid forcing a full Freshdesk refresh.
func (s *Server) fetchNavCounts(ctx context.Context) (total, today, triage int) {
	var tickets []freshdesk.Ticket
	ok, _ := s.Cache.GetJSON(ctx, "tickets:dashboard", &tickets)
	if !ok {
		return 0, 0, 0
	}

	// Load local states
	dbStates, err := s.Queries.ListTicketStates(ctx)
	if err != nil {
		slog.Warn("list ticket states for nav counts", "error", err)
		return len(tickets), 0, 0
	}

	stateMap := make(map[int64]*ticketLocalState, len(dbStates))
	for _, st := range dbStates {
		var ra *time.Time
		if st.ReviewAfter != nil {
			if t, pErr := time.Parse(time.RFC3339, *st.ReviewAfter); pErr == nil {
				ra = &t
			}
		}
		stateMap[st.TicketID] = &ticketLocalState{
			Priority:    int(st.Priority),
			Blocked:     st.Blocked != 0,
			Note:        st.Note,
			ReviewAfter: ra,
			Today:       st.Today != 0,
		}
	}

	total = len(tickets)
	for _, t := range tickets {
		ls, exists := stateMap[t.ID]
		if exists && ls.Today {
			today++
		}
		// Check triageable
		card := TicketCard{ID: t.ID}
		if isTriageable(card, stateMap) {
			triage++
		}
	}

	return total, today, triage
}

// priorityLabel maps a Freshdesk priority code to a human-readable label.
func priorityLabel(priority int) string {
	switch priority {
	case 1:
		return "Low"
	case 2:
		return "Medium"
	case 3:
		return "High"
	case 4:
		return "Urgent"
	default:
		return ""
	}
}
