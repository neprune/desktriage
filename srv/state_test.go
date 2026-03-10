package srv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"desktriage.davea.me/cache"
	"desktriage.davea.me/config"
	"desktriage.davea.me/db"
	"desktriage.davea.me/db/dbgen"
	"desktriage.davea.me/freshdesk"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	wdb, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RunMigrations(wdb); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { wdb.Close() })

	return &Server{
		DB:           wdb,
		Queries:      dbgen.New(wdb),
		Cache:        cache.NewStore(wdb),
		Config:       &config.Config{},
		TemplatesDir: "templates",
		StaticDir:    "static",
		apiSem:       make(chan struct{}, 10),
	}
}

// newTestServerWithFreshdesk creates a test server backed by a mock Freshdesk
// API. The handler is called for every request to the mock server.
func newTestServerWithFreshdesk(t *testing.T, handler http.HandlerFunc) *Server {
	t.Helper()
	s := newTestServer(t)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	s.Freshdesk = freshdesk.NewClient(freshdesk.Config{
		BaseURL: ts.URL,
		APIKey:  "testkey",
	})
	return s
}

// seedCachedTicket puts a fake ticket in the dashboard cache.
func seedCachedTicket(t *testing.T, s *Server, ticketID int64) {
	t.Helper()
	tickets := []freshdesk.Ticket{{
		ID:          ticketID,
		Subject:     "Test ticket",
		Status:      2,
		CompanyID:   0,
		ResponderID: 0,
		UpdatedAt:   time.Now().Add(-1 * time.Hour),
	}}
	data, _ := json.Marshal(tickets)
	// Directly insert into cache
	_, err := s.DB.ExecContext(context.Background(),
		`INSERT OR REPLACE INTO api_cache (cache_key, response_body, cached_at, max_age_secs) VALUES (?, ?, ?, ?)`,
		"tickets:dashboard", string(data), time.Now().UTC().Format(time.RFC3339), 3600)
	if err != nil {
		t.Fatal(err)
	}
}

// seedCachedTicketWithResponder puts a fake ticket with a specific responder in the dashboard cache.
func seedCachedTicketWithResponder(t *testing.T, s *Server, ticketID, responderID int64) {
	t.Helper()
	tickets := []freshdesk.Ticket{{
		ID:          ticketID,
		Subject:     "Test ticket",
		Status:      2,
		ResponderID: responderID,
		UpdatedAt:   time.Now().Add(-1 * time.Hour),
	}}
	data, _ := json.Marshal(tickets)
	_, err := s.DB.ExecContext(context.Background(),
		`INSERT OR REPLACE INTO api_cache (cache_key, response_body, cached_at, max_age_secs) VALUES (?, ?, ?, ?)`,
		"tickets:dashboard", string(data), time.Now().UTC().Format(time.RFC3339), 3600)
	if err != nil {
		t.Fatal(err)
	}
}

// serveFakeTicket returns true and writes a JSON ticket response if the
// request is a GET for /api/v2/tickets/{id}. Used as a fallback in test mocks
// so buildTicketCard can fetch the ticket after cache invalidation.
func serveFakeTicket(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "GET" {
		return false
	}
	// Match /api/v2/tickets/<digits> but not /api/v2/tickets/<digits>/...
	path := r.URL.Path
	const prefix = "/api/v2/tickets/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := path[len(prefix):]
	if rest == "" || strings.Contains(rest, "/") {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(freshdesk.Ticket{
		ID:      12345,
		Subject: "Test ticket",
		Status:  2,
	})
	return true
}

func putState(s *Server, ticketID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PUT", "/ticket/"+ticketID+"/state", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", ticketID)
	rr := httptest.NewRecorder()
	s.HandleUpdateState(rr, req)
	return rr
}

func TestUpdateState_Today(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=today&value=1")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	st, err := s.Queries.GetTicketState(context.Background(), 12345)
	if err != nil {
		t.Fatal(err)
	}
	if st.Today != 1 {
		t.Errorf("today: want 1, got %d", st.Today)
	}

	// Verify partial contains "Today" badge
	if !strings.Contains(rr.Body.String(), "badge-today") {
		t.Error("response should contain badge-today")
	}
}

func TestUpdateState_Blocked(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=blocked&value=1")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Blocked != 1 {
		t.Errorf("blocked: want 1, got %d", st.Blocked)
	}
}

func TestUpdateState_Note(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=note&value=hello+world")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Note != "hello world" {
		t.Errorf("note: want 'hello world', got %q", st.Note)
	}
}

func TestUpdateState_ReviewAfter(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=review_after&value=2026-04-15")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.ReviewAfter == nil || !strings.HasPrefix(*st.ReviewAfter, "2026-04-15") {
		t.Errorf("review_after: want 2026-04-15..., got %v", st.ReviewAfter)
	}

	// Verify badge in response
	if !strings.Contains(rr.Body.String(), "Review: Apr 15") {
		t.Error("response should contain review date")
	}
}

