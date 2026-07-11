// Package server wires Greenlight's HTTP API and web UI.
package server

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/MasterChief3301/greenlight/internal/app"
	"github.com/MasterChief3301/greenlight/web"
)

// Server holds the HTTP dependencies.
type Server struct {
	app  *app.App
	sec  *security
	tmpl *template.Template
}

// New builds a Server and parses templates.
func New(a *app.App) (*Server, error) {
	tmpl, err := buildTemplates(web.Templates)
	if err != nil {
		return nil, err
	}
	secure := strings.HasPrefix(a.Cfg.PublicURL, "https://")
	return &Server{
		app:  a,
		sec:  newSecurity(a.Cfg.SessionSecret, a.Cfg.SessionTTL, secure),
		tmpl: tmpl,
	}, nil
}

// Handler returns the root HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Static assets.
	staticFS, _ := fs.Sub(web.Static, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(http.FileServer(http.FS(staticFS)))))

	// Health.
	mux.HandleFunc("GET /healthz", s.handleHealth)

	// --- JSON API (X-API-Key auth) ---
	mux.HandleFunc("POST /api/requests", s.requireAPIKey(s.apiCreateRequest))
	mux.HandleFunc("GET /api/requests", s.requireAPIKey(s.apiListRequests))
	mux.HandleFunc("GET /api/requests/{id}", s.requireAPIKey(s.apiGetRequest))
	mux.HandleFunc("POST /api/requests/{id}/cancel", s.requireAPIKey(s.apiCancelRequest))

	// --- Auth ---
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.requireCSRF(s.handleLogout))

	// --- UI (session auth) ---
	mux.HandleFunc("GET /{$}", s.requireSession(s.handleDashboard))
	mux.HandleFunc("GET /partials/dashboard", s.requireSession(s.handlePartialDashboard))
	mux.HandleFunc("GET /requests/{id}", s.requireSession(s.handleDetail))
	mux.HandleFunc("POST /requests/{id}/decide", s.requireSession(s.requireCSRF(s.handleDecide)))
	mux.HandleFunc("GET /history", s.requireSession(s.handleHistory))
	mux.HandleFunc("GET /settings", s.requireSession(s.handleSettings))
	mux.HandleFunc("POST /settings/test-notification", s.requireSession(s.requireCSRF(s.handleTestNotification)))
	mux.HandleFunc("POST /settings/rules", s.requireSession(s.requireCSRF(s.handleCreateRule)))
	mux.HandleFunc("POST /settings/rules/{id}/delete", s.requireSession(s.requireCSRF(s.handleDeleteRule)))
	mux.HandleFunc("POST /settings/keys", s.requireSession(s.requireCSRF(s.handleCreateKey)))
	mux.HandleFunc("POST /settings/keys/{id}/delete", s.requireSession(s.requireCSRF(s.handleDeleteKey)))

	return s.logRequests(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.DB().Ping(); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "db unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// cacheStatic adds a modest cache header to static assets.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		next.ServeHTTP(w, r)
	})
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
