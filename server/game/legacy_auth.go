package game

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Decode only the compatibility key. Legacy server mode and gameplay settings
// must never influence dedicated startup.
func legacyDedicatedSigningKey(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("keyRead: %w", err)
	}
	var document struct {
		Auth struct {
			JWTSecret string `toml:"jwt_secret"`
		} `toml:"auth"`
	}
	err = toml.Unmarshal(contents, &document)
	if err != nil {
		// A TOML parser error may include the signing key's source line.
		return "", errors.New("legacy signing-key configuration is invalid")
	}
	return document.Auth.JWTSecret, nil
}
