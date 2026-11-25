package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The embedded document is immutable after startup. Each world selects and
// retains its own entry rather than consuming mutable process-wide rules.
type serverRuleStore struct {
	shopRules sporenet.PartShopRules
	zoneRules zoneRuleDocument
}

// NewHandler's compatibility dependency also carries the typed zone selector.
// Production world preparation and alert passes use their zone-owned snapshot.
func (e *serverRuleStore) AllyAlertRules() zonenpc.AllyAlertRules {
	return zonenpc.DefaultAllyAlertRules()
}

func writeServerRuleSections(ctx context.Context, path string, sections map[string]any) (err error) {
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("rulesContext: %w", err)
	}
	var payload bytes.Buffer
	err = toml.NewEncoder(&payload).Encode(sections)
	if err != nil {
		return fmt.Errorf("rulesEncode: %w", err)
	}
	w, err := os.CreateTemp(filepath.Dir(path), "server-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("rulesTemp: %w", err)
	}
	temporaryPath := w.Name()
	isCommitted := false
	defer func() {
		if isCommitted {
			return
		}
		removeErr := os.Remove(temporaryPath)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("rulesCleanup: %w", removeErr))
		}
	}()
	writtenByteCount, writeErr := w.Write(payload.Bytes())
	if writeErr == nil && writtenByteCount != payload.Len() {
		writeErr = errors.New("incomplete server configuration write")
	}
	if writeErr == nil {
		writeErr = w.Sync()
	}
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("rulesWrite: %w", errors.Join(writeErr, closeErr))
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("rulesCommitContext: %w", err)
	}
	err = os.Rename(temporaryPath, path)
	if err != nil {
		return fmt.Errorf("rulesReplace: %w", err)
	}
	isCommitted = true
	return nil
}
