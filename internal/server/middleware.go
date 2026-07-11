package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/MasterChief3301/greenlight/internal/store"
)

// requireAPIKey authenticates /api/* callers via the X-API-Key header.
func (s *Server) requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing X-API-Key header")
			return
		}
		k, err := s.app.Store.LookupAPIKey(hashAPIKey(key))
		if errors.Is(err, store.ErrNotFound) {
			writeJSONError(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		if err != nil {
			s.app.Log.Error("api key lookup", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		// Best-effort last-used tracking; don't fail the request on error.
		go func() { _ = s.app.Store.TouchAPIKey(k.ID) }()
		next(w, r)
	}
}

// requireSession gates UI routes behind the admin login, redirecting to /login
// with a return-to parameter when unauthenticated.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.sec.isAuthenticated(r) {
			redirect := "/login?next=" + urlQueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, redirect, http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// requireCSRF wraps a mutating UI handler, rejecting requests with a bad token.
func (s *Server) requireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.sec.checkCSRF(r) {
			http.Error(w, "invalid or missing CSRF token", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// logRequests logs each HTTP request with method, path, status, and duration.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		s.app.Log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"dur", time.Since(start).Round(time.Millisecond).String(),
			"ip", clientIP(r),
		)
	})
}

// statusRecorder captures the response status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
