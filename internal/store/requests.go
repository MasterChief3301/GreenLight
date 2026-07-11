package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eneat/greenlight/internal/models"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

const requestColumns = `id, title, description, source, category, priority, status,
	decided_by, decision_comment, default_action, timeout_seconds, resume_url,
	resume_payload_extra, metadata, reminder_sent_at, callback_failed, callback_error,
	created_at, decided_at`

// CreateRequest inserts a new pending request.
func (s *Store) CreateRequest(r *models.Request) error {
	_, err := s.db.Exec(`
		INSERT INTO approval_requests
			(id, title, description, source, category, priority, status, decided_by,
			 decision_comment, default_action, timeout_seconds, resume_url,
			 resume_payload_extra, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Title, r.Description, r.Source, r.Category, string(r.Priority),
		string(r.Status), string(r.DecidedBy), r.DecisionComment, string(r.DefaultAction),
		r.TimeoutSeconds, r.ResumeURL, r.ResumePayloadExtra, r.Metadata, r.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert request: %w", err)
	}
	return nil
}

func scanRequest(sc interface {
	Scan(dest ...interface{}) error
}) (*models.Request, error) {
	var r models.Request
	var priority, status, decidedBy, defaultAction string
	var reminderSent, decidedAt sql.NullTime
	var callbackFailed int
	if err := sc.Scan(
		&r.ID, &r.Title, &r.Description, &r.Source, &r.Category, &priority, &status,
		&decidedBy, &r.DecisionComment, &defaultAction, &r.TimeoutSeconds, &r.ResumeURL,
		&r.ResumePayloadExtra, &r.Metadata, &reminderSent, &callbackFailed, &r.CallbackError,
		&r.CreatedAt, &decidedAt,
	); err != nil {
		return nil, err
	}
	r.Priority = models.Priority(priority)
	r.Status = models.Status(status)
	r.DecidedBy = models.DecidedBy(decidedBy)
	r.DefaultAction = models.Action(defaultAction)
	r.ReminderSentAt = scanTime(reminderSent)
	r.DecidedAt = scanTime(decidedAt)
	r.CallbackFailed = callbackFailed != 0
	return &r, nil
}

// GetRequest fetches a request by ID. Returns ErrNotFound if missing.
func (s *Store) GetRequest(id string) (*models.Request, error) {
	row := s.db.QueryRow(`SELECT `+requestColumns+` FROM approval_requests WHERE id = ?`, id)
	r, err := scanRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	return r, nil
}

// RequestFilter narrows a ListRequests query. Empty fields are ignored.
type RequestFilter struct {
	Status   string
	Source   string
	Category string
	Since    *time.Time
	Until    *time.Time
	Limit    int
}

// ListRequests returns requests matching the filter, newest first.
func (s *Store) ListRequests(f RequestFilter) ([]*models.Request, error) {
	var where []string
	var args []interface{}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	if f.Source != "" {
		where = append(where, "source = ?")
		args = append(args, f.Source)
	}
	if f.Category != "" {
		where = append(where, "category = ?")
		args = append(args, f.Category)
	}
	if f.Since != nil {
		where = append(where, "created_at >= ?")
		args = append(args, *f.Since)
	}
	if f.Until != nil {
		where = append(where, "created_at <= ?")
		args = append(args, *f.Until)
	}

	query := `SELECT ` + requestColumns + ` FROM approval_requests`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list requests: %w", err)
	}
	defer rows.Close()

	var out []*models.Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListPending returns all pending requests, oldest first (soonest deadline bias).
func (s *Store) ListPending() ([]*models.Request, error) {
	rows, err := s.db.Query(`SELECT ` + requestColumns +
		` FROM approval_requests WHERE status = 'pending' ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()
	var out []*models.Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListOverdue returns pending requests whose deadline (created_at + timeout) has
// passed as of now.
func (s *Store) ListOverdue(now time.Time) ([]*models.Request, error) {
	rows, err := s.db.Query(`
		SELECT `+requestColumns+`
		FROM approval_requests
		WHERE status = 'pending'
		  AND datetime(created_at, '+' || timeout_seconds || ' seconds') <= ?
		ORDER BY created_at ASC`, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("list overdue: %w", err)
	}
	defer rows.Close()
	var out []*models.Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ErrAlreadyResolved indicates a compare-and-set failed because the request was
// no longer pending (someone/something resolved it first).
var ErrAlreadyResolved = errors.New("request already resolved")

// Resolve atomically transitions a pending request to a terminal status. It only
// succeeds if the request is still pending, making it race-safe between a user
// decision and the timeout engine. Returns the updated request, or
// ErrAlreadyResolved if it was no longer pending, or ErrNotFound if absent.
func (s *Store) Resolve(id string, status models.Status, by models.DecidedBy, comment string, decidedAt time.Time) (*models.Request, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var current string
	err = tx.QueryRow(`SELECT status FROM approval_requests WHERE id = ?`, id).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if models.Status(current) != models.StatusPending {
		return nil, ErrAlreadyResolved
	}

	_, err = tx.Exec(`
		UPDATE approval_requests
		SET status = ?, decided_by = ?, decision_comment = ?, decided_at = ?
		WHERE id = ? AND status = 'pending'`,
		string(status), string(by), comment, decidedAt.UTC(), id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetRequest(id)
}

// MarkReminderSent records that a reminder ping was published for a request.
func (s *Store) MarkReminderSent(id string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE approval_requests SET reminder_sent_at = ? WHERE id = ?`, at.UTC(), id)
	return err
}

// SetCallbackResult records the outcome of the resume_url callback attempt.
func (s *Store) SetCallbackResult(id string, failed bool, errMsg string) error {
	failedInt := 0
	if failed {
		failedInt = 1
	}
	_, err := s.db.Exec(
		`UPDATE approval_requests SET callback_failed = ?, callback_error = ? WHERE id = ?`,
		failedInt, errMsg, id)
	return err
}

// DeleteDecidedBefore removes decided (non-pending) requests whose decision time
// (or creation time, if somehow undecided) is older than cutoff. Pending
// requests are never deleted. Returns the number of rows removed.
func (s *Store) DeleteDecidedBefore(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(`
		DELETE FROM approval_requests
		WHERE status != 'pending'
		  AND COALESCE(decided_at, created_at) < ?`, cutoff.UTC())
	if err != nil {
		return 0, fmt.Errorf("purge history: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DistinctSources returns the distinct non-empty source values, for filter UIs.
func (s *Store) DistinctSources() ([]string, error) {
	return s.distinct("source")
}

// DistinctCategories returns the distinct non-empty category values.
func (s *Store) DistinctCategories() ([]string, error) {
	return s.distinct("category")
}

func (s *Store) distinct(col string) ([]string, error) {
	// col is a fixed internal identifier, never user input.
	rows, err := s.db.Query(`SELECT DISTINCT ` + col + ` FROM approval_requests WHERE ` + col + ` != '' ORDER BY ` + col)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
