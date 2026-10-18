//go:build scenario && !windows

package main

import (
	"context"
	"errors"
)

func (e *scenarioClient) Liveness(ctx context.Context) (scenarioClientLiveness, error) {
	return scenarioClientLiveness{}, errors.New("owned scenario client liveness requires Windows")
}
