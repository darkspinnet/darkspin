package appearancefs

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/darkspinnet/darkspin/content/creation"
)

var botAppearanceMutex sync.Mutex

// A gameplay appearance packs the revision above a 32-bit ID. Keep the
// resulting integer below 2^53 so native Flash number conversions are lossless.
const maximumBotAppearanceVersion = uint32(1<<21 - 1)

func (e Store) GenerateBot(ctx context.Context, sourceID, sourceVersion, noun uint32, seed uint64) (uint32, uint32, error) {
	id, version, err := e.generateBot(ctx, sourceID, sourceVersion, noun, seed)
	if err == nil {
		return id, version, nil
	}
	contextErr := ctx.Err()
	if contextErr != nil {
		return 0, 0, fmt.Errorf("botGenerate: %w", contextErr)
	}
	if e.Logger != nil {
		e.Logger.Printf("Bot appearance variation unavailable noun=%d: %v; retaining source appearance", noun, err)
	}
	return sourceID, sourceVersion, nil
}

func (e Store) generateBot(ctx context.Context, sourceID, sourceVersion, noun uint32, seed uint64) (uint32, uint32, error) {
	err := ctx.Err()
	if err != nil {
		return 0, 0, fmt.Errorf("botContext: %w", err)
	}
	if e.Templates == nil {
		return 0, 0, errors.New("bot appearance templates unavailable")
	}
	botAppearanceMutex.Lock()
	defer botAppearanceMutex.Unlock()
	var source []byte
	if sourceID != 0 && sourceVersion > 1 {
		source, err = os.ReadFile(filepath.Join(e.Root, "creature_png", fmt.Sprintf("%d_%d_thumb.png", sourceID, sourceVersion)))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, 0, fmt.Errorf("botSavedRead: %w", err)
		}
	}
	if len(source) == 0 {
		source, err = fs.ReadFile(e.Templates, fmt.Sprintf("template_png/%d_thumb.png", noun))
		if err != nil {
			return 0, 0, fmt.Errorf("botTemplateRead: %w", err)
		}
	}
	// Do not reuse images generated without the native carrier checksum footer.
	identity := append([]byte("darkspin-bot-appearance-v3"), source...)
	identity = binary.LittleEndian.AppendUint64(identity, seed)
	digest := sha256.Sum256(identity)
	id := binary.LittleEndian.Uint32(digest[:4]) | 0x80000000
	version := binary.LittleEndian.Uint32(digest[4:8])%(maximumBotAppearanceVersion-1) + 2
	directory := filepath.Join(e.Root, "cache", "bot_appearance")
	thumb := filepath.Join(directory, fmt.Sprintf("%d_%d_thumb.png", id, version))
	large := filepath.Join(directory, fmt.Sprintf("%d_%d_large.png", id, version))
	thumbStat, thumbErr := os.Stat(thumb)
	largeStat, largeErr := os.Stat(large)
	if thumbErr == nil && largeErr == nil && thumbStat.Mode().IsRegular() && largeStat.Mode().IsRegular() && thumbStat.Size() > 0 && largeStat.Size() > 0 {
		return id, version, nil
	}
	if thumbErr != nil && !errors.Is(thumbErr, os.ErrNotExist) {
		return 0, 0, fmt.Errorf("botThumbStat: %w", thumbErr)
	}
	if largeErr != nil && !errors.Is(largeErr, os.ErrNotExist) {
		return 0, 0, fmt.Errorf("botLargeStat: %w", largeErr)
	}
	varied, err := creation.VaryAppearance(source, seed)
	if err != nil && sourceVersion > 1 {
		// Legacy imported saves can use an older model schema. Generate from
		// this hero's installed retail template instead of altering that save.
		source, err = fs.ReadFile(e.Templates, fmt.Sprintf("template_png/%d_thumb.png", noun))
		if err != nil {
			return 0, 0, fmt.Errorf("botFallbackRead: %w", err)
		}
		varied, err = creation.VaryAppearance(source, seed)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("botVary: %w", err)
	}
	err = os.MkdirAll(directory, 0700)
	if err != nil {
		return 0, 0, fmt.Errorf("botDirectory: %w", err)
	}
	// Both routes must carry the same varied model, rather than a stale
	// original model hidden inside the large preview.
	for index, path := range []string{thumb, large} {
		err = os.WriteFile(path, varied, 0600)
		if err != nil {
			return 0, 0, fmt.Errorf("botImageWrite[%d]: %w", index, err)
		}
	}
	if e.Logger != nil {
		e.Logger.Printf("Generated bot appearance noun=%d source=%d version=%d appearance=%d_%d asset=%d",
			noun, sourceID, sourceVersion, id, version, uint64(version)<<32|uint64(id))
	}
	return id, version, nil
}
