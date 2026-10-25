//go:build scenario

package main

import (
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/control"
)

// Internal launch boundary. The public adapters resolve credentials and never
// marshal this request or accept raw JWTs from scripts.
type debugClientLaunchRequest struct {
	UserID             int64
	ClientID           string
	ProfileName        string
	GameDirectory      string
	FangPath           string
	ServerAddress      string
	JWT                string
	SnapshotEndpoint   string
	SnapshotToken      string
	SnapshotDirectory  string
	IsDetached         bool
	IsModded           bool
	ModManifest        string
	IsCinematicSkipped bool
	IsIntroSkipped     bool
}

type debugClientState struct {
	UserID           int64
	ClientID         string
	ProfileName      string
	InstanceID       string
	ProcessID        uint32
	IsDetached       bool
	IsModded         bool
	IsRunning        bool
	LaunchMode       string
	Status           string
	ProfileDirectory string
	TracePath        string
	Capability       scenario.Capability
	Observation      control.ClientState
}
