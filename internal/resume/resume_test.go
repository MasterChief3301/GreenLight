package resume

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MasterChief3301/greenlight/internal/models"
)

func sampleRequest(resumeURL string) *models.Request {
	now := time.Now().UTC()
	return &models.Request{
		ID:                 "abc123",
		Status:             models.StatusApproved,
		DecidedBy:          models.DecidedByUser,
		DecisionComment:    "ship it",
		ResumeURL:          resumeURL,
		ResumePayloadExtra: `{"correlation":"xyz"}`,
		DecidedAt:          &now,
	}
}

// TestDeliverGET verifies that a GET-configured client resumes with a GET and
// carries the decision in the query string (n8n's default Wait-node webhook).
func TestDeliverGET(t *testing.T) {
	var gotMethod, gotDecision, gotCorrelation string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotDecision = r.URL.Query().Get("decision")
		gotCorrelation = r.URL.Query().Get("correlation")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := New(5*time.Second, 0, "GET")
	if err := c.Deliver(context.Background(), sampleRequest(srv.URL)); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	if gotDecision != "approved" {
		t.Errorf("query decision = %q, want approved", gotDecision)
	}
	if gotCorrelation != "xyz" {
		t.Errorf("query correlation = %q, want xyz (extra field)", gotCorrelation)
	}
}

// TestDeliverPOST verifies POST delivery carries the decision in the JSON body
// AND the query string.
func TestDeliverPOST(t *testing.T) {
	var gotMethod, gotContentType string
	var bodyDecision, queryDecision string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		queryDecision = r.URL.Query().Get("decision")
		b, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(b, &payload)
		if d, ok := payload["decision"].(string); ok {
			bodyDecision = d
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := New(5*time.Second, 0, "POST")
	if err := c.Deliver(context.Background(), sampleRequest(srv.URL)); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotContentType)
	}
	if bodyDecision != "approved" {
		t.Errorf("body decision = %q, want approved", bodyDecision)
	}
	if queryDecision != "approved" {
		t.Errorf("query decision = %q, want approved", queryDecision)
	}
}

// TestDeliverRetriesThenFails verifies a non-2xx response is retried and
// ultimately surfaces as an error (what shows the "callback failed" badge).
func TestDeliverRetriesThenFails(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(2*time.Second, 1, "POST") // 1 retry → 2 attempts
	err := c.Deliver(context.Background(), sampleRequest(srv.URL))
	if err == nil {
		t.Fatal("expected error on repeated 404")
	}
	if calls != 2 {
		t.Errorf("attempts = %d, want 2", calls)
	}
}

// TestDeliverDefaultsToPOST verifies an empty method defaults to POST.
func TestDeliverDefaultsToPOST(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := New(5*time.Second, 0, "")
	if err := c.Deliver(context.Background(), sampleRequest(srv.URL)); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST (default)", gotMethod)
	}
}
