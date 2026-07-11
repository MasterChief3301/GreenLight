// Package ntfy publishes push notifications to an ntfy server.
package ntfy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eneat/greenlight/internal/models"
)

// Client publishes messages to a single ntfy topic.
type Client struct {
	baseURL string
	topic   string
	token   string
	user    string
	pass    string
	http    *http.Client
}

// Config configures a Client.
type Config struct {
	BaseURL string
	Topic   string
	Token   string
	User    string
	Pass    string
}

// New builds a Client. It returns nil if the base URL or topic is empty, in
// which case callers should treat notifications as disabled.
func New(cfg Config) *Client {
	if cfg.BaseURL == "" || cfg.Topic == "" {
		return nil
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		topic:   cfg.Topic,
		token:   cfg.Token,
		user:    cfg.User,
		pass:    cfg.Pass,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Message is a notification to publish.
type Message struct {
	Title    string
	Body     string
	Priority models.Priority
	ClickURL string   // opened when the notification is tapped
	Tags     []string // ntfy emoji/tags
}

// ntfyPriority maps our priority to ntfy's 1-5 scale.
func ntfyPriority(p models.Priority) string {
	switch p {
	case models.PriorityLow:
		return "2"
	case models.PriorityHigh:
		return "5"
	default:
		return "3"
	}
}

// Publish sends a message to the configured topic.
func (c *Client) Publish(ctx context.Context, m Message) error {
	url := c.baseURL + "/" + c.topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(m.Body)))
	if err != nil {
		return err
	}
	if m.Title != "" {
		req.Header.Set("Title", sanitizeHeader(m.Title))
	}
	req.Header.Set("Priority", ntfyPriority(m.Priority))
	if m.ClickURL != "" {
		req.Header.Set("Click", m.ClickURL)
	}
	if len(m.Tags) > 0 {
		req.Header.Set("Tags", strings.Join(m.Tags, ","))
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy publish: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ntfy publish: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// sanitizeHeader strips characters that are invalid in HTTP header values
// (ntfy reads title/click from headers, so newlines must not leak through).
func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
