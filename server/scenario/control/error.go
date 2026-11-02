//go:build scenario

package control

import (
	"fmt"
	"sort"
	"strings"
)

type ErrorCode string

const (
	InvalidRequest   ErrorCode = "invalid_request"
	NotFound         ErrorCode = "not_found"
	Conflict         ErrorCode = "conflict"
	NotAvailable     ErrorCode = "unavailable"
	OwnershipLost    ErrorCode = "ownership_lost"
	DeadlineExceeded ErrorCode = "deadline_exceeded"
)

// ScenarioError preserves a typed cause chain within the host. RPC adapters
// encode and restore its fields explicitly; a JSON string is not a cause chain.
type ScenarioError struct {
	Code             ErrorCode
	Context          string
	Message          string
	ClientInstanceID string
	ActionID         string
	Cause            error
	Failures         map[string]*ScenarioError
}

func (e *ScenarioError) Error() string {
	detail := e.Message
	if len(e.Failures) > 0 {
		names := make([]string, 0, len(e.Failures))
		for name := range e.Failures {
			names = append(names, name)
		}
		sort.Strings(names)
		branches := make([]string, 0, len(names))
		for _, name := range names {
			branches = append(branches, name+": "+e.Failures[name].Error())
		}
		detail += ": " + strings.Join(branches, "; ")
	}
	if e.Cause != nil {
		if detail != "" {
			detail += ": "
		}
		detail += e.Cause.Error()
	}
	if e.Context != "" {
		return fmt.Sprintf("%s: %s", e.Context, detail)
	}
	return detail
}

func (e *ScenarioError) Unwrap() error {
	return e.Cause
}
