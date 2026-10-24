package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	creationreader "github.com/darkspinnet/darkspin/content/creation"
)

var modCreationIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// modSavedAppearance rebases native editor saves onto this launch's Mod server.
// Accept only the account-local creature/revision filenames owned by this runtime.
func modSavedAppearance(root, address string, creatureID, version uint32, thumbURL, largeURL string) (string, string, error) {
	if version <= 1 {
		return "", "", nil
	}
	paths := make([]string, 0, 2)
	for index, imageURL := range []string{thumbURL, largeURL} {
		kind := "thumb"
		if index == 1 {
			kind = "large"
		}
		name := fmt.Sprintf("%d_%d_%s.png", creatureID, version, kind)
		uri, err := url.Parse(imageURL)
		if err != nil {
			return "", "", fmt.Errorf("savedURL[%d]: %w", index, err)
		}
		if uri.Path != "/creature_png/"+name {
			return "", "", nil
		}
		fi, err := os.Stat(filepath.Join(root, "creature_png", name))
		if err != nil {
			return "", "", fmt.Errorf("savedStat[%d]: %w", index, err)
		}
		if !fi.Mode().IsRegular() || fi.Size() == 0 {
			return "", "", errors.New("saved Mod creature image is empty or not a regular file")
		}
		paths = append(paths, "http://"+address+uri.Path)
	}
	return paths[0], paths[1], nil
}

func initialSporeUserPath() string {
	root := os.Getenv("APPDATA")
	if root == "" {
		return ""
	}
	return filepath.Join(root, "Spore")
}

func modCreationPath(paths *spinnerPathSet, id string) (string, error) {
	if !modCreationIDPattern.MatchString(id) {
		return "", errors.New("invalid creation identity")
	}
	return filepath.Join(modRuntimePath(paths), "cache", "creature", id), nil
}

func (a *App) findModCreation(paths *spinnerPathSet, id string) (creationreader.Creation, error) {
	for _, creation := range a.modState.creations {
		if creation.ID == id {
			return creation, nil
		}
	}
	settings, err := readLauncherSettings(paths.settingPath)
	if err != nil {
		return creationreader.Creation{}, fmt.Errorf("sourceSettings: %w", err)
	}
	creations, err := a.scanModCreations(settings.Mod.SporePath)
	if err != nil {
		return creationreader.Creation{}, fmt.Errorf("sourceScan: %w", err)
	}
	for _, creation := range creations {
		if creation.ID == id {
			return creation, nil
		}
	}
	return creationreader.Creation{}, errors.New("the creature no longer exists in the selected Spore source")
}

func (a *App) cacheModCreation(paths *spinnerPathSet, id string) error {
	cachePath, err := modCreationPath(paths, id)
	if err != nil {
		return fmt.Errorf("creationPath: %w", err)
	}
	creation, err := a.findModCreation(paths, id)
	if err != nil {
		return fmt.Errorf("creationFind: %w", err)
	}
	for _, resource := range []struct {
		resourceType, group uint32
		suffix              string
	}{
		{0x2f7d0004, creationreader.CreatureGroup, ".png"},
		{creationreader.CreatureType, creationreader.CreatureGroup, ".creaturedata"},
	} {
		payload, readErr := creationreader.Resource(creation, resource.resourceType, resource.group)
		if readErr != nil {
			return fmt.Errorf("creationResource: %w", readErr)
		}
		err = os.MkdirAll(filepath.Dir(cachePath), 0o755)
		if err != nil {
			return fmt.Errorf("creationMkdir: %w", err)
		}
		err = replaceLauncherConfig(cachePath+resource.suffix, payload)
		if err != nil {
			return fmt.Errorf("creationWrite: %w", err)
		}
	}
	large, err := creationreader.Resource(creation, 0x2f7d0004, 0x40626201)
	if errors.Is(err, os.ErrNotExist) {
		large, err = os.ReadFile(cachePath + ".png")
	}
	if err != nil {
		return fmt.Errorf("largeRead: %w", err)
	}
	err = replaceLauncherConfig(cachePath+".large.png", large)
	if err != nil {
		return fmt.Errorf("largeWrite: %w", err)
	}
	metadata, err := json.Marshal(creation)
	if err != nil {
		return fmt.Errorf("creationMarshal: %w", err)
	}
	err = replaceLauncherConfig(cachePath+".json", metadata)
	if err != nil {
		return fmt.Errorf("metadataWrite: %w", err)
	}
	return nil
}

// GetModPortrait loads only displayed portraits, avoiding a large base64 catalog.
func (a *App) GetModPortrait(id string) (string, error) {
	a.modMu.Lock()
	defer a.modMu.Unlock()
	paths, err := a.modPaths()
	if err != nil {
		return "", fmt.Errorf("portraitPaths: %w", err)
	}
	cachePath, err := modCreationPath(paths, id)
	if err != nil {
		return "", fmt.Errorf("portraitPath: %w", err)
	}
	payload, err := os.ReadFile(cachePath + ".png")
	if errors.Is(err, os.ErrNotExist) {
		creation, findErr := a.findModCreation(paths, id)
		if findErr != nil {
			return "", fmt.Errorf("portraitFind: %w", findErr)
		}
		payload, err = creationreader.Resource(creation, 0x2f7d0004, creationreader.CreatureGroup)
	}
	if err != nil {
		return "", fmt.Errorf("portraitRead: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload), nil
}

func modAppearanceRevision(payload []byte, identity string) uint32 {
	// Advance the revision when the mount pipeline changes so the native client
	// retries appearances rejected before the Spore cache packages were available.
	identity = "spore-scale-v4\x00" + identity
	digest := sha256.Sum256(append(append([]byte(nil), payload...), []byte(identity)...))
	// Fit the client's signed 31-bit revision field, reserving native defaults.
	return uint32(digest[0]&0x3f)<<24 | uint32(digest[1])<<16 | uint32(digest[2])<<8 | uint32(digest[3]) | 2
}

func modClientProfile(identity string) string {
	digest := sha256.Sum256([]byte(identity))
	return "mod-" + hex.EncodeToString(digest[:8])
}
