package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MasterChief3301/greenlight/internal/models"
)

// CreateAPIKey stores a new API key by its hash and label.
func (s *Store) CreateAPIKey(label, keyHash string) (*models.APIKey, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO api_keys (label, key_hash, created_at) VALUES (?, ?, ?)`,
		label, keyHash, now)
	if err != nil {
		return nil, fmt.Errorf("insert api key: %w", err)
	}
	id, _ := res.LastInsertId()
	return &models.APIKey{ID: id, Label: label, KeyHash: keyHash, CreatedAt: now}, nil
}

// LookupAPIKey finds a key by its hash and returns it, or ErrNotFound.
func (s *Store) LookupAPIKey(keyHash string) (*models.APIKey, error) {
	row := s.db.QueryRow(
		`SELECT id, label, key_hash, created_at, last_used_at FROM api_keys WHERE key_hash = ?`,
		keyHash)
	var k models.APIKey
	var lastUsed sql.NullTime
	err := row.Scan(&k.ID, &k.Label, &k.KeyHash, &k.CreatedAt, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	k.LastUsedAt = scanTime(lastUsed)
	return &k, nil
}

// TouchAPIKey updates the last_used_at timestamp.
func (s *Store) TouchAPIKey(id int64) error {
	_, err := s.db.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, time.Now().UTC(), id)
	return err
}

// ListAPIKeys returns all keys (hashes included, never the plaintext).
func (s *Store) ListAPIKeys() ([]*models.APIKey, error) {
	rows, err := s.db.Query(
		`SELECT id, label, key_hash, created_at, last_used_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.APIKey
	for rows.Next() {
		var k models.APIKey
		var lastUsed sql.NullTime
		if err := rows.Scan(&k.ID, &k.Label, &k.KeyHash, &k.CreatedAt, &lastUsed); err != nil {
			return nil, err
		}
		k.LastUsedAt = scanTime(lastUsed)
		out = append(out, &k)
	}
	return out, rows.Err()
}

// DeleteAPIKey removes a key by ID.
func (s *Store) DeleteAPIKey(id int64) error {
	res, err := s.db.Exec(`DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountAPIKeys returns the number of configured API keys.
func (s *Store) CountAPIKeys() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM api_keys`).Scan(&n)
	return n, err
}
