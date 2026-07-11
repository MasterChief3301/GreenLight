package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eneat/greenlight/internal/models"
	"github.com/eneat/greenlight/internal/store"
)

// --- Auth pages ---

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.sec.isAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	next := r.URL.Query().Get("next")
	s.render(w, r, "login", viewData{Title: "Log in", Data: map[string]interface{}{"Next": next}})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.sec.loginAllowed(ip) {
		s.setFlash(w, "err", "Too many attempts. Try again later.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !s.sec.checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	password := r.FormValue("password")
	if !verifyPassword(s.app.Cfg.AdminPassword, password) {
		s.sec.recordLoginFailure(ip)
		s.app.Log.Warn("failed login", "ip", ip)
		s.setFlash(w, "err", "Incorrect password.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.sec.recordLoginSuccess(ip)
	s.sec.setSessionCookie(w, s.sec.issueSession())

	next := r.FormValue("next")
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sec.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Dashboard ---

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := s.dashboardData()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "dashboard", viewData{
		Title:  "Dashboard",
		Active: "dashboard",
		Data:   data,
	})
}

// handlePartialDashboard renders just the dashboard body (pending + recent),
// used by the periodic htmx poll so the page updates without a full refresh.
func (s *Server) handlePartialDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := s.dashboardData()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.renderFragment(w, r, "dashboard_body", viewData{Data: data})
}

// dashboardData gathers the pending list and the most recent decided requests.
func (s *Server) dashboardData() (map[string]interface{}, error) {
	pending, err := s.app.Store.ListPending()
	if err != nil {
		return nil, err
	}
	// Fetch a generous slice of recent requests, then keep the newest decided
	// ones (so a long pending list doesn't crowd out the "recently decided"
	// section).
	recent, err := s.app.Store.ListRequests(store.RequestFilter{Limit: 30})
	if err != nil {
		return nil, err
	}
	const maxRecent = 8
	var decided []*models.Request
	for _, req := range recent {
		if req.Status != models.StatusPending {
			decided = append(decided, req)
			if len(decided) == maxRecent {
				break
			}
		}
	}
	return map[string]interface{}{"Pending": pending, "Recent": decided}, nil
}

// --- Detail ---

type kv struct{ Key, Value string }

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := s.app.Store.GetRequest(id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "detail", viewData{
		Title:  req.Title,
		Active: "dashboard",
		Data:   map[string]interface{}{"Req": req, "Metadata": parseMetadata(req.Metadata)},
	})
}

// parseMetadata turns the stored JSON object into a sorted key/value list.
func parseMetadata(raw string) []kv {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{Key: k, Value: fmt.Sprint(v)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// --- Decide ---

func (s *Server) handleDecide(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	action := models.Action(r.FormValue("action"))
	comment := strings.TrimSpace(r.FormValue("comment"))

	req, err := s.app.Decide(id, action, comment)
	isHTMX := r.Header.Get("HX-Request") == "true"

	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, store.ErrAlreadyResolved) {
		// Someone/something already decided; refresh the dashboard to current state.
		if isHTMX {
			s.handlePartialDashboard(w, r)
			return
		}
		s.setFlash(w, "err", "That request was already resolved.")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// From the dashboard (htmx), return the freshly rendered body so the decided
	// item immediately moves to "Recently decided".
	if isHTMX {
		s.handlePartialDashboard(w, r)
		return
	}
	verb := "approved"
	if !req.Status.IsApproval() {
		verb = "rejected"
	}
	s.setFlash(w, "ok", fmt.Sprintf("Request %s.", verb))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- History ---

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := store.RequestFilter{
		Status:   q.Get("status"),
		Source:   q.Get("source"),
		Category: q.Get("category"),
		Limit:    500,
	}
	sinceStr := q.Get("since")
	untilStr := q.Get("until")
	if t, ok := parseDate(sinceStr, false); ok {
		filter.Since = &t
	}
	if t, ok := parseDate(untilStr, true); ok {
		filter.Until = &t
	}

	reqs, err := s.app.Store.ListRequests(filter)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	sources, _ := s.app.Store.DistinctSources()
	categories, _ := s.app.Store.DistinctCategories()

	s.render(w, r, "history", viewData{
		Title:  "History",
		Active: "history",
		Data: map[string]interface{}{
			"Requests":   reqs,
			"Filter":     filter,
			"Sources":    sources,
			"Categories": categories,
			"Statuses":   allStatuses(),
			"SinceStr":   sinceStr,
			"UntilStr":   untilStr,
		},
	})
}

func parseDate(s string, endOfDay bool) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Second)
	}
	return t.UTC(), true
}

