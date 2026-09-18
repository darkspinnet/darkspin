package appearancefs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Store struct {
	Root string
}

// Version advertises only saved images that the HTTP creature route can serve.
// Version 1 selects the client's noun-specific shipped template.
func (e Store) Version(ctx context.Context, creatureID uint32, imageURL string) (uint32, error) {
	err := ctx.Err()
	if err != nil {
		return 0, fmt.Errorf("appearanceContext: %w", err)
	}
	uri, err := url.Parse(imageURL)
	if err != nil || imageURL == "" {
		return 1, nil
	}
	prefix := fmt.Sprintf("/creature_png/%d_", creatureID)
	if !strings.HasPrefix(uri.Path, prefix) || !strings.HasSuffix(uri.Path, "_thumb.png") {
		return 1, nil
	}
	encodedVersion := strings.TrimSuffix(strings.TrimPrefix(uri.Path, prefix), "_thumb.png")
	version, err := strconv.ParseUint(encodedVersion, 10, 31)
	if err != nil || version <= 1 {
		return 1, nil
	}
	name := fmt.Sprintf("%d_%d_thumb.png", creatureID, version)
	fi, err := os.Stat(filepath.Join(e.Root, "creature_png", name))
	if errors.Is(err, os.ErrNotExist) {
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("appearanceStat: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Size() == 0 {
		return 1, nil
	}
	return uint32(version), nil
}
