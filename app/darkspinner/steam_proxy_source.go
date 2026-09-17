//go:build !windows || !steamproxy

package main

// Development builds without the steamproxy tag deliberately fail Steam
// integration installation instead of sourcing an unverified external DLL.
var embeddedSteamProxy []byte
