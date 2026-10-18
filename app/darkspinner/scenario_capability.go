//go:build scenario

package main

import (
	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/scenario"
)

// scenarioCapability reports only this launcher's compiled support. Live
// server and loaded Fang verification must be performed before automation.
func scenarioCapability() scenario.Capability {
	return scenario.NewCapability("launcher", buildinfo.ID)
}
