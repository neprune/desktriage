// Package freshdesk provides a client for the Freshdesk API v2.
package freshdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config holds the configuration for a Freshdesk API client.
type Config struct {
	BaseURL string // e.g. "https://foosupport.freshdesk.com"
	APIKey  string // used as Basic Auth username (password is "X")
}

// Client is a Freshdesk API v2 client.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient creates a new Freshdesk API client.
func NewClient(cfg Config) *Client {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

// Attachment represents a Freshdesk attachment on a ticket or conversation.
type Attachment struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	URL         string    `json:"attachment_url"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
}

// Ticket represents a Freshdesk ticket.
type Ticket struct {
	ID              int64        `json:"id"`
	Subject         string       `json:"subject"`
	Description     string       `json:"description"`
	DescriptionText string       `json:"description_text"`
	Status          int          `json:"status"`
	Priority        int          `json:"priority"`
	Type            string       `json:"type"`
	Tags            []string     `json:"tags"`
	RequesterID     int64        `json:"requester_id"`
	ResponderID     int64        `json:"responder_id"`
	CompanyID       int64        `json:"company_id"`
	GroupID         int64        `json:"group_id"`
	Source          int          `json:"source"`
	Attachments     []Attachment `json:"attachments"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	DueBy           time.Time    `json:"due_by"`
	FrDueBy         time.Time    `json:"fr_due_by"`
}

