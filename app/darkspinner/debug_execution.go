//go:build scenario

package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type debugLaunchOptions struct {
	scriptName     string
	executionLimit time.Duration
}

func parseDebugLaunchOptions(arguments []string) (debugLaunchOptions, error) {
	options := debugLaunchOptions{executionLimit: debugLuaExecutionLimit}
	filteredArguments := make([]string, 0, len(arguments))
	isLimitSet := false
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument != "--lua-timeout" && !strings.HasPrefix(argument, "--lua-timeout=") {
			filteredArguments = append(filteredArguments, argument)
			continue
		}
		if isLimitSet {
			return options, errors.New("--lua-timeout may be supplied only once")
		}
		isLimitSet = true
		duration := strings.TrimPrefix(argument, "--lua-timeout=")
		if argument == "--lua-timeout" {
			index++
			if index >= len(arguments) {
				return options, errors.New("--lua-timeout requires a duration, such as 20m")
			}
			duration = arguments[index]
		}
		limit, err := time.ParseDuration(duration)
		if err != nil {
			return options, fmt.Errorf("luaTimeoutParse: %w", err)
		}
		if limit < time.Second || limit > debugLuaMaximumExecutionLimit {
			return options, errors.New("--lua-timeout must be between 1s and 30m")
		}
		options.executionLimit = limit
	}
	name, err := parseDebugArguments(filteredArguments)
	if err != nil {
		return options, fmt.Errorf("luaArguments: %w", err)
	}
	if isLimitSet && name == "" {
		return options, errors.New("--lua-timeout requires --lua")
	}
	options.scriptName = name
	return options, nil
}
