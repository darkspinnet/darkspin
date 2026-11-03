package game

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/darkspinnet/darkspin/server/assets"
)

var configurationMutex sync.Mutex

var ErrConfigurationConflict = errors.New("configuration changed; reload before saving")

// WithConfiguration serializes a complete read/modify/write operation with all
// shared configuration owners. The rewrite must use the supplied current bytes
// and must not call another shared configuration read/write operation.
// Editors in other processes can still change the file after the final content
// check; filesystem rename does not provide a cross-process compare-and-swap.
func WithConfiguration(path string, rewrite func([]byte) ([]byte, error)) error {
	if rewrite == nil {
		return errors.New("configuration rewrite unavailable")
	}
	configurationMutex.Lock()
	defer configurationMutex.Unlock()
	currentContents, err := readConfiguration(path)
	if err != nil {
		return fmt.Errorf("configurationRead: %w", err)
	}
	contents, err := rewrite(currentContents)
	if err != nil {
		return fmt.Errorf("configurationRewrite: %w", err)
	}
	if bytes.Equal(contents, currentContents) {
		return nil
	}
	err = replaceConfiguration(path, currentContents, contents)
	if err != nil {
		return fmt.Errorf("configurationSave: %w", err)
	}
	return nil
}

func readConfiguration(path string) ([]byte, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("configurationRead: %w", err)
	}
	return contents, nil
}

func replaceConfiguration(path string, previousContents, contents []byte) error {
	var document configDocument
	err := toml.Unmarshal(contents, &document)
	if err != nil {
		return fmt.Errorf("configurationDecode: %w", err)
	}
	mode := os.FileMode(0o644)
	fi, err := os.Stat(path)
	if err == nil {
		mode = fi.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("configurationStat: %w", err)
	}
	if document.Server.IsDedicated != nil && *document.Server.IsDedicated {
		mode = 0o600
	}
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return fmt.Errorf("configurationDirectory: %w", err)
	}
	w, err := os.CreateTemp(filepath.Dir(path), ".darkspin-*.toml")
	if err != nil {
		return fmt.Errorf("configurationTemp: %w", err)
	}
	tempPath := w.Name()
	defer removeConfigurationTemp(tempPath)
	err = w.Chmod(mode)
	if err != nil {
		closeErr := w.Close()
		return fmt.Errorf("configurationMode: %w", errors.Join(err, closeErr))
	}
	byteCount, err := w.Write(contents)
	if err != nil || byteCount != len(contents) {
		closeErr := w.Close()
		return fmt.Errorf("configurationWrite: %w", errors.Join(err, closeErr, errors.New("configuration write incomplete")))
	}
	err = w.Sync()
	if err != nil {
		closeErr := w.Close()
		return fmt.Errorf("configurationSync: %w", errors.Join(err, closeErr))
	}
	err = w.Close()
	if err != nil {
		return fmt.Errorf("configurationClose: %w", err)
	}
	latestContents, err := readConfiguration(path)
	if err != nil {
		return fmt.Errorf("configurationRecheck: %w", err)
	}
	if !bytes.Equal(latestContents, previousContents) {
		return ErrConfigurationConflict
	}
	err = os.Rename(tempPath, path)
	if err != nil {
		return fmt.Errorf("configurationReplace: %w", err)
	}
	return nil
}

func removeConfigurationTemp(path string) {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("Configuration temporary cleanup failed: %v", err)
	}
}

func createDefaultConfiguration(path string) (bool, error) {
	configurationMutex.Lock()
	defer configurationMutex.Unlock()
	fi, err := os.Stat(path)
	if err == nil {
		if fi.IsDir() {
			return false, errors.New("configuration path is a directory")
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("defaultStat: %w", err)
	}
	err = replaceConfiguration(path, nil, assets.DefaultConfigTOML)
	if err != nil {
		return false, fmt.Errorf("defaultSave: %w", err)
	}
	return true, nil
}
