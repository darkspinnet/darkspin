//go:build scenario && !windows

package main

import (
	"context"
	"errors"
)

func (e *scenarioClient) CaptureEntryFrame(ctx context.Context, req scenarioEntryFrameRequest) (scenarioEntryFrame, error) {
	return scenarioEntryFrame{WindowCapture: &scenarioEntryWindowCapture{RunID: req.RunID, Sequence: req.Sequence,
		Backend: scenarioWindowBackend, Reason: "owned-window capture requires Windows386"}}, errors.New("entry frame capture requires the supported Windows desktop")
}
