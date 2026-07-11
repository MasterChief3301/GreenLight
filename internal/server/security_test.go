package server

import (
	"testing"
	"time"
)

func TestSessionRoundTrip(t *testing.T) {
	sec := newSecurity([]byte("0123456789abcdef0123456789abcdef"), time.Hour, false)
	tok := sec.issueSession()
	if !sec.validSession(tok) {
		t.Error("freshly issued session should be valid")
	}
}

func TestSessionTampering(t *testing.T) {
	sec := newSecurity([]byte("0123456789abcdef0123456789abcdef"), time.Hour, false)
	tok := sec.issueSession()
	if sec.validSession(tok + "x") {
		t.Error("tampered session should be invalid")
	}
	if sec.validSession("garbage.deadbeef") {
		t.Error("garbage session should be invalid")
	}
	// Different secret must reject.
	other := newSecurity([]byte("ffffffffffffffffffffffffffffffff"), time.Hour, false)
	if other.validSession(tok) {
		t.Error("session signed with a different secret should be invalid")
	}
}

func TestSessionExpiry(t *testing.T) {
	sec := newSecurity([]byte("0123456789abcdef0123456789abcdef"), -time.Second, false)
	tok := sec.issueSession()
	if sec.validSession(tok) {
		t.Error("expired session should be invalid")
	}
}

func TestLoginRateLimit(t *testing.T) {
	sec := newSecurity([]byte("0123456789abcdef0123456789abcdef"), time.Hour, false)
	ip := "1.2.3.4"
	if !sec.loginAllowed(ip) {
		t.Fatal("should be allowed initially")
	}
	for i := 0; i < maxLoginAttempts; i++ {
		sec.recordLoginFailure(ip)
	}
	if sec.loginAllowed(ip) {
		t.Error("should be locked out after max failures")
	}
	// A different IP is unaffected.
	if !sec.loginAllowed("5.6.7.8") {
		t.Error("different IP should still be allowed")
	}
	// Success clears the counter.
	sec.recordLoginSuccess(ip)
	if !sec.loginAllowed(ip) {
		t.Error("should be allowed after successful login clears counter")
	}
}

func TestVerifyPassword(t *testing.T) {
	if !verifyPassword("secret", "secret") {
		t.Error("matching passwords should verify")
	}
	if verifyPassword("secret", "wrong") {
		t.Error("non-matching passwords should not verify")
	}
	if verifyPassword("secret", "") {
		t.Error("empty submission should not verify")
	}
}

func TestHashAPIKeyStable(t *testing.T) {
	if hashAPIKey("glk_abc") != hashAPIKey("glk_abc") {
		t.Error("hashing should be deterministic")
	}
	if hashAPIKey("a") == hashAPIKey("b") {
		t.Error("different keys should hash differently")
	}
}