func TestUpdateState_ReviewAfterClear(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	putState(s, "12345", "field=review_after&value=2026-04-15")
	rr := putState(s, "12345", "field=review_after&value=")

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.ReviewAfter != nil {
		t.Errorf("review_after: want nil, got %v", st.ReviewAfter)
	}
}

func TestUpdateState_Priority(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=priority&value=3")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Priority != 3 {
		t.Errorf("priority: want 3, got %d", st.Priority)
	}

	if !strings.Contains(rr.Body.String(), "#3") {
		t.Error("response should contain priority badge")
	}
}

func TestUpdateState_ToggleOff(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	putState(s, "12345", "field=today&value=1")
	rr := putState(s, "12345", "field=today&value=0")

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d", rr.Code)
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Today != 0 {
		t.Errorf("today: want 0, got %d", st.Today)
	}
}

func TestUpdateState_PreservesOtherFields(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	putState(s, "12345", "field=today&value=1")
	putState(s, "12345", "field=blocked&value=1")

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Today != 1 {
		t.Errorf("today should be preserved: want 1, got %d", st.Today)
	}
	if st.Blocked != 1 {
		t.Errorf("blocked: want 1, got %d", st.Blocked)
	}
}

func TestUpdateState_Dismiss(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=dismiss&value=1")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Priority != -1 {
		t.Errorf("priority: want -1 (dismissed), got %d", st.Priority)
	}
}

func TestUpdateState_FromToday_ReturnsEmpty(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=today&value=0&from=today")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("expected empty body for today unflag, got %d bytes", rr.Body.Len())
	}
}

func TestUpdateState_InvalidField(t *testing.T) {
	s := newTestServer(t)
	rr := putState(s, "12345", "field=nope&value=1")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}

func TestUpdateState_InvalidID(t *testing.T) {
	s := newTestServer(t)
	rr := putState(s, "abc", "field=today&value=1")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}

func TestUpdateState_FromTicket_ReturnsStatePartial(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=today&value=1&from=ticket")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	// Should return the ticket-state partial (not empty, not the ticket-card)
	body := rr.Body.String()
	if body == "" {
		t.Error("expected non-empty body for ticket state partial")
	}
	if !strings.Contains(body, "state-panel") {
		t.Error("response should contain state-panel class")
	}
	if !strings.Contains(body, "Yes") {
		t.Error("response should show Today as Yes")
	}

	// Verify state was saved
	st, err := s.Queries.GetTicketState(context.Background(), 12345)
	if err != nil {
		t.Fatal(err)
	}
	if st.Today != 1 {
		t.Errorf("today: want 1, got %d", st.Today)
	}
}

func TestUpdateState_FromTicket_Priority(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=priority&value=2&from=ticket")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "state-panel") {
		t.Error("response should contain state-panel")
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Priority != 2 {
		t.Errorf("priority: want 2, got %d", st.Priority)
	}
}

