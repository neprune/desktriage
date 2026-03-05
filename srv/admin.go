package srv

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"desktriage.davea.me/config"
	"desktriage.davea.me/db/dbgen"
	"desktriage.davea.me/freshdesk"
)

// AdminConfigData is the template data for the admin config page.
type AdminConfigData struct {
	Page      string
	PageTitle string
	// Shared nav counts (needed by layout)
	TotalCount  int
	TodayCount  int
	// Config-specific
	Keys    []config.ConfigDef
	Values  map[string]string
	Saved   bool
	Error   string
	// Sprint anchors
	SprintAnchors []SprintAnchorView
	CurrentSprint string // e.g. "S5 (2026)"
	CurrentYear   int
	// Account managers
	AccountManagers []AccountManagerView
	AllAgents       []freshdesk.Agent
	AllCompanies    []freshdesk.Company
}

// HandleAdminConfig renders the admin config page.
func (s *Server) HandleAdminConfig(w http.ResponseWriter, r *http.Request) {
	data := s.buildAdminConfigData(r)
	if r.URL.Query().Get("saved") == "1" {
		data.Saved = true
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderTemplate(w, "admin_config.html", data); err != nil {
		slog.Warn("render admin config", "error", err)
	}
}

// HandleAdminConfigSave saves config values from the form.
func (s *Server) HandleAdminConfigSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form data", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	for _, def := range config.KnownKeys {
		val := strings.TrimSpace(r.FormValue("cfg_" + def.Key))
		if val == "" && def.Required {
			data := s.buildAdminConfigData(r)
			data.Error = def.Key + " is required"
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnprocessableEntity)
			if err := s.renderTemplate(w, "admin_config.html", data); err != nil {
				slog.Warn("render admin config", "error", err)
			}
			return
		}
		if val == "" {
			// Delete optional keys that are blank
			if err := s.Queries.DeleteConfig(ctx, def.Key); err != nil {
				slog.Warn("delete config", "key", def.Key, "error", err)
			}
			continue
		}
		if err := s.Queries.UpsertConfig(ctx, dbgen.UpsertConfigParams{
			Key:         def.Key,
			Value:       val,
			Description: def.Description,
		}); err != nil {
			slog.Error("save config", "key", def.Key, "error", err)
			http.Error(w, "Failed to save config", http.StatusInternalServerError)
			return
		}
	}

	// Reload config into server
	cfg, err := config.LoadFromDB(ctx, s.Queries)
	if err != nil {
		slog.Error("reload config", "error", err)
	} else {
		s.Config = cfg
		// Rebuild Freshdesk client with new config
		s.Freshdesk = newFreshdeskClient(cfg)
	}

	http.Redirect(w, r, "/admin/config?saved=1", http.StatusSeeOther)
}

func (s *Server) buildAdminConfigData(r *http.Request) AdminConfigData {
	ctx := r.Context()
	rows, err := s.Queries.ListConfig(ctx)
	if err != nil {
		slog.Error("list config", "error", err)
	}
	values := make(map[string]string, len(rows))
	for _, r := range rows {
		values[r.Key] = r.Value
	}

	// Sprint anchors
	anchorRows, err := s.Queries.ListSprintAnchors(ctx)
	if err != nil {
		slog.Warn("list sprint anchors", "error", err)
	}

	return AdminConfigData{
		Page:            "admin",
		PageTitle:       "DeskTriage — Settings",
		Keys:            config.KnownKeys,
		Values:          values,
		SprintAnchors:   sprintAnchorViews(anchorRows),
		CurrentSprint:   s.currentSprintLabel(ctx),
		CurrentYear:     time.Now().Year(),
		AccountManagers: s.loadAccountManagerViews(ctx),
		AllAgents:       s.loadAgentsForAdmin(ctx),
		AllCompanies:    s.loadCompaniesForAdmin(ctx),
	}
}
