package server

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// buildTemplates parses all templates from the embedded FS with the shared
// FuncMap.
func buildTemplates(fsys fs.FS) (*template.Template, error) {
	return template.New("").Funcs(templateFuncs()).ParseFS(fsys, "templates/*.html")
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"dict":        dict,
		"humanStatus": humanStatus,
		"humanTime":   humanTime,
		"unixMillis":  func(t time.Time) int64 { return t.UnixMilli() },
	}
}

// dict builds a map from alternating key/value pairs, for passing multiple
// values to a sub-template.
func dict(pairs ...interface{}) (map[string]interface{}, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments")
	}
	m := make(map[string]interface{}, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %d is not a string", i)
		}
		m[key] = pairs[i+1]
	}
	return m, nil
}

// humanStatus renders a status value as a friendly label.
func humanStatus(s interface{}) string {
	switch fmt.Sprint(s) {
	case "pending":
		return "Pending"
	case "approved":
		return "Approved"
	case "rejected":
		return "Rejected"
	case "expired-approved":
		return "Auto-approved"
	case "expired-rejected":
		return "Auto-rejected"
	case "cancelled":
		return "Cancelled"
	}
	return fmt.Sprint(s)
}

// humanTime formats a timestamp (or *time.Time) in the server's local zone.
func humanTime(v interface{}) string {
	var t time.Time
	switch x := v.(type) {
	case time.Time:
		t = x
	case *time.Time:
		if x == nil {
			return "—"
		}
		t = *x
	default:
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// viewData is the common template context for every page.
type viewData struct {
	Title         string
	Active        string
	Authenticated bool
	CSRF          string
	Flash         string
	FlashKind     string // "ok" | "err"
	Data          interface{}
}

// render executes a named template to the response.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, vd viewData) {
	vd.Authenticated = s.sec.isAuthenticated(r)
	vd.CSRF = s.sec.ensureCSRF(w, r)
	if vd.Flash == "" {
		if f, kind := s.readFlash(w, r); f != "" {
			vd.Flash, vd.FlashKind = f, kind
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, vd); err != nil {
		s.app.Log.Error("template render", "name", name, "err", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// renderFragment executes a named template as a bare HTML fragment (no layout,
// no flash handling), ensuring the CSRF token is available. Used for htmx swaps.
func (s *Server) renderFragment(w http.ResponseWriter, r *http.Request, name string, vd viewData) {
	vd.CSRF = s.sec.ensureCSRF(w, r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, vd); err != nil {
		s.app.Log.Error("fragment render", "name", name, "err", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func urlQueryEscape(s string) string {
	// minimal escape for the next= redirect param
	r := strings.NewReplacer("&", "%26", "?", "%3F", "#", "%23", " ", "%20")
	return r.Replace(s)
}
