package game

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// reserveCreatureVersion keeps native ID/revision filenames unique even though
// creature IDs are account-local. Reservations remain after rejected saves so
// no future upload can overwrite an image another client may have cached.
func (e *API) reserveCreatureVersion(creatureID, previousVersion uint32) (uint32, error) {
	directory := filepath.Join(e.storage, "creature_png")
	err := os.MkdirAll(directory, 0o755)
	if err != nil {
		return 0, fmt.Errorf("revisionDirectory: %w", err)
	}
	for version := uint64(previousVersion) + 1; version <= 0x7fffffff; version++ {
		isUsed := false
		for _, kind := range []string{"large", "thumb"} {
			name := fmt.Sprintf("%d_%d_%s.png", creatureID, version, kind)
			_, err = os.Stat(filepath.Join(directory, name))
			if err == nil {
				isUsed = true
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return 0, fmt.Errorf("revisionImage: %w", err)
			}
		}
		if isUsed {
			continue
		}
		reservation := filepath.Join(directory, fmt.Sprintf("%d_%d.revision", creatureID, version))
		err = os.Mkdir(reservation, 0o700)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("revisionReserve: %w", err)
		}
		return uint32(version), nil
	}
	return 0, errors.New("creature revision exhausted")
}
