//go:build scenario && !windows

package main

import (
	"context"
	"errors"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type scenarioClientRequest struct {
	RunID            string
	GamePath         string
	WorkingDirectory string
	FangPath         string
	Arguments        []string
	ServerAddress    string
}

type scenarioClient struct{}

func startScenarioClient(lifetimeContext, startupContext context.Context, req scenarioClientRequest) (*scenarioClient, scenario.Capability, error) {
	return nil, scenario.Capability{}, errors.New("scenario suspended-client launch is supported only on Windows")
}

func (e *scenarioClient) Resume(ctx context.Context) (uint32, error) {
	return 0, errors.New("scenario client resume is supported only on Windows")
}

func (e *scenarioClient) Close(ctx context.Context) error {
	return nil
}
