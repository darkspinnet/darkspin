//go:build !scenario

package main

func debugDetachedCredential(jwt string) (string, error) { return jwt, nil }

func debugDetachedGameDirectory(directory string) string { return directory }
