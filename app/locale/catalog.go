// Package locale owns the shared launcher and in-game text catalogs.
package locale

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed *.json
var catalogs embed.FS

// Load returns the selected catalog. Empty entries fall back to their English
// source at the presentation boundary, including native Fang controls.
func Load(code string) (map[string]string, error) {
	code = strings.ToLower(strings.ReplaceAll(code, "_", "-"))
	switch strings.SplitN(code, "-", 2)[0] {
	case "de":
		code = "de-de"
	case "fr":
		code = "fr-fr"
	case "pl":
		code = "pl-pl"
	case "ru":
		code = "ru-ru"
	default:
		code = "en-us"
	}
	payload, err := catalogs.ReadFile(code + ".json")
	if err != nil {
		return nil, fmt.Errorf("catalogRead: %w", err)
	}
	var catalog struct {
		Messages map[string]string `json:"messages"`
	}
	err = json.Unmarshal(payload, &catalog)
	if err != nil {
		return nil, fmt.Errorf("catalogDecode: %w", err)
	}
	return catalog.Messages, nil
}
