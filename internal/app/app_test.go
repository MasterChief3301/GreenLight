package app

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/eneat/greenlight/internal/config"
	"github.com/eneat/greenlight/internal/models"
	"github.com/eneat/greenlight/internal/store"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{
		PublicURL:             "http://localhost:8080",
		DefaultAction:         "reject",
		DefaultTimeoutSeconds: 3600,
		CallbackTimeout:       1e9,
		CallbackMaxRetries:    0,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, cfg, log)
}

func TestCreateRequestValidation(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	cases := []struct {
		name string
		in   CreateInput
		ok   bool
	}{
		{"missing title", CreateInput{ResumeURL: "http://x"}, false},
		{"missing resume_url", CreateInput{Title: "x"}, false},
		{"bad priority", CreateInput{Title: "x", ResumeURL: "http://x", Priority: "urgent"}, false},
		{"bad metadata json", CreateInput{Title: "x", ResumeURL: "http://x", Metadata: []byte("{bad")}, false},
		{"valid", CreateInput{Title: "x", ResumeURL: "http://x"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := a.CreateRequest(ctx, c.in)
			if c.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !c.ok && err == nil {
				t.Errorf("expected validation error, got nil")
			}
		})
	}
}

func TestCreateRequestDefaultsFromConfig(t *testing.T) {
	a := newTestApp(t)
	r, err := a.CreateRequest(context.Background(), CreateInput{Title: "x", ResumeURL: "http://x"})
	if err != nil {
		t.Fatal(err)
	}
	if r.DefaultAction != models.ActionReject {
		t.Errorf("default action = %v, want reject (config fallback)", r.DefaultAction)
	}
	if r.TimeoutSeconds != 3600 {
		t.Errorf("timeout = %d, want 3600 (config fallback)", r.TimeoutSeconds)
	}
	if r.Status != models.StatusPending {
		t.Errorf("status = %v, want pending", r.Status)
	}
}

func TestCreateRequestDefaultsFromRule(t *testing.T) {
	a := newTestApp(t)
	if err := a.Store.CreateRule(&models.DefaultRule{
		Source: "backups", DefaultAction: models.ActionApprove, TimeoutSeconds: 120,
	}); err != nil {
		t.Fatal(err)
	}
	r, err := a.CreateRequest(context.Background(), CreateInput{
		Title: "x", Source: "backups", ResumeURL: "http://x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.DefaultAction != models.ActionApprove || r.TimeoutSeconds != 120 {
		t.Errorf("got %v/%d, want approve/120 from rule", r.DefaultAction, r.TimeoutSeconds)
	}
}

func TestCreateRequestExplicitOverridesRule(t *testing.T) {
	a := newTestApp(t)
	if err := a.Store.CreateRule(&models.DefaultRule{
		Source: "backups", DefaultAction: models.ActionApprove, TimeoutSeconds: 120,
	}); err != nil {
		t.Fatal(err)
	}
	r, err := a.CreateRequest(context.Background(), CreateInput{
		Title: "x", Source: "backups", ResumeURL: "http://x",
		DefaultAction: "reject", TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.DefaultAction != models.ActionReject || r.TimeoutSeconds != 30 {
		t.Errorf("got %v/%d, want explicit reject/30", r.DefaultAction, r.TimeoutSeconds)
	}
}

func TestExpireAppliesDefault(t *testing.T) {
	a := newTestApp(t)
	r, err := a.CreateRequest(context.Background(), CreateInput{
		Title: "x", ResumeURL: "http://127.0.0.1:1/unreachable",
		DefaultAction: "approve", TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := a.Expire(r)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if resolved.Status != models.StatusExpiredApproved {
		t.Errorf("status = %v, want expired-approved", resolved.Status)
	}
	if resolved.DecidedBy != models.DecidedByTimeout {
		t.Errorf("decided_by = %v, want timeout", resolved.DecidedBy)
	}
	a.Wait() // let the background callback attempt finish cleanly
}

func TestActionTerminalStatus(t *testing.T) {
	cases := []struct {
		a       models.Action
		expired bool
		want    models.Status
	}{
		{models.ActionApprove, false, models.StatusApproved},
		{models.ActionApprove, true, models.StatusExpiredApproved},
		{models.ActionReject, false, models.StatusRejected},
		{models.ActionReject, true, models.StatusExpiredRejected},
	}
	for _, c := range cases {
		if got := c.a.TerminalStatus(c.expired); got != c.want {
			t.Errorf("%v.TerminalStatus(%v) = %v, want %v", c.a, c.expired, got, c.want)
		}
	}
}
