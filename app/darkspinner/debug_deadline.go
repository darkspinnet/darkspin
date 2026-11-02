//go:build scenario

package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (e *debugSupervisor) executeDebugRequest(ctx context.Context, req debugOperationRequest) (any, error) {
	if req.TimeoutMS > 120000 {
		return nil, errors.New("operation timeout must be at most 120000 milliseconds")
	}
	if req.TimeoutMS != 0 {
		operationContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
		defer cancel()
		ctx = operationContext
	}
	if isDebugAutomationOperation(req.Operation) {
		result, err := e.executeDebugAutomation(ctx, req)
		if err != nil {
			return result, fmt.Errorf("automationRun: %w", err)
		}
		return result, nil
	}
	result, err := e.executeDebugOperation(ctx, req.Operation, req.Payload)
	if err != nil {
		return result, fmt.Errorf("operationRun: %w", err)
	}
	return result, nil
}
