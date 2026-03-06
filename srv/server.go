package srv

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"desktriage.davea.me/cache"
	"desktriage.davea.me/config"
	"desktriage.davea.me/db"
	"desktriage.davea.me/db/dbgen"
	"desktriage.davea.me/freshdesk"
)

// Server is the main application server.
type Server struct {
	DB           *sql.DB
	Queries      *dbgen.Queries
	Cache        *cache.Store
	Freshdesk    *freshdesk.Client
	Config       *config.Config
	AgentID      int64 // resolved on startup
	TemplatesDir string
	StaticDir    string
	apiSem       chan struct{} // bounds concurrent Freshdesk API calls
	staticHash   string        // cache-busting hash of static assets
	noteLimiter  *rateLimiter  // rate limits note creation per ticket
}

// New creates a new server with all dependencies wired up (opens its own DB).
func New(dbPath string, cfg *config.Config) (*Server, error) {
	wdb, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.RunMigrations(wdb); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	return NewWithDB(wdb, cfg)
}

// NewWithDB creates a new server using an already-opened database.
func NewWithDB(wdb *sql.DB, cfg *config.Config) (*Server, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(thisFile)

	srv := &Server{
		Config:       cfg,
		TemplatesDir: filepath.Join(baseDir, "templates"),
		StaticDir:    filepath.Join(baseDir, "static"),
	}

	srv.DB = wdb
	srv.Queries = dbgen.New(wdb)
	srv.Cache = cache.NewStore(wdb)

	// Freshdesk client
	srv.Freshdesk = freshdesk.NewClient(freshdesk.Config{
		BaseURL: cfg.FreshdeskURL,
		APIKey:  cfg.FreshdeskKey,
	})
	srv.apiSem = make(chan struct{}, 10)
	srv.staticHash = computeStaticHash(srv.StaticDir)
	srv.noteLimiter = newRateLimiter()

	return srv, nil
}

// Serve starts the HTTP server.
func (s *Server) Serve(addr string) error {
	return s.ServeWithContext(context.Background(), addr)
}

// ServeWithContext starts the HTTP server and shuts down gracefully when ctx is cancelled.
func (s *Server) ServeWithContext(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("GET /login", s.HandleLogin)
	mux.HandleFunc("POST /login", s.HandleLoginSubmit)
	mux.HandleFunc("POST /logout", s.HandleLogout)

	// Pages
	mux.HandleFunc("GET /{$}", s.HandleDashboard)
	mux.HandleFunc("GET /today", s.HandleToday)
	mux.HandleFunc("POST /refresh", s.HandleRefresh)

	// Ticket detail
	mux.HandleFunc("GET /ticket/{id}", s.HandleTicketDetail)

	// API (htmx)
	mux.HandleFunc("PUT /ticket/{id}/state", s.HandleUpdateState)
	mux.HandleFunc("GET /ticket/{id}/preview", s.HandleTicketPreview)
	mux.HandleFunc("POST /ticket/{id}/note", s.HandleAddNote)
	mux.HandleFunc("POST /ticket/{id}/freshdesk-status", s.HandleUpdateFreshdeskStatus)
	mux.HandleFunc("GET /api/status-choices", s.HandleStatusChoices)

	// Admin
	mux.HandleFunc("GET /admin/config", s.HandleAdminConfig)
	mux.HandleFunc("POST /admin/config", s.HandleAdminConfigSave)
	mux.HandleFunc("POST /admin/sprint-anchor", s.HandleAdminSprintAnchorAdd)
	mux.HandleFunc("DELETE /admin/sprint-anchor/{year}/{sprint}", s.HandleAdminSprintAnchorDelete)
	mux.HandleFunc("POST /admin/account-manager", s.HandleAdminAccountManagerAdd)
	mux.HandleFunc("DELETE /admin/account-manager/{company_id}/{agent_id}", s.HandleAdminAccountManagerDelete)

	// Service worker at root scope (required for PWA)
	mux.HandleFunc("GET /sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-cache, max-age=0")
		http.ServeFile(w, r, filepath.Join(s.StaticDir, "sw.js"))
	})

	// Static files (short cache for quick iteration)
	staticFS := http.StripPrefix("/static/", http.FileServer(http.Dir(s.StaticDir)))
	mux.Handle("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, max-age=0")
		staticFS.ServeHTTP(w, r)
	}))

	// Middleware chain: auth (outermost) → csrf → router
	var handler http.Handler = mux
	handler = csrfMiddleware(handler)
	handler = s.authMiddleware(handler)

	httpSrv := &http.Server{Addr: addr, Handler: handler}

	// Shut down gracefully when the context is cancelled.
	go func() {
		<-ctx.Done()
		slog.Info("shutting down server")
		httpSrv.Shutdown(context.Background())
	}()

	slog.Info("starting server", "addr", addr)
	return httpSrv.ListenAndServe()
}

