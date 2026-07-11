package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MasterChief3301/greenlight/internal/models"
)

// CreateRule inserts a default rule.
func (s *Store) CreateRule(r *models.DefaultRule) error {
	now := time.Now().UTC()
	r.CreatedAt = now
	r.UpdatedAt = now
	res, err := s.db.Exec(`
		INSERT INTO default_rules (source, category, default_action, timeout_seconds, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		r.Source, r.Category, string(r.DefaultAction), r.TimeoutSeconds, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert rule: %w", err)
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

// UpdateRule modifies an existing rule.
func (s *Store) UpdateRule(r *models.DefaultRule) error {
	r.UpdatedAt = time.Now().UTC()
	res, err := s.db.Exec(`
		UPDATE default_rules
		SET source = ?, category = ?, default_action = ?, timeout_seconds = ?, updated_at = ?
		WHERE id = ?`,
		r.Source, r.Category, string(r.DefaultAction), r.TimeoutSeconds, r.UpdatedAt, r.ID)
	if err != nil {
		return fmt.Errorf("update rule: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteRule removes a rule by ID.
func (s *Store) DeleteRule(id int64) error {
	res, err := s.db.Exec(`DELETE FROM default_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetRule fetches a single rule by ID.
func (s *Store) GetRule(id int64) (*models.DefaultRule, error) {
	row := s.db.QueryRow(`
		SELECT id, source, category, default_action, timeout_seconds, created_at, updated_at
		FROM default_rules WHERE id = ?`, id)
	r, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// ListRules returns all rules, most specific first.
func (s *Store) ListRules() ([]*models.DefaultRule, error) {
	rows, err := s.db.Query(`
		SELECT id, source, category, default_action, timeout_seconds, created_at, updated_at
		FROM default_rules
		ORDER BY (source != '') DESC, (category != '') DESC, source, category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.DefaultRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResolveRule finds the most specific rule matching source+category. Precedence:
// exact (source+category) > source-only > category-only > global. Returns nil if
// no rule matches.
func (s *Store) ResolveRule(source, category string) (*models.DefaultRule, error) {
	rows, err := s.db.Query(`
		SELECT id, source, category, default_action, timeout_seconds, created_at, updated_at
		FROM default_rules
		WHERE (source = ? OR source = '') AND (category = ? OR category = '')
		ORDER BY (source != '') DESC, (category != '') DESC
		LIMIT 1`, source, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	return scanRule(rows)
}

func scanRule(sc interface {
	Scan(dest ...interface{}) error
}) (*models.DefaultRule, error) {
	var r models.DefaultRule
	var action string
	if err := sc.Scan(&r.ID, &r.Source, &r.Category, &action, &r.TimeoutSeconds, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	r.DefaultAction = models.Action(action)
	return &r, nil
}
