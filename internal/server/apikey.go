package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// apiKeyPrefix labels generated keys so they're recognizable.
const apiKeyPrefix = "glk_"

// generateAPIKey returns a new random plaintext API key. It is shown to the user
// exactly once; only its hash is stored.
func generateAPIKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return apiKeyPrefix + hex.EncodeToString(b)
}

// hashAPIKey returns the hex SHA-256 of a plaintext key, used as the stored/
// lookup value.
func hashAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