func TestUpdateState_TodayAssignsTicket(t *testing.T) {
	var assignCalled bool
	var assignedResponder float64

	s := newTestServerWithFreshdesk(t, func(w http.ResponseWriter, r *http.Request) {
		// /api/v2/agents/me — return a fake agent for ensureAgentID
		if r.URL.Path == "/api/v2/agents/me" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(freshdesk.Agent{
				ID:      999,
				Contact: freshdesk.AgentContact{Name: "Test Agent"},
			})
			return
		}
		// PUT /api/v2/tickets/12345 — the assign call
		if r.Method == "PUT" && r.URL.Path == "/api/v2/tickets/12345" {
			assignCalled = true
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if v, ok := body["responder_id"]; ok {
				assignedResponder, _ = v.(float64)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if !serveFakeTicket(w, r) {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=today&value=1")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	if !assignCalled {
		t.Error("expected Freshdesk assign API call when marking today, but it was not called")
	}
	if assignedResponder != 999 {
		t.Errorf("responder_id = %v, want 999", assignedResponder)
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Today != 1 {
		t.Errorf("today: want 1, got %d", st.Today)
	}
}

func TestUpdateState_UntodayDoesNotUnassign(t *testing.T) {
	var putCalls int

	s := newTestServerWithFreshdesk(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/agents/me" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(freshdesk.Agent{
				ID:      999,
				Contact: freshdesk.AgentContact{Name: "Test Agent"},
			})
			return
		}
		if r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/v2/tickets/") {
			putCalls++
			w.WriteHeader(http.StatusOK)
			return
		}
		if !serveFakeTicket(w, r) {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	seedCachedTicket(t, s, 12345)

	// Mark as today (triggers assign).
	putState(s, "12345", "field=today&value=1")
	assignCalls := putCalls

	// Unmark today — should NOT trigger another PUT.
	putState(s, "12345", "field=today&value=0")

	if putCalls != assignCalls {
		t.Errorf("unmarking today triggered %d additional Freshdesk PUT calls, want 0", putCalls-assignCalls)
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Today != 0 {
		t.Errorf("today: want 0, got %d", st.Today)
	}
}

func TestUpdateState_TodayToggleOnAssigns(t *testing.T) {
	var assignCalled bool

	s := newTestServerWithFreshdesk(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/agents/me" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(freshdesk.Agent{
				ID:      999,
				Contact: freshdesk.AgentContact{Name: "Test Agent"},
			})
			return
		}
		if r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/v2/tickets/") {
			assignCalled = true
			w.WriteHeader(http.StatusOK)
			return
		}
		if !serveFakeTicket(w, r) {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	seedCachedTicket(t, s, 12345)

	// Toggle on (ticket starts with today=0, so toggle should set it to 1).
	rr := putState(s, "12345", "field=today&value=toggle")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	if !assignCalled {
		t.Error("expected assign call on toggle to today=1")
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Today != 1 {
		t.Errorf("today: want 1, got %d", st.Today)
	}
}

func TestUpdateState_TodayToggleOffDoesNotUnassign(t *testing.T) {
	var putCalls int

	s := newTestServerWithFreshdesk(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/agents/me" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(freshdesk.Agent{
				ID:      999,
				Contact: freshdesk.AgentContact{Name: "Test Agent"},
			})
			return
		}
		if r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/v2/tickets/") {
			putCalls++
			w.WriteHeader(http.StatusOK)
			return
		}
		if !serveFakeTicket(w, r) {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	seedCachedTicket(t, s, 12345)

	// Set today=1 first (triggers assign).
	putState(s, "12345", "field=today&value=1")
	callsAfterOn := putCalls

	// Toggle off (today=1 -> 0) — should NOT call assign/unassign.
	putState(s, "12345", "field=today&value=toggle")

	if putCalls != callsAfterOn {
		t.Errorf("toggle off triggered %d additional Freshdesk PUT calls, want 0", putCalls-callsAfterOn)
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Today != 0 {
		t.Errorf("today: want 0, got %d", st.Today)
	}
}

func TestUpdateState_TodaySkipsAssignIfAlreadyAssigned(t *testing.T) {
	var putCalls int

	s := newTestServerWithFreshdesk(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/agents/me" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(freshdesk.Agent{
				ID:      999,
				Contact: freshdesk.AgentContact{Name: "Test Agent"},
			})
			return
		}
		if r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/v2/tickets/") {
			putCalls++
			w.WriteHeader(http.StatusOK)
			return
		}
		if !serveFakeTicket(w, r) {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	// Seed with ticket already assigned to agent 999.
	seedCachedTicketWithResponder(t, s, 12345, 999)
	// Ensure AgentID is resolved before the test so ensureAgentID sets it.
	s.AgentID = 999

	rr := putState(s, "12345", "field=today&value=1")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	if putCalls != 0 {
		t.Errorf("expected 0 Freshdesk PUT calls (already assigned), got %d", putCalls)
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Today != 1 {
		t.Errorf("today: want 1, got %d", st.Today)
	}
}

func TestUpdateState_TodayAssignFailureNonFatal(t *testing.T) {
	s := newTestServerWithFreshdesk(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/agents/me" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(freshdesk.Agent{
				ID:      999,
				Contact: freshdesk.AgentContact{Name: "Test Agent"},
			})
			return
		}
		// Simulate Freshdesk API failure.
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"description":"Server error"}`))
	})
	seedCachedTicket(t, s, 12345)

	// Should still succeed — assign failure is non-fatal.
	rr := putState(s, "12345", "field=today&value=1")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d: %s, want 200 (assign failure should be non-fatal)", rr.Code, rr.Body.String())
	}

	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Today != 1 {
		t.Errorf("today: want 1, got %d", st.Today)
	}
}

func TestNextWeekday(t *testing.T) {
	cases := []struct {
		name    string
		now     time.Time
		wantDay time.Weekday
		wantOff int // expected days ahead
	}{
		{"Monday", date(2026, 3, 2), time.Tuesday, 1},    // Mon -> Tue
		{"Thursday", date(2026, 3, 5), time.Friday, 1},    // Thu -> Fri
		{"Friday", date(2026, 3, 6), time.Monday, 3},      // Fri -> Mon
		{"Saturday", date(2026, 3, 7), time.Monday, 2},    // Sat -> Mon
		{"Sunday", date(2026, 3, 8), time.Monday, 1},      // Sun -> Mon
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextWeekday(tc.now)
			if got.Weekday() != tc.wantDay {
				t.Errorf("weekday: want %s, got %s", tc.wantDay, got.Weekday())
			}
			want := tc.now.AddDate(0, 0, tc.wantOff).Truncate(24 * time.Hour)
			if !got.Equal(want) {
				t.Errorf("date: want %s, got %s", want, got)
			}
		})
	}
}

func date(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 14, 30, 0, 0, time.UTC)
}
