package server

import (
	"encoding/base64"
	"net/http"
	"strings"
)

const flashCookie = "gl_flash"

// setFlash stores a one-shot flash message (kind is "ok" or "err") to be shown
// on the next rendered page. Used with the POST-redirect-GET pattern.
func (s *Server) setFlash(w http.ResponseWriter, kind, msg string) {
	val := base64.RawURLEncoding.EncodeToString([]byte(kind + "|" + msg))
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.sec.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60,
	})
}

// readFlash returns and clears any pending flash message.
func (s *Server) readFlash(w http.ResponseWriter, r *http.Request) (msg, kind string) {
	c, err := r.Cookie(flashCookie)
	if err != nil || c.Value == "" {
		return "", ""
	}
	// Clear it.
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.sec.secure, SameSite: http.SameSiteLaxMode,
	})
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[1], parts[0]
}
