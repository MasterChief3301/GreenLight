// Package app holds Greenlight's business logic, coordinating the store, ntfy
// notifications, and resume-URL callbacks. It is shared by the HTTP handlers
// and the background timeout scheduler.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/eneat/greenlight/internal/config"
	"github.com/eneat/greenlight/internal/models"
	"github.com/eneat/greenlight/internal/ntfy"
	"github.com/eneat/greenlight/internal/resume"
	"github.com/eneat/greenlight/internal/store"
)

// App bundles the dependencies used across handlers and the scheduler.
type App struct {
	Store  *store.Store
	Ntfy   *ntfy.Client // may be nil when notifications are disabled
	Resume *resume.Client
	Cfg    *config.Config
	Log    *slog.Logger

	// wg tracks in-flight background callback deliveries for graceful shutdown.
	wg sync.WaitGroup
}

// New constructs an App.
func New(st *store.Store, cfg *config.Config, log *slog.Logger) *App {
	nt := ntfy.New(ntfy.Config{
		BaseURL: cfg.NtfyBaseURL,
		Topic:   cfg.NtfyTopic,
		Token:   cfg.NtfyToken,
		User:    cfg.NtfyUser,
		Pass:    cfg.NtfyPass,
	})
	return &App{
		Store:  st,
		Ntfy:   nt,
		Resume: resume.New(cfg.CallbackTimeout, cfg.CallbackMaxRetries, cfg.ResumeMethod),
		Cfg:    cfg,
		Log:    log,
	}
}

// Wait blocks until all in-flight background callbacks finish. Used on shutdown.
func (a *App) Wait() { a.wg.Wait() }

// CreateInput describes a new approval request from the API.
type CreateInput struct {
	Title              string
	Description        string
	Source             string
	Category           string
	Priority           string
	DefaultAction      string // optional; resolved from rules when empty
	TimeoutSeconds     int    // optional; resolved from rules when zero
	ResumeURL          string
	ResumePayloadExtra json.RawMessage
	Metadata           json.RawMessage
}

// ValidationError signals a bad request from the caller.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func verr(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// CreateRequest validates and stores a new request, resolving defaults from
// rules, then publishes a notification (best-effort).
func (a *App) CreateRequest(ctx context.Context, in CreateInput) (*models.Request, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return nil, verr("title is required")
	}
	if strings.TrimSpace(in.ResumeURL) == "" {
		return nil, verr("resume_url is required")
	}

	priority := models.Priority(in.Priority)
	if in.Priority == "" {
		priority = models.PriorityNormal
	}
	if !priority.Valid() {
		return nil, verr("invalid priority %q (want low|normal|high)", in.Priority)
	}

	// Resolve default action + timeout: explicit values win, else a matching
	// rule, else the global config fallback.
	action := models.Action(in.DefaultAction)
	timeout := in.TimeoutSeconds
	if action == "" || timeout <= 0 {
		rule, err := a.Store.ResolveRule(in.Source, in.Category)
		if err != nil {
			return nil, fmt.Errorf("resolve rule: %w", err)
		}
		if rule != nil {
			if action == "" {
				action = rule.DefaultAction
			}
			if timeout <= 0 {
				timeout = rule.TimeoutSeconds
			}
		}
		if action == "" {
			action = models.Action(a.Cfg.DefaultAction)
		}
		if timeout <= 0 {
			timeout = a.Cfg.DefaultTimeoutSeconds
		}
	}
	if !action.Valid() {
		return nil, verr("invalid default_action %q (want approve|reject)", in.DefaultAction)
	}

	if err := validJSON(in.ResumePayloadExtra); err != nil {
		return nil, verr("resume_payload_extra is not valid JSON: %v", err)
	}
	if err := validJSON(in.Metadata); err != nil {
		return nil, verr("metadata is not valid JSON: %v", err)
	}

	r := &models.Request{
		ID:                 newID(),
		Title:              in.Title,
		Description:        in.Description,
		Source:             in.Source,
		Category:           in.Category,
		Priority:           priority,
		Status:             models.StatusPending,
		DefaultAction:      action,
		TimeoutSeconds:     timeout,
		ResumeURL:          in.ResumeURL,
		ResumePayloadExtra: string(in.ResumePayloadExtra),
		Metadata:           string(in.Metadata),
		CreatedAt:          time.Now().UTC(),
	}
	if err := a.Store.CreateRequest(r); err != nil {
		return nil, err
	}
	a.Log.Info("request created", "id", r.ID, "title", r.Title, "source", r.Source,
		"default_action", r.DefaultAction, "timeout_seconds", r.TimeoutSeconds)

	a.notifyCreated(ctx, r)
	return r, nil
}

