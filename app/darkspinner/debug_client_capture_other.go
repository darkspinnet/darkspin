//go:build scenario && !windows

package main

import (
	"context"
	"github.com/darkspinnet/darkspin/server/scenario/control"
)

func (e *debugClientManager) Screenshot(ctx context.Context, clientID, label string) (debugScreenshot, error) {
	return debugScreenshot{ClientID: clientID, Label: label, Reason: "owned window capture requires Windows"}, &control.ScenarioError{Code: control.NotAvailable, Context: "clientScreenshot", Message: "owned window capture requires Windows"}
}
