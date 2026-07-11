package store

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/MasterChief3301/greenlight/internal/models"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func newPending(t *testing.T, st *Store, timeout int) *models.Request {
	t.Helper()
	r := &models.Request{
		ID:             "req-" + t.Name() + time.Now().Format("150405.000000"),
		Title:          "test",
		Priority:       models.PriorityNormal,
		Status:         models.StatusPending,
		DefaultAction:  models.ActionReject,
		TimeoutSeconds: timeout,
		ResumeURL:      "http://example/resume",
		CreatedAt:      time.Now().UTC(),
	}
	if err := st.CreateRequest(r); err != nil {
		t.Fatalf("create: %v", err)
	}
	return r
}

func TestResolveTransitions(t *testing.T) {
	st := newTestStore(t)
	r := newPending(t, st, 300)

	got, err := st.Resolve(r.ID, models.StatusApproved, models.DecidedByUser, "ok", time.Now().UTC())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Status != models.StatusApproved {
		t.Errorf("status = %v, want approved", got.Status)
	}
	if got.DecidedBy != models.DecidedByUser {
		t.Errorf("decided_by = %v, want user", got.DecidedBy)
	}
	if got.DecidedAt == nil {
		t.Error("decided_at not set")
	}
}

func TestResolveOnlyOnce(t *testing.T) {
	st := newTestStore(t)
	r := newPending(t, st, 300)

	if _, err := st.Resolve(r.ID, models.StatusApproved, models.DecidedByUser, "", time.Now().UTC()); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	// Second resolve (e.g. timeout racing) must fail with ErrAlreadyResolved.
	_, err := st.Resolve(r.ID, models.StatusExpiredRejected, models.DecidedByTimeout, "", time.Now().UTC())
	if err != ErrAlreadyResolved {
		t.Errorf("second resolve err = %v, want ErrAlreadyResolved", err)
	}
}

// TestResolveConcurrent hammers a single request with many concurrent resolvers
// (simulating a user click racing the timeout engine) and asserts exactly one
// succeeds.
func TestResolveConcurrent(t *testing.T) {
	st := newTestStore(t)
	r := newPending(t, st, 300)

	const n = 20
	var wg sync.WaitGroup
	successes := make(chan bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := st.Resolve(r.ID, models.StatusApproved, models.DecidedByUser, "", time.Now().UTC())
			if err == nil {
				successes <- true
			}
		}()
	}
	wg.Wait()
	close(successes)
	count := 0
	for range successes {
		count++
	}
	if count != 1 {
		t.Errorf("exactly-once resolution failed: %d resolvers succeeded, want 1", count)
	}
}

func TestResolveNotFound(t *testing.T) {
	st := newTestStore(t)
	_, err := st.Resolve("nope", models.StatusApproved, models.DecidedByUser, "", time.Now().UTC())
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListOverdue(t *testing.T) {
	st := newTestStore(t)
	// Overdue: created 10s ago, 5s timeout.
	old := &models.Request{
		ID: "overdue", Title: "old", Priority: models.PriorityNormal, Status: models.StatusPending,
		DefaultAction: models.ActionApprove, TimeoutSeconds: 5, ResumeURL: "x",
		CreatedAt: time.Now().Add(-10 * time.Second).UTC(),
	}
	if err := st.CreateRequest(old); err != nil {
		t.Fatal(err)
	}
	// Not overdue: created now, 300s timeout.
	newPending(t, st, 300)

	overdue, err := st.ListOverdue(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(overdue) != 1 || overdue[0].ID != "overdue" {
		t.Errorf("overdue = %+v, want just [overdue]", overdue)
	}
}

func TestRulePrecedence(t *testing.T) {
	st := newTestStore(t)
	mk := func(src, cat string, action models.Action, timeout int) {
		if err := st.CreateRule(&models.DefaultRule{Source: src, Category: cat, DefaultAction: action, TimeoutSeconds: timeout}); err != nil {
			t.Fatal(err)
		}
	}
	mk("", "", models.ActionReject, 100)                // global
	mk("backups", "", models.ActionApprove, 200)        // source-only
	mk("backups", "cleanup", models.ActionApprove, 300) // exact

	cases := []struct {
		src, cat string
		wantTO   int
	}{
		{"backups", "cleanup", 300}, // exact wins
		{"backups", "other", 200},   // source-only
		{"deploys", "x", 100},       // global fallback
		{"", "", 100},               // global
	}
	for _, c := range cases {
		rule, err := st.ResolveRule(c.src, c.cat)
		if err != nil {
			t.Fatalf("resolve %s/%s: %v", c.src, c.cat, err)
		}
		if rule == nil || rule.TimeoutSeconds != c.wantTO {
			t.Errorf("resolve %s/%s -> %+v, want timeout %d", c.src, c.cat, rule, c.wantTO)
		}
	}
}

func TestDeleteDecidedBefore(t *testing.T) {
	st := newTestStore(t)
	now := time.Now().UTC()
	old := now.Add(-10 * 24 * time.Hour)   // 10 days ago
	fresh := now.Add(-1 * time.Hour)       // 1 hour ago
	cutoff := now.Add(-7 * 24 * time.Hour) // keep last 7 days

	// mk creates a pending request at created time, then optionally resolves it
	// to a terminal status at the given decided time (setting decided_at).
	mk := func(id string, status models.Status, created, decided time.Time, resolve bool) {
		r := &models.Request{
			ID: id, Title: id, Priority: models.PriorityNormal, Status: models.StatusPending,
			DefaultAction: models.ActionReject, TimeoutSeconds: 60, ResumeURL: "x", CreatedAt: created,
		}
		if err := st.CreateRequest(r); err != nil {
			t.Fatal(err)
		}
		if resolve {
			if _, err := st.Resolve(id, status, models.DecidedByUser, "", decided); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("old-approved", models.StatusApproved, old, old, true)        // should be deleted
	mk("old-expired", models.StatusExpiredRejected, old, old, true)  // should be deleted
	mk("fresh-approved", models.StatusApproved, fresh, fresh, true)  // kept (recent)
	mk("old-pending", models.StatusPending, old, time.Time{}, false) // kept (never delete pending)

	n, err := st.DeleteDecidedBefore(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	for _, id := range []string{"fresh-approved", "old-pending"} {
		if _, err := st.GetRequest(id); err != nil {
			t.Errorf("%s should still exist, got %v", id, err)
		}
	}
	if _, err := st.GetRequest("old-approved"); err != ErrNotFound {
		t.Errorf("old-approved should be deleted, got %v", err)
	}
}

func TestAPIKeyLifecycle(t *testing.T) {
	st := newTestStore(t)
	if n, _ := st.CountAPIKeys(); n != 0 {
		t.Fatalf("expected 0 keys initially")
	}
	k, err := st.CreateAPIKey("n8n", "hash123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.LookupAPIKey("hash123"); err != nil {
		t.Errorf("lookup: %v", err)
	}
	if _, err := st.LookupAPIKey("wrong"); err != ErrNotFound {
		t.Errorf("lookup wrong = %v, want ErrNotFound", err)
	}
	if err := st.DeleteAPIKey(k.ID); err != nil {
		t.Errorf("delete: %v", err)
	}
	if _, err := st.LookupAPIKey("hash123"); err != ErrNotFound {
		t.Errorf("after delete lookup = %v, want ErrNotFound", err)
	}
}
