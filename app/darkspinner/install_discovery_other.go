//go:build !windows && !linux

package main

func discoverSteamRoots() []string {
	return nil
}

func discoverSteamGameRoot([]string) string {
	return ""
}
