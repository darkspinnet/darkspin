//go:build !scenario

package main

import "errors"

// Refuse the private worker argument before ordinary startup can prepare a
// player's installation. No scenario operation is compiled in this build.
func runScenarioWorker(arguments []string) (bool, error) {
	if len(arguments) == 1 && arguments[0] == "--scenario-worker" {
		return true, errors.New("scenario worker is excluded from this build")
	}
	return false, nil
}