// notifyCreated publishes the "new request" notification, logging on failure.
func (a *App) notifyCreated(ctx context.Context, r *models.Request) {
	if a.Ntfy == nil {
		return
	}
	body := r.Description
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	if body == "" {
		body = fmt.Sprintf("From %s — default action on timeout: %s", orDash(r.Source), r.DefaultAction)
	}
	msg := ntfy.Message{
		Title:    "Approval needed: " + r.Title,
		Body:     body,
		Priority: r.Priority,
		ClickURL: a.RequestURL(r.ID),
		Tags:     []string{"vertical_traffic_light"},
	}
	if err := a.Ntfy.Publish(ctx, msg); err != nil {
		a.Log.Warn("ntfy publish failed", "id", r.ID, "err", err)
	}
}

// RequestURL builds the public URL for a request's detail page.
func (a *App) RequestURL(id string) string {
	return a.Cfg.PublicURL + "/requests/" + id
}

// Decide resolves a pending request to a user decision and delivers the callback
// in the background. Returns ErrAlreadyResolved if the request was not pending.
func (a *App) Decide(id string, action models.Action, comment string) (*models.Request, error) {
	if !action.Valid() {
		return nil, verr("invalid action %q", action)
	}
	status := action.TerminalStatus(false)
	r, err := a.Store.Resolve(id, status, models.DecidedByUser, comment, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	a.Log.Info("request decided", "id", r.ID, "status", r.Status, "by", "user")
	a.deliverAsync(r)
	return r, nil
}

// Cancel withdraws a pending request (caller-initiated). No callback is sent.
func (a *App) Cancel(id string) (*models.Request, error) {
	r, err := a.Store.Resolve(id, models.StatusCancelled, models.DecidedByAPI, "", time.Now().UTC())
	if err != nil {
		return nil, err
	}
	a.Log.Info("request cancelled", "id", r.ID)
	return r, nil
}

// Expire applies the default action to an overdue request, race-safe against a
// concurrent user decision. Returns ErrAlreadyResolved if no longer pending.
func (a *App) Expire(r *models.Request) (*models.Request, error) {
	status := r.DefaultAction.TerminalStatus(true)
	resolved, err := a.Store.Resolve(r.ID, status, models.DecidedByTimeout, "", time.Now().UTC())
	if err != nil {
		return nil, err
	}
	a.Log.Info("request expired (default applied)", "id", resolved.ID, "status", resolved.Status)
	if a.Cfg.NotifyOnTimeout && a.Ntfy != nil {
		verb := "approved"
		if !resolved.Status.IsApproval() {
			verb = "rejected"
		}
		_ = a.Ntfy.Publish(context.Background(), ntfy.Message{
			Title:    "Auto-" + verb + ": " + resolved.Title,
			Body:     "Timeout reached; default action applied.",
			Priority: models.PriorityLow,
			ClickURL: a.RequestURL(resolved.ID),
			Tags:     []string{"hourglass"},
		})
	}
	a.deliverAsync(resolved)
	return resolved, nil
}

// SendReminder publishes a reminder ping for a still-pending request and records
// that it was sent.
func (a *App) SendReminder(r *models.Request) error {
	if a.Ntfy != nil {
		remaining := time.Until(r.DeadlineTime()).Round(time.Second)
		if err := a.Ntfy.Publish(context.Background(), ntfy.Message{
			Title:    "Still pending: " + r.Title,
			Body:     fmt.Sprintf("Default (%s) fires in %s.", r.DefaultAction, remaining),
			Priority: r.Priority,
			ClickURL: a.RequestURL(r.ID),
			Tags:     []string{"bell"},
		}); err != nil {
			return err
		}
	}
	return a.Store.MarkReminderSent(r.ID, time.Now().UTC())
}

// deliverAsync runs the resume callback in a tracked goroutine and records the
// outcome on the request.
func (a *App) deliverAsync(r *models.Request) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := a.Resume.Deliver(ctx, r)
		if err != nil {
			a.Log.Error("resume callback failed", "id", r.ID, "err", err)
			if serr := a.Store.SetCallbackResult(r.ID, true, err.Error()); serr != nil {
				a.Log.Error("record callback failure", "id", r.ID, "err", serr)
			}
			return
		}
		a.Log.Info("resume callback delivered", "id", r.ID)
		if serr := a.Store.SetCallbackResult(r.ID, false, ""); serr != nil {
			a.Log.Error("record callback success", "id", r.ID, "err", serr)
		}
	}()
}

// TestNotification publishes a test message (used by the Settings page).
func (a *App) TestNotification(ctx context.Context) error {
	if a.Ntfy == nil {
		return fmt.Errorf("ntfy is not configured")
	}
	return a.Ntfy.Publish(ctx, ntfy.Message{
		Title:    "Greenlight test",
		Body:     "If you can read this, notifications work. ✅",
		Priority: models.PriorityNormal,
		ClickURL: a.Cfg.PublicURL,
		Tags:     []string{"white_check_mark"},
	})
}

func validJSON(b json.RawMessage) error {
	if len(b) == 0 {
		return nil
	}
	if !json.Valid(b) {
		return fmt.Errorf("malformed JSON")
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// newID generates a short, URL-safe, unguessable request identifier.
func newID() string {
	b := make([]byte, 9) // 18 hex chars
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is fatal-level rare; fall back to time-based.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
