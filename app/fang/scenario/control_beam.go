//go:build windows && cgo && scenario && fangdebug

package scenario

/*
#include "scenario/control.h"
*/
import "C"

type controlBeamOutState struct {
	IsAvailable bool `json:"is_available"`
	IsReady     bool `json:"is_ready"`
	IsRequested bool `json:"is_requested"`
}

func readControlBeamReceipt(requestID uint32) *controlBeamOutState {
	var snapshot C.fang_control_beam_state
	isFound := C.fang_control_window_beam_result(C.uint32_t(requestID), &snapshot)
	if isFound == 0 {
		return nil
	}
	return &controlBeamOutState{IsAvailable: snapshot.is_available != 0,
		IsReady: snapshot.is_ready != 0, IsRequested: snapshot.is_requested != 0}
}
