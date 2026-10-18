//go:build scenario

package main

import "time"

// scenarioClientLiveness describes a zero-time observation of the originally
// retained process. An observation error establishes neither alive nor exited.
type scenarioClientLiveness struct {
	ProcessID  uint32
	ObservedAt time.Time
	IsAlive    bool
	IsExited   bool
	ExitCode   *uint32
}
