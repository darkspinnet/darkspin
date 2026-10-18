//go:build !linux && !darwin

package main

func isWineRunnerSupported() bool {
	return false
}

func detectWineRunners(*spinnerPathSet) []WineRunner {
	return nil
}
