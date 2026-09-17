//go:build !windows

package main

import "errors"

func openFirewallSettings() error {
	return errors.New("Windows Firewall settings are available on Windows only")
}
