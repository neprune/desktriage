package freshdesk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestClient starts an httptest.Server with the given handler and returns
// a *Client pointed at it, plus a cleanup function.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	client := NewClient(Config{
		BaseURL: ts.URL,
		APIKey:  "testkey",
	})
	return client, ts
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling JSON: %v", err)
	}
	return data
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestListTickets(t *testing.T) {
	tickets := []Ticket{
		{ID: 1, Subject: "First ticket", Status: 2, Priority: 1},
		{ID: 2, Subject: "Second ticket", Status: 3, Priority: 2},
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v2/tickets") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, tickets))
	})

	got, err := client.ListTickets(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTickets returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tickets, got %d", len(got))
	}
	if got[0].ID != 1 || got[0].Subject != "First ticket" {
		t.Errorf("ticket[0] = %+v, want ID=1, Subject=First ticket", got[0])
	}
	if got[1].ID != 2 || got[1].Subject != "Second ticket" {
		t.Errorf("ticket[1] = %+v, want ID=2, Subject=Second ticket", got[1])
	}
}

func TestListTicketsPagination(t *testing.T) {
	// Build page fixtures: 100 tickets on page 1, 5 on page 2.
	page1 := make([]Ticket, 100)
	for i := range page1 {
		page1[i] = Ticket{ID: int64(i + 1), Subject: fmt.Sprintf("ticket-%d", i+1)}
	}
	page2 := make([]Ticket, 5)
	for i := range page2 {
		page2[i] = Ticket{ID: int64(101 + i), Subject: fmt.Sprintf("ticket-%d", 101+i)}
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			w.Write(mustJSON(t, page1))
		case "2":
			w.Write(mustJSON(t, page2))
		default:
			// Should not be reached; page 2 had < 100 results.
			t.Errorf("unexpected page requested: %s", page)
			w.Write([]byte("[]"))
		}
	})

	got, err := client.ListTickets(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTickets returned error: %v", err)
	}
	if len(got) != 105 {
		t.Fatalf("expected 105 tickets, got %d", len(got))
	}
	if got[0].ID != 1 {
		t.Errorf("first ticket ID = %d, want 1", got[0].ID)
	}
	if got[104].ID != 105 {
		t.Errorf("last ticket ID = %d, want 105", got[104].ID)
	}
}

func TestFilterTickets(t *testing.T) {
	envelope := struct {
		Total   int      `json:"total"`
		Results []Ticket `json:"results"`
	}{
		Total: 2,
		Results: []Ticket{
			{ID: 10, Subject: "Filtered A", Status: 2},
			{ID: 11, Subject: "Filtered B", Status: 3},
		},
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v2/search/tickets") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query().Get("query")
		// The client wraps the query in double quotes.
		if q != `"status:2 AND agent_id:5"` {
			t.Errorf("unexpected query param: %s", q)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, envelope))
	})

	got, err := client.FilterTickets(context.Background(), "status:2 AND agent_id:5")
	if err != nil {
		t.Fatalf("FilterTickets returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tickets, got %d", len(got))
	}
	if got[0].ID != 10 || got[0].Subject != "Filtered A" {
		t.Errorf("ticket[0] = %+v, want ID=10 Subject=Filtered A", got[0])
	}
	if got[1].ID != 11 {
		t.Errorf("ticket[1].ID = %d, want 11", got[1].ID)
	}
}

func TestGetTicket(t *testing.T) {
	ticket := Ticket{ID: 42, Subject: "Important issue", Status: 2, Priority: 3}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/tickets/42" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, ticket))
	})

	got, err := client.GetTicket(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetTicket returned error: %v", err)
	}
	if got.ID != 42 {
		t.Errorf("ID = %d, want 42", got.ID)
	}
	if got.Subject != "Important issue" {
		t.Errorf("Subject = %q, want %q", got.Subject, "Important issue")
	}
	if got.Priority != 3 {
		t.Errorf("Priority = %d, want 3", got.Priority)
	}
}

func TestGetConversations(t *testing.T) {
	conversations := []Conversation{
		{ID: 100, Body: "<p>Hello</p>", BodyText: "Hello", UserID: 1, TicketID: 7, Incoming: true},
		{ID: 101, Body: "<p>Reply</p>", BodyText: "Reply", UserID: 2, TicketID: 7, Incoming: false},
		{ID: 102, Body: "<p>Note</p>", BodyText: "Note", UserID: 2, TicketID: 7, Private: true},
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/tickets/7/conversations" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, conversations))
	})

	got, err := client.GetConversations(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetConversations returned error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 conversations, got %d", len(got))
	}
	if got[0].ID != 100 || got[0].BodyText != "Hello" || !got[0].Incoming {
		t.Errorf("conversation[0] = %+v, unexpected values", got[0])
	}
	if got[2].ID != 102 || !got[2].Private {
		t.Errorf("conversation[2] = %+v, expected private note", got[2])
	}
}

func TestGetCompany(t *testing.T) {
	company := Company{
		ID:          300,
		Name:        "Acme Corp",
		Description: "A fine company",
		Domains:     []string{"acme.com", "acme.co.uk"},
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/companies/300" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, company))
	})

	got, err := client.GetCompany(context.Background(), 300)
	if err != nil {
		t.Fatalf("GetCompany returned error: %v", err)
	}
	if got.ID != 300 {
		t.Errorf("ID = %d, want 300", got.ID)
	}
	if got.Name != "Acme Corp" {
		t.Errorf("Name = %q, want %q", got.Name, "Acme Corp")
	}
	if len(got.Domains) != 2 || got.Domains[0] != "acme.com" {
		t.Errorf("Domains = %v, want [acme.com acme.co.uk]", got.Domains)
	}
}

