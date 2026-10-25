//go:build scenario

package main

import (
	"context"
	"fmt"
	"time"
)

// Replacements release only clients owned by this supervisor and profile alias.
// The operation mutex remains held through closure and profile replacement.
func (e *debugSupervisor) releaseProfileClients(ctx context.Context, name string) error {
	manager := e.operationState.clientManager
	for _, client := range manager.List() {
		if client.ProfileName != name {
			continue
		}
		graceful, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := manager.Close(graceful, client.ClientID, false)
		cancel()
		if err != nil {
			e.logLuaActivity("Closing profile client " + client.ClientID + ": " + err.Error())
			err = manager.Close(ctx, client.ClientID, true)
			if err != nil {
				return fmt.Errorf("profileClose: %w", err)
			}
		}
		err = manager.Forget(client.ClientID)
		if err != nil {
			return fmt.Errorf("profileForget: %w", err)
		}
		e.registry.ForgetClient(client.ClientID)
	}
	return nil
}
