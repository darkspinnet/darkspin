package main

import (
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// DeleteModProfile removes an inactive account from the modded profile store.
func (e *App) DeleteModProfile(identity string) error {
	e.modMu.Lock()
	defer e.modMu.Unlock()
	identity = strings.TrimSpace(identity)
	err := validateProfileIdentity(identity)
	if err != nil {
		return fmt.Errorf("modIdentity: %w", err)
	}
	err = e.requireModIdle()
	if err != nil {
		return fmt.Errorf("modIdle: %w", err)
	}
	paths, err := e.modPaths()
	if err != nil {
		return fmt.Errorf("modResolve: %w", err)
	}
	ctx := e.modContext()
	repository, err := e.openProfileRepositoryAt(ctx, modDatabasePath(paths))
	if err != nil {
		return fmt.Errorf("modOpen: %w", err)
	}
	defer e.closeModRepository(repository)
	userManager, err := sporenet.NewUserManagerWithRepository(repository)
	if err != nil {
		return fmt.Errorf("modManager: %w", err)
	}
	err = userManager.DeleteProfile(ctx, identity)
	if err != nil {
		return fmt.Errorf("modDelete: %w", err)
	}
	return nil
}
