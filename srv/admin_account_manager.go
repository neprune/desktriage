package srv

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"desktriage.davea.me/db/dbgen"
	"desktriage.davea.me/freshdesk"
)

// AccountManagerView is a display-ready account manager assignment.
type AccountManagerView struct {
	CompanyID   int64
	CompanyName string
	AgentID     int64
	AgentName   string
}

// HandleAdminAccountManagerAdd adds an account manager assignment.
func (s *Server) HandleAdminAccountManagerAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form data", http.StatusBadRequest)
		return
	}

	companyID, err := strconv.ParseInt(r.FormValue("company_id"), 10, 64)
	if err != nil || companyID == 0 {
		http.Error(w, "invalid company", http.StatusBadRequest)
		return
	}
	agentID, err := strconv.ParseInt(r.FormValue("agent_id"), 10, 64)
	if err != nil || agentID == 0 {
		http.Error(w, "invalid agent", http.StatusBadRequest)
		return
	}

	if err := s.Queries.InsertAccountManager(r.Context(), dbgen.InsertAccountManagerParams{
		CompanyID: companyID,
		AgentID:   agentID,
	}); err != nil {
		slog.Error("insert account manager", "error", err)
		http.Error(w, "Failed to save", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/config?saved=1", http.StatusSeeOther)
}

// HandleAdminAccountManagerDelete removes an account manager assignment.
func (s *Server) HandleAdminAccountManagerDelete(w http.ResponseWriter, r *http.Request) {
	companyID, err := strconv.ParseInt(r.PathValue("company_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid company_id", http.StatusBadRequest)
		return
	}
	agentID, err := strconv.ParseInt(r.PathValue("agent_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid agent_id", http.StatusBadRequest)
		return
	}

	if err := s.Queries.DeleteAccountManager(r.Context(), dbgen.DeleteAccountManagerParams{
		CompanyID: companyID,
		AgentID:   agentID,
	}); err != nil {
		slog.Error("delete account manager", "error", err)
		http.Error(w, "Failed to delete", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/config?saved=1", http.StatusSeeOther)
}

// loadAccountManagerViews returns display-ready account manager rows.
func (s *Server) loadAccountManagerViews(ctx context.Context) []AccountManagerView {
	rows, err := s.Queries.ListAccountManagers(ctx)
	if err != nil {
		slog.Warn("list account managers", "error", err)
		return nil
	}

	views := make([]AccountManagerView, 0, len(rows))
	for _, row := range rows {
		views = append(views, AccountManagerView{
			CompanyID:   row.CompanyID,
			CompanyName: s.resolveCompanyName(ctx, row.CompanyID),
			AgentID:     row.AgentID,
			AgentName:   s.resolveAgentName(ctx, row.AgentID),
		})
	}
	return views
}

// resolveAgentName returns the full name for a Freshdesk agent.
func (s *Server) resolveAgentName(ctx context.Context, agentID int64) string {
	name, _ := s.resolveContact(ctx, agentID)
	return name
}

// resolveAgentFirstName returns just the first name for a Freshdesk agent.
func (s *Server) resolveAgentFirstName(ctx context.Context, agentID int64) string {
	name := s.resolveAgentName(ctx, agentID)
	if name == "" || name == "Unknown" {
		return name
	}
	parts := strings.Fields(name)
	return parts[0]
}

// companyDisplayName returns the company name with account manager first names
// in parentheses, e.g. "Borsetshire Council (Sam)".
func (s *Server) companyDisplayName(ctx context.Context, companyID int64, companyName string) string {
	if companyID == 0 || companyName == "" {
		return companyName
	}

	managers, err := s.Queries.ListAccountManagersByCompany(ctx, companyID)
	if err != nil {
		slog.Warn("list account managers for company", "company_id", companyID, "error", err)
		return companyName
	}
	if len(managers) == 0 {
		return companyName
	}

	names := make([]string, 0, len(managers))
	for _, m := range managers {
		firstName := s.resolveAgentFirstName(ctx, m.AgentID)
		if firstName != "" && firstName != "Unknown" {
			names = append(names, firstName)
		}
	}
	if len(names) == 0 {
		return companyName
	}

	return fmt.Sprintf("%s (%s)", companyName, strings.Join(names, ", "))
}

// loadAgentsForAdmin returns all Freshdesk agents (cached 7 days).
func (s *Server) loadAgentsForAdmin(ctx context.Context) []freshdesk.Agent {
	cacheKey := "admin:agents"
	var agents []freshdesk.Agent
	ok, err := s.Cache.GetJSON(ctx, cacheKey, &agents)
	if err != nil {
		slog.Warn("cache read for agents list", "error", err)
	}
	if ok {
		return agents
	}

	agents, err = s.Freshdesk.ListAgents(ctx)
	if err != nil {
		slog.Warn("fetch agents list", "error", err)
		return nil
	}

	_ = s.Cache.SetJSON(ctx, cacheKey, agents, 7*24*time.Hour)
	return agents
}

// loadCompaniesForAdmin returns all Freshdesk companies (cached 7 days).
func (s *Server) loadCompaniesForAdmin(ctx context.Context) []freshdesk.Company {
	cacheKey := "admin:companies"
	var companies []freshdesk.Company
	ok, err := s.Cache.GetJSON(ctx, cacheKey, &companies)
	if err != nil {
		slog.Warn("cache read for companies list", "error", err)
	}
	if ok {
		return companies
	}

	companies, err = s.Freshdesk.ListCompanies(ctx)
	if err != nil {
		slog.Warn("fetch companies list", "error", err)
		return nil
	}

	_ = s.Cache.SetJSON(ctx, cacheKey, companies, 7*24*time.Hour)
	return companies
}
