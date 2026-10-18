package game

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Gameplay appearances use a revision-qualified 64-bit asset ID because the
// native client caches appearances by ID alone. Resolve the native reconstructed
// URL to the existing immutable saved image; Arsenal URLs keep their local IDs.
func gameplayAppearanceResource(resource string) (string, error) {
	const prefix = "/creature_png/"
	if !strings.HasPrefix(resource, prefix) {
		return resource, nil
	}
	fields := strings.Split(strings.TrimPrefix(resource, prefix), "_")
	if len(fields) != 3 {
		return resource, nil
	}
	assetID, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return "", fmt.Errorf("appearanceAsset: %w", err)
	}
	if assetID <= 0xffffffff {
		return resource, nil
	}
	version, err := strconv.ParseUint(fields[1], 10, 31)
	if err != nil {
		return "", fmt.Errorf("appearanceRevision: %w", err)
	}
	creatureID := uint32(assetID)
	if version <= 1 || assetID>>32 != version || creatureID == 0 ||
		(fields[2] != "thumb.png" && fields[2] != "large.png") {
		return "", errors.New("gameplay appearance identity invalid")
	}
	return fmt.Sprintf("%s%d_%d_%s", prefix, creatureID, version, fields[2]), nil
}