func allStatuses() []string {
	return []string{"pending", "approved", "rejected", "expired-approved", "expired-rejected", "cancelled"}
}

// --- Settings ---

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	rules, err := s.app.Store.ListRules()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	keys, err := s.app.Store.ListAPIKeys()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	// A freshly-generated key is passed once via a short-lived cookie.
	newKey := ""
	if c, err := r.Cookie("gl_newkey"); err == nil {
		newKey = c.Value
		http.SetCookie(w, &http.Cookie{Name: "gl_newkey", Value: "", Path: "/", MaxAge: -1})
	}

	s.render(w, r, "settings", viewData{
		Title:  "Settings",
		Active: "settings",
		Data: map[string]interface{}{
			"Rules":          rules,
			"Keys":           keys,
			"NewKey":         newKey,
			"NtfyConfigured": s.app.Cfg.NtfyConfigured(),
			"NtfyBaseURL":    s.app.Cfg.NtfyBaseURL,
			"NtfyTopic":      s.app.Cfg.NtfyTopic,
			"GlobalAction":   s.app.Cfg.DefaultAction,
			"GlobalTimeout":  s.app.Cfg.DefaultTimeoutSeconds,
			"Retention":      retentionLabel(s.app.Cfg.HistoryRetention),
		},
	})
}

// retentionLabel describes the history-retention setting for the Settings page.
func retentionLabel(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d%(24*time.Hour) == 0 {
		days := int(d / (24 * time.Hour))
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	return d.String()
}

func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	if err := s.app.TestNotification(r.Context()); err != nil {
		s.setFlash(w, "err", "Test failed: "+err.Error())
	} else {
		s.setFlash(w, "ok", "Test notification sent.")
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	action := models.Action(r.FormValue("default_action"))
	if !action.Valid() {
		s.setFlash(w, "err", "Invalid action.")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	timeout, _ := strconv.Atoi(r.FormValue("timeout_seconds"))
	if timeout <= 0 {
		s.setFlash(w, "err", "Timeout must be a positive number of seconds.")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	rule := &models.DefaultRule{
		Source:         strings.TrimSpace(r.FormValue("source")),
		Category:       strings.TrimSpace(r.FormValue("category")),
		DefaultAction:  action,
		TimeoutSeconds: timeout,
	}
	if err := s.app.Store.CreateRule(rule); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			s.setFlash(w, "err", "A rule for that source/category already exists.")
		} else {
			s.setFlash(w, "err", "Could not create rule.")
		}
	} else {
		s.setFlash(w, "ok", "Rule added.")
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.app.Store.DeleteRule(id); err != nil {
		s.setFlash(w, "err", "Rule not found.")
	} else {
		s.setFlash(w, "ok", "Rule deleted.")
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		s.setFlash(w, "err", "Label is required.")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	plaintext := generateAPIKey()
	if _, err := s.app.Store.CreateAPIKey(label, hashAPIKey(plaintext)); err != nil {
		s.setFlash(w, "err", "Could not create key.")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	// Show the plaintext exactly once via a short-lived cookie.
	http.SetCookie(w, &http.Cookie{
		Name: "gl_newkey", Value: plaintext, Path: "/", MaxAge: 30,
		HttpOnly: true, Secure: s.sec.secure, SameSite: http.SameSiteLaxMode,
	})
	s.setFlash(w, "ok", "API key created.")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.app.Store.DeleteAPIKey(id); err != nil {
		s.setFlash(w, "err", "Key not found.")
	} else {
		s.setFlash(w, "ok", "API key revoked.")
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// serverError logs and renders a 500.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.app.Log.Error("handler error", "path", r.URL.Path, "err", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
