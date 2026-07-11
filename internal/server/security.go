package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "gl_session"
	csrfCookie    = "gl_csrf"
	csrfField     = "csrf_token"
)

// security handles cookie signing, session issuing/verification, CSRF tokens,
// and login rate limiting.
type security struct {
	secret     []byte
	sessionTTL time.Duration
	secure     bool // set Secure flag on cookies (true when PublicURL is https)

	mu       sync.Mutex
	attempts map[string]*attemptRecord // login rate limiting keyed by client IP
}

type attemptRecord struct {
	count       int
	lockedUntil time.Time
	windowStart time.Time
}

func newSecurity(secret []byte, ttl time.Duration, secure bool) *security {
	return &security{
		secret:     secret,
		sessionTTL: ttl,
		secure:     secure,
		attempts:   make(map[string]*attemptRecord),
	}
}

// sign returns hex(HMAC-SHA256(msg)).
func (s *security) sign(msg string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// issueSession returns a signed session token valid for the TTL.
func (s *security) issueSession() string {
	payload := "auth:" + strconv.FormatInt(time.Now().Unix(), 10)
	enc := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return enc + "." + s.sign(enc)
}

// validSession reports whether a session token is authentic and unexpired.
func (s *security) validSession(token string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	enc, sig := parts[0], parts[1]
	expected := s.sign(enc)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return false
	}
	payload := string(raw)
	if !strings.HasPrefix(payload, "auth:") {
		return false
	}
	issued, err := strconv.ParseInt(strings.TrimPrefix(payload, "auth:"), 10, 64)
	if err != nil {
		return false
	}
	return time.Since(time.Unix(issued, 0)) < s.sessionTTL
}

func (s *security) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.sessionTTL.Seconds()),
	})
}

func (s *security) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (s *security) isAuthenticated(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	return s.validSession(c.Value)
}

// ensureCSRF returns the CSRF token for the request, setting a fresh cookie if
// one is not already present.
func (s *security) ensureCSRF(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookie); err == nil && len(c.Value) >= 32 {
		return c.Value
	}
	token := randomToken(32)
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.sessionTTL.Seconds()),
	})
	return token
}

// checkCSRF validates the submitted CSRF token against the cookie.
func (s *security) checkCSRF(r *http.Request) bool {
	c, err := r.Cookie(csrfCookie)
	if err != nil || c.Value == "" {
		return false
	}
	submitted := r.FormValue(csrfField)
	if submitted == "" {
		submitted = r.Header.Get("X-CSRF-Token")
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(submitted)) == 1
}

// --- login rate limiting ---

const (
	maxLoginAttempts = 5
	loginWindow      = 5 * time.Minute
	lockoutDuration  = 15 * time.Minute
)

// loginAllowed reports whether the client may attempt a login now.
func (s *security) loginAllowed(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.attempts[ip]
	if rec == nil {
		return true
	}
	if time.Now().Before(rec.lockedUntil) {
		return false
	}
	return true
}

// recordLoginFailure increments the failure counter, locking out after too many.
func (s *security) recordLoginFailure(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	rec := s.attempts[ip]
	if rec == nil || now.Sub(rec.windowStart) > loginWindow {
		rec = &attemptRecord{windowStart: now}
		s.attempts[ip] = rec
	}
	rec.count++
	if rec.count >= maxLoginAttempts {
		rec.lockedUntil = now.Add(lockoutDuration)
	}
}

// recordLoginSuccess clears the failure counter for a client.
func (s *security) recordLoginSuccess(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, ip)
}

// verifyPassword compares a submitted password to the configured one in
// constant time.
func verifyPassword(configured, submitted string) bool {
	return subtle.ConstantTimeCompare([]byte(configured), []byte(submitted)) == 1
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// clientIP extracts a best-effort client IP for rate limiting.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		return host[:i]
	}
	return host
}
