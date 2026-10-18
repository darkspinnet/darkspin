//go:build scenario

// Package desktop adapts the scenario ports to one owned desktop worker over
// inherited pipes. It exposes no listening control endpoint.
package desktop

import (
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
)

const WorkerArgument = "--scenario-worker"

type StartRequest struct {
	RunID         string
	Paths         scenario.RunPaths
	Definition    scenario.Definition
	GameDirectory string
	ContentPath   string
	FangPath      string
	Port          uint16
}

// Token travels only through private inherited pipes, never arguments or logs.
type Request struct {
	ProtocolVersion uint32
	Token           string
	Sequence        uint64
	Operation       string
	Deadline        time.Time
	Start           *StartRequest
	Milestone       scenario.StepKind
}

type Response struct {
	ProtocolVersion uint32
	Sequence        uint64
	Capabilities    []scenario.Capability
	Observation     scenario.Observation
	Versions        []scenario.Version
	Error           string
}
