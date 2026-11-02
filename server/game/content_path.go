package game

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ContentPath returns the private prepared-content directory beside the config
// unless an absolute path or config-relative directory was explicitly saved.
func (e *Config) ContentPath(configPath string) (string, error) {
	contentPath := strings.TrimSpace(e.String(ConfigContentPath))
	if contentPath == "" {
		contentPath = filepath.Join("darkspin", "cache")
	}
	if !filepath.IsAbs(contentPath) {
		contentPath = filepath.Join(filepath.Dir(configPath), contentPath)
	}
	absolutePath, err := filepath.Abs(contentPath)
	if err != nil {
		return "", fmt.Errorf("contentPath: %w", err)
	}
	return absolutePath, nil
}