// Conversation represents a Freshdesk ticket conversation (note or reply).
type Conversation struct {
	ID          int64        `json:"id"`
	Body        string       `json:"body"`
	BodyText    string       `json:"body_text"`
	Incoming    bool         `json:"incoming"`
	Private     bool         `json:"private"`
	UserID      int64        `json:"user_id"`
	TicketID    int64        `json:"ticket_id"`
	Source      int          `json:"source"`
	FromEmail   string       `json:"from_email"`
	ToEmails    []string     `json:"to_emails"`
	CcEmails    []string     `json:"cc_emails"`
	Attachments []Attachment `json:"attachments"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// Company represents a Freshdesk company.
type Company struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Domains     []string `json:"domains"`
}

// Contact represents a Freshdesk contact.
type Contact struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CompanyID int64  `json:"company_id"`
	Phone     string `json:"phone"`
}

// AgentContact holds the nested contact information within an Agent response.
type AgentContact struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Agent represents a Freshdesk agent.
type Agent struct {
	ID         int64        `json:"id"`
	ContactID  int64        `json:"contact_id"`
	Available  bool         `json:"available"`
	Occasional bool         `json:"occasional"`
	Contact    AgentContact `json:"contact"`
}

// ---------------------------------------------------------------------------
// Internal: HTTP helpers
// ---------------------------------------------------------------------------

// apiError is returned when the Freshdesk API returns a non-2xx status.
type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	snippet := e.Body
	if len(snippet) > 512 {
		snippet = snippet[:512] + "…"
	}
	return fmt.Sprintf("freshdesk: HTTP %d: %s", e.StatusCode, snippet)
}

// doRequest executes an HTTP request against the Freshdesk API.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	fullURL := c.cfg.BaseURL + path

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("freshdesk: building request: %w", err)
	}

	req.SetBasicAuth(c.cfg.APIKey, "X")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("freshdesk: %s %s: %w", method, path, err)
	}

	// Check rate-limit header.
	if rl := resp.Header.Get("X-RateLimit-Remaining"); rl != "" {
		if remaining, err := strconv.Atoi(rl); err == nil && remaining <= 5 {
			slog.Warn("freshdesk: rate limit nearly exhausted",
				"remaining", remaining,
				"method", method,
				"path", path,
			)
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, &apiError{StatusCode: resp.StatusCode, Body: string(snippet)}
	}

	return resp, nil
}

// doJSON executes an HTTP request and decodes the JSON response into T.
func doJSON[T any](c *Client, ctx context.Context, method, path string, body io.Reader) (T, http.Header, error) {
	var zero T

	resp, err := c.doRequest(ctx, method, path, body)
	if err != nil {
		return zero, nil, err
	}
	defer resp.Body.Close()

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return zero, resp.Header, fmt.Errorf("freshdesk: decoding response for %s %s: %w", method, path, err)
	}
	return result, resp.Header, nil
}

// ---------------------------------------------------------------------------
// Pagination helpers
// ---------------------------------------------------------------------------

// paginate collects all pages of a list endpoint that returns []T.
// Freshdesk uses page=1,2,3… and returns an empty array when exhausted.
// perPage is capped at 100 by the API.
func paginate[T any](c *Client, ctx context.Context, basePath string, params url.Values) ([]T, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("per_page", "100")

	var all []T
	for page := 1; ; page++ {
		params.Set("page", strconv.Itoa(page))
		path := basePath + "?" + params.Encode()

		pageResults, _, err := doJSON[[]T](c, ctx, http.MethodGet, path, nil)
		if err != nil {
			return all, err
		}
		if len(pageResults) == 0 {
			break
		}
		all = append(all, pageResults...)
		if len(pageResults) < 100 {
			break // last page
		}
	}
	return all, nil
}

// searchEnvelope is the wrapper returned by Freshdesk search/filter endpoints.
type searchEnvelope[T any] struct {
	Total   int `json:"total"`
	Results []T `json:"results"`
}

// paginateSearch collects all pages from a Freshdesk search endpoint.
// Search results are wrapped in {"total":N, "results":[…]} and use page=1,2,3…
// The search API returns at most 30 results per page.
func paginateSearch[T any](c *Client, ctx context.Context, basePath string, query string) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		params := url.Values{}
		params.Set("query", `"`+query+`"`)
		params.Set("page", strconv.Itoa(page))
		path := basePath + "?" + params.Encode()

		envelope, _, err := doJSON[searchEnvelope[T]](c, ctx, http.MethodGet, path, nil)
		if err != nil {
			return all, err
		}
		all = append(all, envelope.Results...)
		if len(all) >= envelope.Total || len(envelope.Results) == 0 {
			break
		}
	}
	return all, nil
}

// ---------------------------------------------------------------------------
// Public API methods
// ---------------------------------------------------------------------------

// ListTickets retrieves tickets using the list endpoint with the given query
// parameters (e.g. "agent_id", "status", "updated_since", etc.).
// Results are paginated automatically.
func (c *Client) ListTickets(ctx context.Context, params url.Values) ([]Ticket, error) {
	return paginate[Ticket](c, ctx, "/api/v2/tickets", params)
}

// FilterTickets searches tickets using the Freshdesk search/filter API.
// The query should be a Freshdesk filter query string, e.g.:
//
//	"agent_id:123 AND status:2"
func (c *Client) FilterTickets(ctx context.Context, query string) ([]Ticket, error) {
	return paginateSearch[Ticket](c, ctx, "/api/v2/search/tickets", query)
}

// GetTicket retrieves a single ticket by ID.
func (c *Client) GetTicket(ctx context.Context, id int64) (*Ticket, error) {
	path := fmt.Sprintf("/api/v2/tickets/%d", id)
	t, _, err := doJSON[Ticket](c, ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetConversations retrieves all conversations for a ticket, paginated.
func (c *Client) GetConversations(ctx context.Context, ticketID int64) ([]Conversation, error) {
	path := fmt.Sprintf("/api/v2/tickets/%d/conversations", ticketID)
	return paginate[Conversation](c, ctx, path, nil)
}

// GetCompany retrieves a company by ID.
func (c *Client) GetCompany(ctx context.Context, id int64) (*Company, error) {
	path := fmt.Sprintf("/api/v2/companies/%d", id)
	co, _, err := doJSON[Company](c, ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return &co, nil
}

// GetContact retrieves a contact by ID.
func (c *Client) GetContact(ctx context.Context, id int64) (*Contact, error) {
	path := fmt.Sprintf("/api/v2/contacts/%d", id)
	ct, _, err := doJSON[Contact](c, ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return &ct, nil
}

// GetAgent retrieves an agent by ID.
func (c *Client) GetAgent(ctx context.Context, id int64) (*Agent, error) {
	path := fmt.Sprintf("/api/v2/agents/%d", id)
	ag, _, err := doJSON[Agent](c, ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return &ag, nil
}

// GetCurrentAgent retrieves the currently authenticated agent.
func (c *Client) GetCurrentAgent(ctx context.Context) (*Agent, error) {
	ag, _, err := doJSON[Agent](c, ctx, http.MethodGet, "/api/v2/agents/me", nil)
	if err != nil {
		return nil, err
	}
	return &ag, nil
}

// ListAgents returns all agents in the Freshdesk account.
func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	return paginate[Agent](c, ctx, "/api/v2/agents", nil)
}

// ListCompanies returns all companies in the Freshdesk account.
func (c *Client) ListCompanies(ctx context.Context) ([]Company, error) {
	return paginate[Company](c, ctx, "/api/v2/companies", nil)
}

// TicketField represents a Freshdesk ticket field definition.
type TicketField struct {
	ID      int64           `json:"id"`
	Name    string          `json:"name"`
	Label   string          `json:"label"`
	Type    string          `json:"type"`
	Choices json.RawMessage `json:"choices"` // shape varies by field type
}

// StatusChoice represents a single Freshdesk status option.
type StatusChoice struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

// GetTicketFields retrieves all ticket field definitions.
func (c *Client) GetTicketFields(ctx context.Context) ([]TicketField, error) {
	return paginate[TicketField](c, ctx, "/api/v2/ticket_fields", nil)
}

// ParseStatusChoices extracts status choices from the ticket fields list.
// The Freshdesk status field choices are a map with string keys (status code)
// and array values: {"2": ["Open", "Being Processed"], "3": ["Pending", "..."], ...}
// The first element of each array is the agent-facing label.
func ParseStatusChoices(fields []TicketField) []StatusChoice {
	for _, f := range fields {
		if f.Name != "status" || f.Choices == nil {
			continue
		}

		// Try parsing as map of string -> []string (Freshdesk status format)
		var choicesMap map[string][]string
		if err := json.Unmarshal(f.Choices, &choicesMap); err == nil {
			var choices []StatusChoice
			for codeStr, labels := range choicesMap {
				code, err := strconv.Atoi(codeStr)
				if err != nil || len(labels) == 0 {
					continue
				}
				choices = append(choices, StatusChoice{Label: labels[0], Value: code})
			}
			sortStatusChoices(choices)
			return choices
		}

		return nil
	}
	return nil
}

// sortStatusChoices sorts choices by their numeric value.
func sortStatusChoices(choices []StatusChoice) {
	for i := 1; i < len(choices); i++ {
		for j := i; j > 0 && choices[j].Value < choices[j-1].Value; j-- {
			choices[j], choices[j-1] = choices[j-1], choices[j]
		}
	}
}

// UpdateTicketStatus sets the status field on a Freshdesk ticket.
func (c *Client) UpdateTicketStatus(ctx context.Context, ticketID int64, status int) error {
	payload := struct {
		Status int `json:"status"`
	}{
		Status: status,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("freshdesk: marshalling status payload: %w", err)
	}

	path := fmt.Sprintf("/api/v2/tickets/%d", ticketID)
	resp, err := c.doRequest(ctx, http.MethodPut, path, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// UnassignTicket removes the assigned agent from a Freshdesk ticket.
func (c *Client) UnassignTicket(ctx context.Context, ticketID int64) error {
	payload := map[string]any{"responder_id": nil}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("freshdesk: marshalling unassign payload: %w", err)
	}

	path := fmt.Sprintf("/api/v2/tickets/%d", ticketID)
	resp, err := c.doRequest(ctx, http.MethodPut, path, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// CreateNote adds a note (private or public) to a Freshdesk ticket.
func (c *Client) CreateNote(ctx context.Context, ticketID int64, body string, private bool) (*Conversation, error) {
	payload := struct {
		Body    string `json:"body"`
		Private bool   `json:"private"`
	}{
		Body:    body,
		Private: private,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("freshdesk: marshalling note payload: %w", err)
	}

	path := fmt.Sprintf("/api/v2/tickets/%d/notes", ticketID)
	conv, _, err := doJSON[Conversation](c, ctx, http.MethodPost, path, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	return &conv, nil
}