func TestGetContact(t *testing.T) {
	contact := Contact{
		ID:        400,
		Name:      "Sam Sample",
		Email:     "sam@example.com",
		CompanyID: 300,
		Phone:     "+1-555-0199",
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/contacts/400" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, contact))
	})

	got, err := client.GetContact(context.Background(), 400)
	if err != nil {
		t.Fatalf("GetContact returned error: %v", err)
	}
	if got.ID != 400 {
		t.Errorf("ID = %d, want 400", got.ID)
	}
	if got.Name != "Sam Sample" {
		t.Errorf("Name = %q, want %q", got.Name, "Sam Sample")
	}
	if got.Email != "sam@example.com" {
		t.Errorf("Email = %q, want %q", got.Email, "sam@example.com")
	}
	if got.Phone != "+1-555-0199" {
		t.Errorf("Phone = %q, want %q", got.Phone, "+1-555-0199")
	}
}

func TestGetCurrentAgent(t *testing.T) {
	agent := Agent{
		ID:        500,
		ContactID: 501,
		Available: true,
		Contact: AgentContact{
			Name:  "Support Bot",
			Email: "bot@example.com",
		},
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/agents/me" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, agent))
	})

	got, err := client.GetCurrentAgent(context.Background())
	if err != nil {
		t.Fatalf("GetCurrentAgent returned error: %v", err)
	}
	if got.ID != 500 {
		t.Errorf("ID = %d, want 500", got.ID)
	}
	if !got.Available {
		t.Errorf("Available = false, want true")
	}
	if got.Contact.Name != "Support Bot" {
		t.Errorf("Contact.Name = %q, want %q", got.Contact.Name, "Support Bot")
	}
	if got.Contact.Email != "bot@example.com" {
		t.Errorf("Contact.Email = %q, want %q", got.Contact.Email, "bot@example.com")
	}
}

func TestAPIError(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"description":"The requested resource was not found."}`))
	})

	_, err := client.GetTicket(context.Background(), 9999)
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "404") {
		t.Errorf("error message %q does not contain status code 404", errMsg)
	}
	if !strings.Contains(errMsg, "not found") {
		t.Errorf("error message %q does not contain response body snippet", errMsg)
	}

	// Also verify it's an *apiError with the right StatusCode.
	var ae *apiError
	if ok := errorAs(err, &ae); !ok {
		t.Fatalf("expected *apiError, got %T", err)
	}
	if ae.StatusCode != 404 {
		t.Errorf("apiError.StatusCode = %d, want 404", ae.StatusCode)
	}
}

// errorAs is a tiny helper wrapping errors.As to avoid importing errors just
// for a single call (it's also used to keep the test file self-contained).
func errorAs[T any](err error, target *T) bool {
	for err != nil {
		if t, ok := err.(T); ok {
			*target = t
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestBasicAuth(t *testing.T) {
	const apiKey = "my-secret-api-key"
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(apiKey+":X"))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth := r.Header.Get("Authorization")
		if gotAuth != wantAuth {
			t.Errorf("Authorization header = %q, want %q", gotAuth, wantAuth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, Agent{ID: 1, Contact: AgentContact{Name: "Test"}}))
	}))
	t.Cleanup(ts.Close)

	client := NewClient(Config{
		BaseURL: ts.URL,
		APIKey:  apiKey,
	})

	_, err := client.GetCurrentAgent(context.Background())
	if err != nil {
		t.Fatalf("GetCurrentAgent returned error: %v", err)
	}
}

func TestCreateNote(t *testing.T) {
	wantConv := Conversation{
		ID:       9001,
		Body:     "<p>This is a private note</p>",
		BodyText: "This is a private note",
		Private:  true,
		UserID:   42,
		TicketID: 6716,
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify method.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// Verify path.
		if r.URL.Path != "/api/v2/tickets/6716/notes" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// Verify Authorization header is present (Basic auth).
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Basic ") {
			t.Errorf("expected Basic auth header, got %q", auth)
		}
		// Verify JSON body.
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if _, ok := reqBody["body"]; !ok {
			t.Errorf("request body missing 'body' field")
		}
		if reqBody["body"] != "This is a private note" {
			t.Errorf("body = %q, want %q", reqBody["body"], "This is a private note")
		}
		if reqBody["private"] != true {
			t.Errorf("private = %v, want true", reqBody["private"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(mustJSON(t, wantConv))
	})

	got, err := client.CreateNote(context.Background(), 6716, "This is a private note", true)
	if err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}
	if got.ID != 9001 {
		t.Errorf("ID = %d, want 9001", got.ID)
	}
	if got.TicketID != 6716 {
		t.Errorf("TicketID = %d, want 6716", got.TicketID)
	}
	if !got.Private {
		t.Errorf("Private = false, want true")
	}
	if got.BodyText != "This is a private note" {
		t.Errorf("BodyText = %q, want %q", got.BodyText, "This is a private note")
	}
	if got.UserID != 42 {
		t.Errorf("UserID = %d, want 42", got.UserID)
	}
}

func TestListTicketsPassesParams(t *testing.T) {
	// Verify that user-supplied query parameters are forwarded.
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("agent_id"); got != "77" {
			t.Errorf("agent_id param = %q, want %q", got, "77")
		}
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Errorf("per_page param = %q, want %q", got, "100")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	})

	params := url.Values{}
	params.Set("agent_id", "77")

	_, err := client.ListTickets(context.Background(), params)
	if err != nil {
		t.Fatalf("ListTickets returned error: %v", err)
	}
}
