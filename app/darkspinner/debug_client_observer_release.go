//go:build windows && !scenario

package main

func debugDetachedLaunchObserver() injectedLaunchObserver { return nil }