// HandleDashboard renders the main dashboard page.
func (s *Server) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := s.fetchDashboardData(r.Context())
	if err != nil {
		slog.Error("fetch dashboard data", "error", err)
		http.Error(w, "Failed to load dashboard", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderTemplateCtx(r.Context(), w, "dashboard.html", data); err != nil {
		slog.Warn("render dashboard", "error", err)
	}
}

// HandleRefresh busts the cache and redirects back to the dashboard.
func (s *Server) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	if err := s.Cache.Flush(r.Context()); err != nil {
		slog.Warn("flush cache", "error", err)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// newFreshdeskClient builds a Freshdesk client from current config.
func newFreshdeskClient(cfg *config.Config) *freshdesk.Client {
	return freshdesk.NewClient(freshdesk.Config{
		BaseURL: cfg.FreshdeskURL,
		APIKey:  cfg.FreshdeskKey,
	})
}

// computeStaticHash produces a short hash from the contents of key static files.
func computeStaticHash(dir string) string {
	h := sha256.New()
	for _, name := range []string{"style.css", "script.js"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			h.Write(data)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:10]
}

func (s *Server) templateFuncMap(ctx context.Context) template.FuncMap {
	return template.FuncMap{
		"staticHash":   func() string { return s.staticHash },
		"freshdeskURL": func() string { return s.Config.FreshdeskURL },
		"formatSize":   formatSize,
		"hasPrefix":    strings.HasPrefix,
		"csrfToken":    func() string { return csrfTokenFromContext(ctx) },
	}
}

// formatSize returns a human-readable file size string.
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func (s *Server) renderTemplate(w http.ResponseWriter, name string, data any) error {
	return s.renderTemplateCtx(context.Background(), w, name, data)
}

func (s *Server) renderTemplateCtx(ctx context.Context, w http.ResponseWriter, name string, data any) error {
	layoutPath := filepath.Join(s.TemplatesDir, "layout.html")
	pagePath := filepath.Join(s.TemplatesDir, name)
	// Include all partial templates (files starting with _)
	partials, _ := filepath.Glob(filepath.Join(s.TemplatesDir, "_*.html"))
	files := append([]string{layoutPath, pagePath}, partials...)
	tmpl, err := template.New("").Funcs(s.templateFuncMap(ctx)).ParseFiles(files...)
	if err != nil {
		return fmt.Errorf("parse template %q: %w", name, err)
	}
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		return fmt.Errorf("execute template %q: %w", name, err)
	}
	return nil
}

// renderPartial renders a named template block (e.g. "ticket-card") without layout.
func (s *Server) renderPartial(w http.ResponseWriter, name string, data any) error {
	return s.renderPartialCtx(context.Background(), w, name, data)
}

func (s *Server) renderPartialCtx(ctx context.Context, w http.ResponseWriter, name string, data any) error {
	partials, _ := filepath.Glob(filepath.Join(s.TemplatesDir, "_*.html"))
	if len(partials) == 0 {
		return fmt.Errorf("no partial templates found")
	}
	tmpl, err := template.New("").Funcs(s.templateFuncMap(ctx)).ParseFiles(partials...)
	if err != nil {
		return fmt.Errorf("parse partials: %w", err)
	}
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		return fmt.Errorf("execute partial %q: %w", name, err)
	}
	return nil
}
