//go:build scenario

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/control"
)

const debugLuaWorkerArgument = "--scenario-script-worker"
const debugLuaFrameLimit = 2 * 1024 * 1024
const debugLuaExecutionLimit = 180 * time.Second
const debugLuaMaximumExecutionLimit = 30 * time.Minute

type debugOperationRequest struct {
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload"`
	TimeoutMS uint32          `json:"timeout_ms,omitempty"`
}

type debugOperationResponse struct {
	Result  any         `json:"result"`
	Failure *debugError `json:"failure"`
}

type debugLuaBootstrap struct {
	Name     string    `json:"name"`
	Source   string    `json:"source"`
	Deadline time.Time `json:"deadline"`
}

type debugLuaFrame struct {
	Version   int                    `json:"version"`
	Kind      string                 `json:"kind"`
	Sequence  uint64                 `json:"sequence"`
	Bootstrap *debugLuaBootstrap     `json:"bootstrap,omitempty"`
	Request   *debugOperationRequest `json:"request,omitempty"`
	Result    json.RawMessage        `json:"result,omitempty"`
	Failure   *debugError            `json:"failure,omitempty"`
	Traceback string                 `json:"traceback,omitempty"`
}

func decodeDebugJSON(payload []byte, document any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(document)
	if err != nil {
		return fmt.Errorf("jsonDecode: %w", err)
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		return errors.New("request contains trailing JSON")
	}
	return nil
}

func debugErrorDomain(failure *debugError) *control.ScenarioError {
	if failure == nil {
		return nil
	}
	result := &control.ScenarioError{Code: control.ErrorCode(failure.Code), Context: failure.Context,
		Message: failure.Message, ClientInstanceID: failure.ClientInstanceID, ActionID: failure.ActionID}
	if failure.Cause != nil {
		result.Cause = debugErrorDomain(failure.Cause)
	}
	if len(failure.Failures) > 0 {
		result.Failures = make(map[string]*control.ScenarioError, len(failure.Failures))
		for name, branchFailure := range failure.Failures {
			result.Failures[name] = debugErrorDomain(branchFailure)
		}
	}
	return result
}
