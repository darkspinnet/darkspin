//go:build windows && cgo && scenario && fangdebug

package main

/*
#include <stdint.h>
*/
import "C"

import (
	"encoding/json"
	"unsafe"

	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/scenario"
)

type scenarioCapabilityDTO struct {
	ProtocolVersion uint32   `json:"protocol_version"`
	Component       string   `json:"component"`
	BuildID         string   `json:"build_id"`
	Features        []string `json:"features"`
}

// GoScenarioCapability returns the required buffer size including the NUL byte.
// A nil or undersized destination performs a size query without writing. This
// export proves the queried DLL's compiled support, not that a client loaded it.
// It performs no gameplay action and exposes no network endpoint.
//
//export GoScenarioCapability
func GoScenarioCapability(destination *C.uchar, capacity C.uint) C.int {
	capability := scenario.NewCapability("fang", buildinfo.ID)
	payload, err := json.Marshal(scenarioCapabilityDTO{
		ProtocolVersion: capability.ProtocolVersion,
		Component:       capability.Component,
		BuildID:         capability.BuildID,
		Features:        capability.Features,
	})
	if err != nil {
		// This DTO has no unsupported JSON fields; a marshal failure still must
		// be reported to the caller rather than publishing a partial capability.
		return -1
	}
	required := len(payload) + 1
	if destination == nil || uint64(capacity) < uint64(required) {
		return C.int(required)
	}
	buffer := unsafe.Slice((*byte)(unsafe.Pointer(destination)), required)
	copy(buffer, payload)
	buffer[len(payload)] = 0
	return C.int(required)
}
