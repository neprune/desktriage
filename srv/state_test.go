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

func TestUpdateState_FromTriage_ReturnsEmpty(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=today&value=1&from=triage")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("expected empty body for triage action, got %d bytes", rr.Body.Len())
	}

	// But the state should still be saved
	st, _ := s.Queries.GetTicketState(context.Background(), 12345)
	if st.Today != 1 {
		t.Errorf("today: want 1, got %d", st.Today)
	}
}

func TestUpdateState_FromFocus_ReturnsEmpty(t *testing.T) {
	s := newTestServer(t)
	seedCachedTicket(t, s, 12345)

	rr := putState(s, "12345", "field=today&value=0&from=focus")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("expected empty body for focus unflag, got %d bytes", rr.Body.Len())
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
