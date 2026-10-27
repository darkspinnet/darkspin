package game

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// PrepareDedicatedAuth gives a dedicated server its own signing secret while
// preserving an operator's existing key. The caller persists changed settings.
func (e *Config) PrepareDedicatedAuth() (bool, error) {
	if !e.Bool(ConfigServerIsDedicated) {
		return false, nil
	}
	secret := e.String(ConfigAuthJWTSecret)
	if secret != "" && secret != DefaultConfig().String(ConfigAuthJWTSecret) {
		if len(secret) < 32 {
			return false, errors.New("auth.jwt_secret must contain at least 32 bytes")
		}
		return false, nil
	}
	secretBytes := make([]byte, 32)
	byteCount, err := rand.Read(secretBytes)
	if err != nil {
		return false, fmt.Errorf("secretGenerate: %w", err)
	}
	if byteCount != len(secretBytes) {
		return false, errors.New("incomplete signing secret")
	}
	err = e.Set(ConfigAuthJWTSecret, base64.RawURLEncoding.EncodeToString(secretBytes))
	if err != nil {
		return false, fmt.Errorf("secretSet: %w", err)
	}
	return true, nil
}
