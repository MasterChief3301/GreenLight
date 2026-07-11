package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"

	"github.com/MasterChief3301/greenlight/internal/store"
)

// BootstrapAPIKey generates and stores an initial API key, returning the
// plaintext (shown once). Used on a fresh install so n8n can authenticate
// before the operator visits the Settings page.
func BootstrapAPIKey(st *store.Store) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	plaintext := "glk_" + hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(plaintext))
	if _, err := st.CreateAPIKey("bootstrap", hex.EncodeToString(sum[:])); err != nil {
		return "", err
	}
	return plaintext, nil
}
