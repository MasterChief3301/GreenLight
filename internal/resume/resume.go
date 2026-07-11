// Package resume delivers decision callbacks to n8n Wait-node resume URLs.
package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MasterChief3301/greenlight/internal/models"
)

// Client calls resume URLs with bounded retries and backoff, using a configured
// HTTP method (which must match the n8n Wait node's webhook method).
type Client struct {
	http       *http.Client
	maxRetries int
	method     string
}

// New builds a resume Client. method is the HTTP method used to call the resume
// URL (e.g. GET or POST); it defaults to POST if empty.
func New(timeout time.Duration, maxRetries int, method string) *Client {
	if maxRetries < 0 {
		maxRetries = 0
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodPost
	}
	return &Client{
		http:       &http.Client{Timeout: timeout},
		maxRetries: maxRetries,
		method:     method,
	}
}

// hasBody reports whether the configured method carries a request body.
func (c *Client) hasBody() bool {
	switch c.method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

// decisionFields builds the decision payload: the core fields merged with any
// caller-supplied extra fields (extras never clobber the core fields).
func decisionFields(r *models.Request) map[string]any {
	decision := "rejected"
	if r.Status.IsApproval() {
		decision = "approved"
	}
	base := map[string]any{
		"id":         r.ID,
		"decision":   decision,
		"status":     string(r.Status),
		"decided_by": string(r.DecidedBy),
	}
	if r.DecisionComment != "" {
		base["comment"] = r.DecisionComment
	}
	if r.DecidedAt != nil {
		base["decided_at"] = r.DecidedAt.UTC().Format(time.RFC3339)
	}
	if r.ResumePayloadExtra != "" {
		var extra map[string]any
		if err := json.Unmarshal([]byte(r.ResumePayloadExtra), &extra); err == nil {
			for k, v := range extra {
				if _, taken := base[k]; !taken {
					base[k] = v
				}
			}
		}
	}
	return base
}

// addQuery appends the decision fields to the resume URL as query parameters, so
// the decision is available to the n8n workflow regardless of the HTTP method
// (e.g. a GET Wait node reads them from $json.query). Complex (non-scalar) extra
// values are skipped in the query string; they still travel in the JSON body.
func addQuery(rawURL string, fields map[string]any) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, v := range fields {
		switch v.(type) {
		case string, bool, int, int64, float64, json.Number:
			q.Set(k, fmt.Sprint(v))
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Deliver calls the resume URL (using the configured method), retrying with
// exponential backoff on transient failures. Returns nil on success.
func (c *Client) Deliver(ctx context.Context, r *models.Request) error {
	if r.ResumeURL == "" {
		return fmt.Errorf("request %s has no resume_url", r.ID)
	}
	fields := decisionFields(r)

	target, err := addQuery(r.ResumeURL, fields)
	if err != nil {
		return fmt.Errorf("invalid resume_url: %w", err)
	}

	var body []byte
	if c.hasBody() {
		body, err = json.Marshal(fields)
		if err != nil {
			return fmt.Errorf("build callback body: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, 8s ... capped at 30s.
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		if err := c.do(ctx, target, body); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return fmt.Errorf("callback failed after %d attempts: %w", c.maxRetries+1, lastErr)
}

func (c *Client) do(ctx context.Context, url string, body []byte) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, c.method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("resume url returned status %d", resp.StatusCode)
	}
	return nil
}
