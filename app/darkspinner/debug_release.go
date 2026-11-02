//go:build !scenario

package main

import (
	"errors"
	"strings"
)

func runDebugSupervisor(arguments []string) (bool, error) {
	for _, argument := range arguments {
		name := argument
		separator := strings.IndexByte(argument, '=')
		if separator >= 0 {
			name = argument[:separator]
		}
		if name == "--debug-api" || name == "--lua" || name == "--lua-headless" || name == "--lua-timeout" || name == "--scenario-supervisor-worker" || name == "--scenario-script-worker" || name == "--scenario-worker" || name == "--scenario-detached-worker" || name == "--scenario-debugger-worker" {
			return true, errors.New("scenario automation is excluded from this build")
		}
	}
	return false, nil
}
