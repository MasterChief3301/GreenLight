// Package models defines the core domain types for Greenlight.
package models

import "time"

// Status is the lifecycle state of an approval request.
type Status string

const (
	StatusPending         Status = "pending"
	StatusApproved        Status = "approved"
	StatusRejected        Status = "rejected"
	StatusExpiredApproved Status = "expired-approved"
	StatusExpiredRejected Status = "expired-rejected"
	StatusCancelled       Status = "cancelled"
)

// IsTerminal reports whether the status is a final, decided state.
func (s Status) IsTerminal() bool {
	return s != StatusPending
}

// IsApproval reports whether the status represents an approved outcome
// (whether decided by a user or by a timeout default).
func (s Status) IsApproval() bool {
	return s == StatusApproved || s == StatusExpiredApproved
}

// Action is a default/decision action.
type Action string

const (
	ActionApprove Action = "approve"
	ActionReject  Action = "reject"
)

// Valid reports whether the action is one of the known values.
func (a Action) Valid() bool {
	return a == ActionApprove || a == ActionReject
}

// TerminalStatus maps a decision action to the status it produces.
// If expired is true, the timeout-default variant is returned.
func (a Action) TerminalStatus(expired bool) Status {
	if a == ActionApprove {
		if expired {
			return StatusExpiredApproved
		}
		return StatusApproved
	}
	if expired {
		return StatusExpiredRejected
	}
	return StatusRejected
}

// Priority affects the ntfy priority header.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
)

// Valid reports whether the priority is a known value.
func (p Priority) Valid() bool {
	return p == PriorityLow || p == PriorityNormal || p == PriorityHigh
}

// DecidedBy records who or what resolved a request.
type DecidedBy string

const (
	DecidedByUser    DecidedBy = "user"
	DecidedByTimeout DecidedBy = "timeout"
	DecidedByAPI     DecidedBy = "api"
)

// Request is an approval request in the system.
type Request struct {
	ID                 string
	Title              string
	Description        string
	Source             string
	Category           string // nullable → "" when absent
	Priority           Priority
	Status             Status
	DecidedBy          DecidedBy // "" when pending
	DecisionComment    string
	DefaultAction      Action
	TimeoutSeconds     int
	ResumeURL          string
	ResumePayloadExtra string // raw JSON, "" when absent
	Metadata           string // raw JSON, "" when absent
	// ReminderSentAt marks when a reminder ping was published, so we only send once.
	ReminderSentAt *time.Time
	// CallbackFailed is set when the resume_url callback exhausted retries.
	CallbackFailed bool
	CallbackError  string
	CreatedAt      time.Time
	DecidedAt      *time.Time
}

// DeadlineTime is the moment the timeout default should fire.
func (r *Request) DeadlineTime() time.Time {
	return r.CreatedAt.Add(time.Duration(r.TimeoutSeconds) * time.Second)
}

// DefaultRule configures the default action/timeout applied to new requests
// that don't specify them, matched by source + category.
type DefaultRule struct {
	ID             int64
	Source         string // "" = matches any source
	Category       string // "" = matches any category
	DefaultAction  Action
	TimeoutSeconds int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// APIKey is a credential used by callers (n8n) to authenticate to the API.
type APIKey struct {
	ID         int64
	Label      string
	KeyHash    string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}
