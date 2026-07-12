// Package config loads Greenlight configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration.
type Config struct {
	// HTTP
	Addr         string // listen address, e.g. ":8080"
	PublicURL    string // externally reachable base URL, used in notification links
	CookieSecure bool   // set Secure flag on session/CSRF cookies (only sent over HTTPS)

	// Storage
	DBPath string

	// Auth
	AdminPassword string // plaintext admin password (single user)
	SessionSecret []byte // HMAC key for signing session + CSRF cookies
	SessionTTL    time.Duration

	// ntfy
	NtfyBaseURL string // e.g. https://ntfy.sh
	NtfyTopic   string
	NtfyToken   string // bearer token (optional)
	NtfyUser    string // basic-auth user (optional, alternative to token)
	NtfyPass    string // basic-auth pass

	// Defaults / engine
	DefaultTimeoutSeconds int
	DefaultAction         string        // "approve" | "reject"
	SchedulerInterval     time.Duration // how often to scan for overdue requests
	ReminderEnabled       bool          // send a reminder ping while still pending
	ReminderFraction      float64       // fraction of timeout elapsed before reminding (e.g. 0.5)
	NotifyOnTimeout       bool          // publish a notification when a timeout default fires

	// Callback behaviour
	CallbackMaxRetries int
	CallbackTimeout    time.Duration
	// ResumeMethod is the HTTP method used to call the n8n resume URL. It must
	// match the Wait node's configured webhook method (n8n's default is GET).
	ResumeMethod string

	// HistoryRetention, when > 0, auto-deletes decided requests older than this
	// age. 0 disables purging (history is kept forever).
	HistoryRetention time.Duration
}

// Load reads configuration from the environment, applying defaults and
// validating required fields.
func Load() (*Config, error) {
	c := &Config{
		Addr:                  getEnv("GREENLIGHT_ADDR", ":8080"),
		PublicURL:             strings.TrimRight(getEnv("GREENLIGHT_PUBLIC_URL", "http://localhost:8080"), "/"),
		DBPath:                getEnv("GREENLIGHT_DB_PATH", "greenlight.db"),
		AdminPassword:         os.Getenv("GREENLIGHT_ADMIN_PASSWORD"),
		NtfyBaseURL:           strings.TrimRight(getEnv("GREENLIGHT_NTFY_BASE_URL", ""), "/"),
		NtfyTopic:             os.Getenv("GREENLIGHT_NTFY_TOPIC"),
		NtfyToken:             os.Getenv("GREENLIGHT_NTFY_TOKEN"),
		NtfyUser:              os.Getenv("GREENLIGHT_NTFY_USER"),
		NtfyPass:              os.Getenv("GREENLIGHT_NTFY_PASS"),
		DefaultTimeoutSeconds: getEnvInt("GREENLIGHT_DEFAULT_TIMEOUT_SECONDS", 3600),
		DefaultAction:         getEnv("GREENLIGHT_DEFAULT_ACTION", "reject"),
		SchedulerInterval:     getEnvDuration("GREENLIGHT_SCHEDULER_INTERVAL", 15*time.Second),
		SessionTTL:            getEnvDuration("GREENLIGHT_SESSION_TTL", 168*time.Hour),
		ReminderEnabled:       getEnvBool("GREENLIGHT_REMINDER_ENABLED", true),
		ReminderFraction:      getEnvFloat("GREENLIGHT_REMINDER_FRACTION", 0.5),
		NotifyOnTimeout:       getEnvBool("GREENLIGHT_NOTIFY_ON_TIMEOUT", true),
		CallbackMaxRetries:    getEnvInt("GREENLIGHT_CALLBACK_MAX_RETRIES", 4),
		CallbackTimeout:       getEnvDuration("GREENLIGHT_CALLBACK_TIMEOUT", 15*time.Second),
		ResumeMethod:          strings.ToUpper(getEnv("GREENLIGHT_RESUME_METHOD", "POST")),
		HistoryRetention:      getEnvDuration("GREENLIGHT_HISTORY_RETENTION", 0),
	}

	// Default to Secure cookies when the public URL is HTTPS, but allow an explicit
	// override so the app can be reached directly over plain http://<ip>:PORT on a
	// trusted LAN (Secure cookies are dropped by the browser over HTTP).
	c.CookieSecure = getEnvBool("GREENLIGHT_COOKIE_SECURE", strings.HasPrefix(c.PublicURL, "https://"))

	if secret := os.Getenv("GREENLIGHT_SESSION_SECRET"); secret != "" {
		c.SessionSecret = []byte(secret)
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	if c.AdminPassword == "" {
		return fmt.Errorf("GREENLIGHT_ADMIN_PASSWORD is required")
	}
	if len(c.SessionSecret) < 16 {
		return fmt.Errorf("GREENLIGHT_SESSION_SECRET is required and must be at least 16 bytes")
	}
	if c.DefaultAction != "approve" && c.DefaultAction != "reject" {
		return fmt.Errorf("GREENLIGHT_DEFAULT_ACTION must be 'approve' or 'reject', got %q", c.DefaultAction)
	}
	if c.DefaultTimeoutSeconds <= 0 {
		return fmt.Errorf("GREENLIGHT_DEFAULT_TIMEOUT_SECONDS must be positive")
	}
	if c.ReminderFraction <= 0 || c.ReminderFraction >= 1 {
		return fmt.Errorf("GREENLIGHT_REMINDER_FRACTION must be between 0 and 1 (exclusive)")
	}
	switch c.ResumeMethod {
	case "GET", "POST", "PUT", "PATCH":
	default:
		return fmt.Errorf("GREENLIGHT_RESUME_METHOD must be one of GET, POST, PUT, PATCH, got %q", c.ResumeMethod)
	}
	return nil
}

// NtfyConfigured reports whether ntfy publishing is set up.
func (c *Config) NtfyConfigured() bool {
	return c.NtfyBaseURL != "" && c.NtfyTopic != ""
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
